// Package objstore is the gateway's thin client to the S3-compatible object
// store (MinIO in dev, any S3 API in prod) that is the source of truth for
// user-uploaded attachments (ADR-0017 P1a).
//
// The gateway owns uploads: on each /v1/chat it PutObjects every attachment,
// then splits by a configurable size threshold — small files are pushed inline
// into the sandbox, large files carry only their object key and are pulled by
// agent-runtime on the model's behalf.
//
// Store is a narrow interface so the HTTP layer depends on an abstraction (real
// MinIO in prod, a fake in tests) rather than the minio-go stubs directly.
//
// Configuration is env-driven (COCOLA_MINIO_*). Object storage is a required
// part of the production chat path: attachments and durable session state must
// not depend on whether a payload happens to fit in an inline gRPC frame.
package objstore

import (
	"bytes"
	"context"
	"fmt"
	"os"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/cocola-project/cocola/packages/go-common/config"
)

// Store is the object-storage seam the gateway depends on. Put is the upload
// side of the source-of-truth write; Get is used by tests and health probes
// (agent-runtime does its own Get in Python). Health lets startup/liveness
// verify the bucket is reachable before serving traffic.
type Store interface {
	Put(ctx context.Context, key string, data []byte, mime string) error
	Get(ctx context.Context, key string) ([]byte, error)
	Health(ctx context.Context) error
}

// Config is the resolved object-store configuration. Endpoint is host:port
// without scheme (minio-go takes UseSSL separately). Bucket is the single
// bucket cocola writes to (auto-created by minio-init in dev).
type Config struct {
	Endpoint  string
	AccessKey string
	SecretKey string
	Bucket    string
	UseSSL    bool
}

// ConfigFromEnv reads the COCOLA_MINIO_* environment. The secret key honours
// the "_FILE" indirection (ADR-0008) so a Vault Agent sidecar can render it to
// disk without the app taking a Vault dependency.
func ConfigFromEnv() Config {
	return Config{
		Endpoint:  os.Getenv("COCOLA_MINIO_ENDPOINT"),
		AccessKey: os.Getenv("COCOLA_MINIO_ACCESS_KEY"),
		SecretKey: config.SecretFromEnv("COCOLA_MINIO_SECRET_KEY"),
		Bucket:    os.Getenv("COCOLA_MINIO_BUCKET"),
		UseSSL:    os.Getenv("COCOLA_MINIO_USE_SSL") == "1",
	}
}

func (c Config) Validate() error {
	switch {
	case c.Endpoint == "":
		return fmt.Errorf("objstore: COCOLA_MINIO_ENDPOINT is required")
	case c.AccessKey == "":
		return fmt.Errorf("objstore: COCOLA_MINIO_ACCESS_KEY is required")
	case c.SecretKey == "":
		return fmt.Errorf("objstore: COCOLA_MINIO_SECRET_KEY is required")
	case c.Bucket == "":
		return fmt.Errorf("objstore: COCOLA_MINIO_BUCKET is required")
	default:
		return nil
	}
}

// Client is the minio-go-backed Store.
type Client struct {
	mc     *minio.Client
	bucket string
}

// compile-time assertion that Client satisfies Store.
var _ Store = (*Client)(nil)

// New constructs a MinIO client from cfg. It does not perform network I/O
// (minio-go connects lazily); call Health to verify reachability. Returns an
// error only on malformed configuration.
func New(cfg Config) (*Client, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	mc, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("objstore: new client: %w", err)
	}
	return &Client{mc: mc, bucket: cfg.Bucket}, nil
}

// Put uploads data under key with the given content type. It overwrites any
// existing object at that key (keys are uuid-prefixed, so collisions are not
// expected in practice).
func (c *Client) Put(ctx context.Context, key string, data []byte, mime string) error {
	opts := minio.PutObjectOptions{}
	if mime != "" {
		opts.ContentType = mime
	}
	_, err := c.mc.PutObject(ctx, c.bucket, key, bytes.NewReader(data), int64(len(data)), opts)
	if err != nil {
		return fmt.Errorf("objstore: put %q: %w", key, err)
	}
	return nil
}

// Get fetches the whole object at key into memory. Attachments are size-capped
// upstream, so a full read is acceptable here.
func (c *Client) Get(ctx context.Context, key string) ([]byte, error) {
	obj, err := c.mc.GetObject(ctx, c.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("objstore: get %q: %w", key, err)
	}
	defer func() { _ = obj.Close() }()
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(obj); err != nil {
		return nil, fmt.Errorf("objstore: read %q: %w", key, err)
	}
	return buf.Bytes(), nil
}

// Health verifies the configured bucket exists and is reachable. Used by
// startup wiring and liveness probes.
func (c *Client) Health(ctx context.Context) error {
	ok, err := c.mc.BucketExists(ctx, c.bucket)
	if err != nil {
		return fmt.Errorf("objstore: health: %w", err)
	}
	if !ok {
		return fmt.Errorf("objstore: bucket %q does not exist", c.bucket)
	}
	return nil
}
