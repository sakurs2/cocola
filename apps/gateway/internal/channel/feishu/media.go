package feishu

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

const (
	maxMediaErrorBytes = int64(1 << 20)
)

type DownloadedResource struct {
	Filename string
	MIME     string
	Content  []byte
}

type BoundedDownloader struct {
	identity    Identity
	credentials interface {
		RuntimeCredential(context.Context, Identity) (RuntimeCredential, error)
	}
	baseURL string
	http    *http.Client
}

func NewBoundedDownloader(
	connector Connector,
	credentials interface {
		RuntimeCredential(context.Context, Identity) (RuntimeCredential, error)
	},
	httpClient *http.Client,
) *BoundedDownloader {
	if httpClient == nil {
		httpClient = &http.Client{
			Timeout: 2 * time.Minute,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
	}
	return &BoundedDownloader{
		identity:    Identity{TenantID: connector.TenantID, UserID: connector.UserID},
		credentials: credentials, http: httpClient,
	}
}

func (d *BoundedDownloader) Download(
	ctx context.Context,
	messageID string,
	resource Resource,
	maxBytes int64,
) (DownloadedResource, error) {
	if maxBytes <= 0 || messageID == "" || resource.FileKey == "" {
		return DownloadedResource{}, ErrInvalid
	}
	resourceType := resource.Type
	if resourceType != "image" && resourceType != "file" {
		return DownloadedResource{}, ErrInvalid
	}
	if d.credentials == nil {
		return DownloadedResource{}, errors.New("Feishu runtime credential resolver is unavailable")
	}
	credential, err := d.credentials.RuntimeCredential(ctx, d.identity)
	if err != nil {
		return DownloadedResource{}, err
	}
	if credential.Status != RuntimeCredentialReady || credential.TenantAccessToken == "" {
		return DownloadedResource{}, errors.New("Feishu runtime credential is unavailable")
	}
	baseURL := d.baseURL
	if baseURL == "" {
		switch credential.Brand {
		case DomainFeishu:
			baseURL = "https://open.feishu.cn"
		case DomainLark:
			baseURL = "https://open.larksuite.com"
		default:
			return DownloadedResource{}, errors.New("Feishu runtime credential brand is invalid")
		}
	}
	endpoint := baseURL + "/open-apis/im/v1/messages/" +
		url.PathEscape(messageID) + "/resources/" + url.PathEscape(resource.FileKey) +
		"?type=" + url.QueryEscape(resourceType)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return DownloadedResource{}, err
	}
	req.Header.Set("authorization", "Bearer "+credential.TenantAccessToken)
	resp, err := d.http.Do(req)
	if err != nil {
		return DownloadedResource{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxMediaErrorBytes))
		return DownloadedResource{}, fmt.Errorf("Feishu resource download returned %d", resp.StatusCode)
	}
	if resp.ContentLength > maxBytes {
		return DownloadedResource{}, ErrAttachmentTooLarge
	}
	var buffer bytes.Buffer
	read, err := io.Copy(&buffer, io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return DownloadedResource{}, err
	}
	if read > maxBytes {
		return DownloadedResource{}, ErrAttachmentTooLarge
	}
	filename := strings.TrimSpace(resource.Filename)
	if disposition := resp.Header.Get("content-disposition"); disposition != "" {
		if _, params, parseErr := mime.ParseMediaType(disposition); parseErr == nil &&
			strings.TrimSpace(params["filename"]) != "" {
			filename = strings.TrimSpace(params["filename"])
		}
	}
	if filename == "" {
		filename = path.Base(resource.FileKey)
	}
	contentType := strings.TrimSpace(resp.Header.Get("content-type"))
	if mediaType, _, parseErr := mime.ParseMediaType(contentType); parseErr == nil {
		contentType = mediaType
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	return DownloadedResource{
		Filename: filename,
		MIME:     contentType,
		Content:  buffer.Bytes(),
	}, nil
}
