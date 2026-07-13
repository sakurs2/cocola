package objstore

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestConfigFromEnv_ReadsAllFields(t *testing.T) {
	t.Setenv("COCOLA_MINIO_ENDPOINT", "127.0.0.1:9000")
	t.Setenv("COCOLA_MINIO_ACCESS_KEY", "cocola")
	t.Setenv("COCOLA_MINIO_SECRET_KEY", "cocola_dev_pw")
	t.Setenv("COCOLA_MINIO_BUCKET", "cocola")
	t.Setenv("COCOLA_MINIO_USE_SSL", "1")

	cfg := ConfigFromEnv()
	if cfg.Endpoint != "127.0.0.1:9000" || cfg.AccessKey != "cocola" ||
		cfg.SecretKey != "cocola_dev_pw" || cfg.Bucket != "cocola" || !cfg.UseSSL {
		t.Fatalf("ConfigFromEnv mismatch: %+v", cfg)
	}
}

func TestConfigFromEnv_UseSSLDefaultsFalse(t *testing.T) {
	t.Setenv("COCOLA_MINIO_ENDPOINT", "x")
	t.Setenv("COCOLA_MINIO_BUCKET", "b")
	os.Unsetenv("COCOLA_MINIO_USE_SSL")
	if ConfigFromEnv().UseSSL {
		t.Fatal("UseSSL should default to false when unset")
	}
	t.Setenv("COCOLA_MINIO_USE_SSL", "true") // only "1" enables it
	if ConfigFromEnv().UseSSL {
		t.Fatal(`UseSSL should be false unless value is exactly "1"`)
	}
}

func TestConfigFromEnv_SecretFileIndirection(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "secret")
	if err := os.WriteFile(f, []byte("from-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("COCOLA_MINIO_SECRET_KEY", "from-env")
	t.Setenv("COCOLA_MINIO_SECRET_KEY_FILE", f)
	if got := ConfigFromEnv().SecretKey; got != "from-file" {
		t.Fatalf("_FILE indirection: want %q, got %q", "from-file", got)
	}
}

func TestConfigValidate(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
		ok   bool
	}{
		{"complete", Config{Endpoint: "e", AccessKey: "a", SecretKey: "s", Bucket: "b"}, true},
		{"no endpoint", Config{AccessKey: "a", SecretKey: "s", Bucket: "b"}, false},
		{"no access key", Config{Endpoint: "e", SecretKey: "s", Bucket: "b"}, false},
		{"no secret key", Config{Endpoint: "e", AccessKey: "a", Bucket: "b"}, false},
		{"no bucket", Config{Endpoint: "e", AccessKey: "a", SecretKey: "s"}, false},
	}
	for _, c := range cases {
		if err := c.cfg.Validate(); (err == nil) != c.ok {
			t.Errorf("%s: Validate() error=%v, ok=%v", c.name, err, c.ok)
		}
	}
}

func TestNew_ErrorsWhenNotConfigured(t *testing.T) {
	if _, err := New(Config{}); err == nil {
		t.Fatal("New should error when config is incomplete")
	}
}

func TestNew_OKWhenConfigured(t *testing.T) {
	c, err := New(Config{Endpoint: "127.0.0.1:9000", Bucket: "cocola", AccessKey: "k", SecretKey: "s"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if c.bucket != "cocola" {
		t.Fatalf("bucket: want cocola, got %q", c.bucket)
	}
}

// fakeStore proves the Store interface is satisfiable by a test double, which
// is how the chat handler is unit-tested without a live MinIO.
type fakeStore struct {
	objects map[string][]byte
}

func (f *fakeStore) Put(_ context.Context, key string, data []byte, _ string) error {
	f.objects[key] = data
	return nil
}
func (f *fakeStore) Get(_ context.Context, key string) ([]byte, error) {
	return f.objects[key], nil
}
func (f *fakeStore) Health(context.Context) error { return nil }

func TestFakeStoreSatisfiesInterface(t *testing.T) {
	var s Store = &fakeStore{objects: map[string][]byte{}}
	if err := s.Put(context.Background(), "k", []byte("v"), "text/plain"); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(context.Background(), "k")
	if string(got) != "v" {
		t.Fatalf("roundtrip: got %q", got)
	}
}
