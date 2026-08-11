package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cocola-project/cocola/apps/admin-api/internal/service"
	"github.com/cocola-project/cocola/apps/admin-api/internal/store"
)

type httpModelIconStore struct {
	data map[string][]byte
	mime map[string]string
}

func (s *httpModelIconStore) PutBytes(_ context.Context, key string, data []byte, mime string) error {
	if s.data == nil {
		s.data = map[string][]byte{}
		s.mime = map[string]string{}
	}
	s.data[key] = append([]byte(nil), data...)
	s.mime[key] = mime
	return nil
}

func (s *httpModelIconStore) GetBytes(_ context.Context, key string) ([]byte, string, error) {
	data, ok := s.data[key]
	if !ok {
		return nil, "", store.ErrNotFound
	}
	return append([]byte(nil), data...), s.mime[key], nil
}

func TestModelIconUploadAndReadRoutes(t *testing.T) {
	assets := &httpModelIconStore{}
	svc := service.New(store.NewMemory(), nil, fixedClock).WithModelIconStore(assets)
	router := New(svc, "k").Router()

	var payload bytes.Buffer
	writer := multipart.NewWriter(&payload)
	part, err := writer.CreateFormFile("file", "model.png")
	if err != nil {
		t.Fatalf("create multipart file: %v", err)
	}
	icon := httpTestPNG(t)
	if _, err := part.Write(icon); err != nil {
		t.Fatalf("write multipart file: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/admin/model-icons", &payload)
	req.Header.Set("authorization", "Bearer k")
	req.Header.Set("content-type", writer.FormDataContentType())
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload icon: want 201, got %d (%s)", rec.Code, rec.Body.String())
	}
	var asset service.ModelIconAsset
	if err := json.Unmarshal(rec.Body.Bytes(), &asset); err != nil {
		t.Fatalf("decode upload response: %v", err)
	}
	if !strings.HasPrefix(asset.Source, "/api/model-icons/") {
		t.Fatalf("unexpected icon source: %q", asset.Source)
	}

	rec = do(t, router, http.MethodGet, "/admin/model-icons/"+asset.ID, "k", nil)
	if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), icon) {
		t.Fatalf("read icon: status=%d bytes=%d", rec.Code, rec.Body.Len())
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q", got)
	}
	if got := rec.Header().Get("Cache-Control"); !strings.Contains(got, "immutable") {
		t.Fatalf("Cache-Control = %q", got)
	}
}

func httpTestPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	img.Set(0, 0, color.RGBA{R: 42, G: 125, B: 225, A: 255})
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return out.Bytes()
}
