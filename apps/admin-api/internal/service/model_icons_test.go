package service

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"testing"

	"github.com/cocola-project/cocola/apps/admin-api/internal/store"
)

type memoryModelIconStore struct {
	data map[string][]byte
	mime map[string]string
}

func (s *memoryModelIconStore) PutBytes(_ context.Context, key string, data []byte, mime string) error {
	if s.data == nil {
		s.data = map[string][]byte{}
		s.mime = map[string]string{}
	}
	s.data[key] = append([]byte(nil), data...)
	s.mime[key] = mime
	return nil
}

func (s *memoryModelIconStore) GetBytes(_ context.Context, key string) ([]byte, string, error) {
	data, ok := s.data[key]
	if !ok {
		return nil, "", store.ErrNotFound
	}
	return append([]byte(nil), data...), s.mime[key], nil
}

func TestModelIconAssetLifecycle(t *testing.T) {
	ctx := context.Background()
	assets := &memoryModelIconStore{}
	svc := New(store.NewMemory(), nil, authTestClock).WithModelIconStore(assets)
	data := testPNG(t, 32, 32)

	asset, err := svc.SaveModelIcon(ctx, data)
	if err != nil {
		t.Fatalf("save model icon: %v", err)
	}
	if asset.ContentType != "image/png" || !validManagedModelIconURL(asset.Source) {
		t.Fatalf("unexpected asset: %+v", asset)
	}
	got, mime, err := svc.GetModelIcon(ctx, asset.ID)
	if err != nil {
		t.Fatalf("get model icon: %v", err)
	}
	if mime != "image/png" || !bytes.Equal(got, data) {
		t.Fatalf("stored icon mismatch: mime=%q bytes=%d", mime, len(got))
	}
	if !validIconConfig(IconImage, "", asset.Source) {
		t.Fatalf("managed icon source rejected: %q", asset.Source)
	}
}

func TestModelIconValidationRejectsUnsafeContent(t *testing.T) {
	assets := &memoryModelIconStore{}
	svc := New(store.NewMemory(), nil, authTestClock).WithModelIconStore(assets)
	for name, data := range map[string][]byte{
		"not an image": []byte("<svg onload=alert(1)></svg>"),
		"too large":    make([]byte, MaxModelIconBytes+1),
		"too wide":     testPNG(t, maxModelIconDimension+1, 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := svc.SaveModelIcon(context.Background(), data); err != ErrInvalidArg {
				t.Fatalf("SaveModelIcon error = %v, want ErrInvalidArg", err)
			}
		})
	}
	if validManagedModelIconURL("/api/model-icons/not-a-hash") {
		t.Fatal("malformed managed icon URL accepted")
	}
}

func testPNG(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	img.Set(0, 0, color.RGBA{R: 42, G: 125, B: 225, A: 255})
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return out.Bytes()
}
