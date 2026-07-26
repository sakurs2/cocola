package feishu

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"
)

const (
	maxTokenResponseBytes = int64(1 << 20)
	maxMediaErrorBytes    = int64(1 << 20)
)

type DownloadedResource struct {
	Filename string
	MIME     string
	Content  []byte
}

type BoundedDownloader struct {
	appID     string
	appSecret string
	baseURL   string
	http      *http.Client

	mu           sync.Mutex
	tenantToken  string
	tokenExpires time.Time
}

func NewBoundedDownloader(
	connector Connector,
	appSecret string,
	httpClient *http.Client,
) *BoundedDownloader {
	baseURL := "https://open.feishu.cn"
	if connector.Domain == DomainLark {
		baseURL = "https://open.larksuite.com"
	}
	if httpClient == nil {
		httpClient = &http.Client{
			Timeout: 2 * time.Minute,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
	}
	return &BoundedDownloader{
		appID: connector.AppID, appSecret: appSecret, baseURL: baseURL, http: httpClient,
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
	token, err := d.token(ctx)
	if err != nil {
		return DownloadedResource{}, err
	}
	endpoint := d.baseURL + "/open-apis/im/v1/messages/" +
		url.PathEscape(messageID) + "/resources/" + url.PathEscape(resource.FileKey) +
		"?type=" + url.QueryEscape(resourceType)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return DownloadedResource{}, err
	}
	req.Header.Set("authorization", "Bearer "+token)
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

func (d *BoundedDownloader) token(ctx context.Context) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.tenantToken != "" && time.Now().UTC().Add(time.Minute).Before(d.tokenExpires) {
		return d.tenantToken, nil
	}
	payload, err := json.Marshal(map[string]string{
		"app_id": d.appID, "app_secret": d.appSecret,
	})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		d.baseURL+"/open-apis/auth/v3/tenant_access_token/internal",
		bytes.NewReader(payload),
	)
	if err != nil {
		return "", err
	}
	req.Header.Set("content-type", "application/json")
	resp, err := d.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxTokenResponseBytes))
		return "", fmt.Errorf("Feishu tenant token returned %d", resp.StatusCode)
	}
	var result struct {
		Code              int    `json:"code"`
		Message           string `json:"msg"`
		TenantAccessToken string `json:"tenant_access_token"`
		Expire            int    `json:"expire"`
	}
	decoder := json.NewDecoder(io.LimitReader(resp.Body, maxTokenResponseBytes))
	if err := decoder.Decode(&result); err != nil {
		return "", err
	}
	if result.Code != 0 || result.TenantAccessToken == "" {
		return "", errors.New("Feishu tenant token was rejected")
	}
	expiresIn := result.Expire
	if expiresIn <= 0 {
		expiresIn = 3600
	}
	d.tenantToken = result.TenantAccessToken
	d.tokenExpires = time.Now().UTC().Add(time.Duration(expiresIn) * time.Second)
	return d.tenantToken, nil
}
