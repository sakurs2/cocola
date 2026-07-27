package feishu

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

type staticRuntimeCredentialResolver struct {
	credential RuntimeCredential
	err        error
}

func (resolver staticRuntimeCredentialResolver) RuntimeCredential(
	context.Context,
	Identity,
) (RuntimeCredential, error) {
	return resolver.credential, resolver.err
}

type countedBody struct {
	reader io.Reader
	read   int
}

func (body *countedBody) Read(target []byte) (int, error) {
	count, err := body.reader.Read(target)
	body.read += count
	return count, err
}

func (*countedBody) Close() error { return nil }

func TestBoundedDownloaderRejectsContentLengthBeforeReading(t *testing.T) {
	resourceBody := &countedBody{reader: strings.NewReader("oversized")}
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK, Body: resourceBody,
			ContentLength: 9, Header: make(http.Header),
		}, nil
	})}
	downloader := testDownloader(client)
	_, err := downloader.Download(
		context.Background(),
		"message-1",
		Resource{Type: "file", FileKey: "file-1"},
		8,
	)
	if err != ErrAttachmentTooLarge {
		t.Fatalf("Download error = %v", err)
	}
	if resourceBody.read != 0 {
		t.Fatalf("oversized response read %d bytes", resourceBody.read)
	}
}

func TestBoundedDownloaderStopsAtLimitPlusOne(t *testing.T) {
	resourceBody := &countedBody{reader: bytes.NewReader(bytes.Repeat([]byte("x"), 1024))}
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK, Body: resourceBody,
			ContentLength: -1, Header: make(http.Header),
		}, nil
	})}
	downloader := testDownloader(client)
	_, err := downloader.Download(
		context.Background(),
		"message-1",
		Resource{Type: "file", FileKey: "file-1"},
		64,
	)
	if err != ErrAttachmentTooLarge {
		t.Fatalf("Download error = %v", err)
	}
	if resourceBody.read > 65 {
		t.Fatalf("streamed resource read %d bytes, want at most 65", resourceBody.read)
	}
}

func testDownloader(client *http.Client) *BoundedDownloader {
	return &BoundedDownloader{
		identity: Identity{TenantID: "tenant", UserID: "user"},
		credentials: staticRuntimeCredentialResolver{credential: RuntimeCredential{
			Status: RuntimeCredentialReady, Brand: DomainFeishu, TenantAccessToken: "token",
		}},
		baseURL: "https://example.test", http: client,
	}
}

func jsonResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
	}
}
