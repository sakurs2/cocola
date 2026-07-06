// Package objstore is the admin-api's thin client to the S3-compatible object
// store (MinIO in dev, any S3 API in prod) for normalized skill bundles.
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

type Store interface {
	PutBytes(ctx context.Context, key string, data []byte, mime string) error
	GetBytes(ctx context.Context, key string) ([]byte, string, error)
	Health(ctx context.Context) error
}

type Config struct {
	Endpoint  string
	AccessKey string
	SecretKey string
	Bucket    string
	UseSSL    bool
}

func ConfigFromEnv() Config {
	return Config{
		Endpoint:  os.Getenv("COCOLA_MINIO_ENDPOINT"),
		AccessKey: os.Getenv("COCOLA_MINIO_ACCESS_KEY"),
		SecretKey: config.SecretFromEnv("COCOLA_MINIO_SECRET_KEY"),
		Bucket:    os.Getenv("COCOLA_MINIO_BUCKET"),
		UseSSL:    os.Getenv("COCOLA_MINIO_USE_SSL") == "1",
	}
}

func (c Config) Enabled() bool {
	return c.Endpoint != "" && c.Bucket != ""
}

type Client struct {
	mc     *minio.Client
	bucket string
}

var _ Store = (*Client)(nil)

func New(cfg Config) (*Client, error) {
	if !cfg.Enabled() {
		return nil, fmt.Errorf("objstore: not configured (endpoint/bucket empty)")
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

func (c *Client) PutBytes(ctx context.Context, key string, data []byte, mime string) error {
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

func (c *Client) GetBytes(ctx context.Context, key string) ([]byte, string, error) {
	obj, err := c.mc.GetObject(ctx, c.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, "", fmt.Errorf("objstore: get %q: %w", key, err)
	}
	defer func() { _ = obj.Close() }()
	info, statErr := obj.Stat()
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(obj); err != nil {
		return nil, "", fmt.Errorf("objstore: read %q: %w", key, err)
	}
	contentType := "application/zip"
	if statErr == nil && info.ContentType != "" {
		contentType = info.ContentType
	}
	return buf.Bytes(), contentType, nil
}

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
