// Package checkpoint decorates a SandboxProvider with reclaim-time snapshots.
package checkpoint

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/cocola-project/cocola/apps/sandbox-manager/internal/provider"
	"github.com/cocola-project/cocola/packages/go-common/config"
)

const (
	defaultTimeoutSecs = 60
	defaultMaxBytes    = 256 * 1024 * 1024
)

var defaultDirs = []string{
	"/home/cocola/.claude",
	"/workspace/uploads",
	"/workspace/outputs",
	"/workspace/persist",
}

// Config controls reclaim-time checkpointing.
type Config struct {
	Dirs        []string
	TimeoutSecs int
	MaxBytes    int

	MinioEndpoint  string
	MinioAccessKey string
	MinioSecretKey string
	MinioBucket    string
	MinioUseSSL    bool
	PGDSN          string
}

// ConfigFromEnv reads COCOLA_SESSION_CHECKPOINT_* plus the existing MinIO/PG env.
func ConfigFromEnv() Config {
	return Config{
		Dirs:           dirsFromEnv(),
		TimeoutSecs:    envInt("COCOLA_SESSION_CHECKPOINT_TIMEOUT_SECS", defaultTimeoutSecs),
		MaxBytes:       envInt("COCOLA_SESSION_CHECKPOINT_MAX_BYTES", defaultMaxBytes),
		MinioEndpoint:  strings.TrimSpace(os.Getenv("COCOLA_MINIO_ENDPOINT")),
		MinioAccessKey: strings.TrimSpace(os.Getenv("COCOLA_MINIO_ACCESS_KEY")),
		MinioSecretKey: config.SecretFromEnv("COCOLA_MINIO_SECRET_KEY"),
		MinioBucket:    strings.TrimSpace(os.Getenv("COCOLA_MINIO_BUCKET")),
		MinioUseSSL:    os.Getenv("COCOLA_MINIO_USE_SSL") == "1",
		PGDSN:          strings.TrimSpace(os.Getenv("COCOLA_PG_DSN")),
	}
}

func (c Config) Validate() error {
	switch {
	case c.MinioEndpoint == "":
		return fmt.Errorf("checkpoint: COCOLA_MINIO_ENDPOINT is required")
	case c.MinioAccessKey == "":
		return fmt.Errorf("checkpoint: COCOLA_MINIO_ACCESS_KEY is required")
	case c.MinioSecretKey == "":
		return fmt.Errorf("checkpoint: COCOLA_MINIO_SECRET_KEY is required")
	case c.MinioBucket == "":
		return fmt.Errorf("checkpoint: COCOLA_MINIO_BUCKET is required")
	case c.PGDSN == "":
		return fmt.Errorf("checkpoint: COCOLA_PG_DSN is required")
	default:
		return nil
	}
}

// Wrap decorates the production provider with required session checkpointing.
func Wrap(base provider.SandboxProvider, cfg Config) (provider.SandboxProvider, error) {
	if base == nil {
		return nil, fmt.Errorf("checkpoint: sandbox provider is required")
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	mc, err := minio.New(cfg.MinioEndpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.MinioAccessKey, cfg.MinioSecretKey, ""),
		Secure: cfg.MinioUseSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("checkpoint: new minio client: %w", err)
	}
	healthCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	exists, healthErr := mc.BucketExists(healthCtx, cfg.MinioBucket)
	cancel()
	if healthErr != nil {
		return nil, fmt.Errorf("checkpoint: minio health: %w", healthErr)
	}
	if !exists {
		return nil, fmt.Errorf("checkpoint: bucket %q does not exist", cfg.MinioBucket)
	}
	return &Provider{SandboxProvider: base, cfg: cfg, minio: mc}, nil
}

// Provider forwards SandboxProvider calls to the wrapped backend and adds the
// optional SessionCheckpointer extension.
type Provider struct {
	provider.SandboxProvider
	cfg   Config
	minio *minio.Client
}

type checkpointObjectCleaner interface {
	ListObjects(ctx context.Context, bucketName string, opts minio.ListObjectsOptions) <-chan minio.ObjectInfo
	RemoveObject(ctx context.Context, bucketName, objectName string, opts minio.RemoveObjectOptions) error
}

var _ provider.SessionCheckpointer = (*Provider)(nil)

// CheckpointSession snapshots key dirs from a live sandbox and records metadata.
func (p *Provider) CheckpointSession(ctx context.Context, userID, sessionID, sandboxID string) error {
	if sandboxID == "" || sessionID == "" {
		return nil
	}
	data, err := p.archive(ctx, sandboxID)
	if err != nil {
		return p.withRecordedFailure(ctx, userID, sessionID, err)
	}
	if len(data) == 0 {
		return nil
	}
	if p.cfg.MaxBytes > 0 && len(data) > p.cfg.MaxBytes {
		err := fmt.Errorf("archive size %d exceeds max %d", len(data), p.cfg.MaxBytes)
		return p.withRecordedFailure(ctx, userID, sessionID, err)
	}
	key := objectKey(userID, sessionID)
	if _, err := p.minio.PutObject(
		ctx,
		p.cfg.MinioBucket,
		key,
		bytes.NewReader(data),
		int64(len(data)),
		minio.PutObjectOptions{ContentType: "application/zstd"},
	); err != nil {
		err = fmt.Errorf("checkpoint upload: %w", err)
		return p.withRecordedFailure(ctx, userID, sessionID, err)
	}
	if err := p.recordSuccess(ctx, userID, sessionID, key, len(data)); err != nil {
		return fmt.Errorf("record checkpoint success: %w", err)
	}
	if err := removeSupersededCheckpointObjects(
		ctx, p.minio, p.cfg.MinioBucket, checkpointPrefix(userID, sessionID), key,
	); err != nil {
		return fmt.Errorf("checkpoint uploaded but old snapshot cleanup failed: %w", err)
	}
	return nil
}

func (p *Provider) withRecordedFailure(ctx context.Context, userID, sessionID string, cause error) error {
	if err := p.recordFailure(ctx, userID, sessionID, cause.Error()); err != nil {
		return errors.Join(cause, fmt.Errorf("record checkpoint failure: %w", err))
	}
	return cause
}

func (p *Provider) archive(ctx context.Context, sandboxID string) ([]byte, error) {
	req := provider.ExecRequest{
		Cmd:     archiveCommand(p.cfg.Dirs),
		Timeout: p.cfg.TimeoutSecs,
	}
	ch, err := p.SandboxProvider.Exec(ctx, sandboxID, req)
	if err != nil {
		return nil, err
	}
	var stdout, stderr strings.Builder
	exitCode := int32(0)
	for ev := range ch {
		switch ev.Kind {
		case provider.ExecEventStdout:
			stdout.Write(ev.Stdout)
		case provider.ExecEventStderr:
			stderr.Write(ev.Stderr)
		case provider.ExecEventExit:
			exitCode = ev.Exit
		case provider.ExecEventError:
			if ev.Err != nil {
				return nil, ev.Err
			}
			return nil, fmt.Errorf("checkpoint archive exec failed")
		}
	}
	if exitCode != 0 {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = fmt.Sprintf("exit %d", exitCode)
		}
		return nil, fmt.Errorf("checkpoint archive failed: %s", msg)
	}
	out := strings.TrimSpace(stdout.String())
	if out == "" {
		return nil, nil
	}
	data, err := base64.StdEncoding.DecodeString(out)
	if err != nil {
		return nil, fmt.Errorf("decode checkpoint archive: %w", err)
	}
	return data, nil
}

func (p *Provider) recordSuccess(ctx context.Context, userID, sessionID, key string, size int) error {
	if userID == "" {
		return fmt.Errorf("session owner is required")
	}
	conn, err := pgx.Connect(ctx, p.cfg.PGDSN)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close(ctx) }()
	tag, err := conn.Exec(ctx, `
INSERT INTO session_map (
    session_id,
    user_id,
    checkpoint_object_key,
    checkpoint_status,
    checkpoint_size_bytes,
    checkpoint_error,
    checkpoint_updated_at,
    updated_at
)
VALUES ($1, $2, $3, 'uploaded', $4, '', now(), now())
ON CONFLICT (session_id)
DO UPDATE SET checkpoint_object_key = EXCLUDED.checkpoint_object_key,
             checkpoint_status = EXCLUDED.checkpoint_status,
             checkpoint_size_bytes = EXCLUDED.checkpoint_size_bytes,
             checkpoint_error = EXCLUDED.checkpoint_error,
             checkpoint_updated_at = EXCLUDED.checkpoint_updated_at,
             updated_at = now()
WHERE session_map.user_id = EXCLUDED.user_id
`, sessionID, userID, key, size)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("session owner mismatch")
	}
	return nil
}

func (p *Provider) recordFailure(ctx context.Context, userID, sessionID, reason string) error {
	if userID == "" {
		return fmt.Errorf("session owner is required")
	}
	conn, err := pgx.Connect(ctx, p.cfg.PGDSN)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close(ctx) }()
	if len(reason) > 1000 {
		reason = reason[:1000]
	}
	tag, err := conn.Exec(ctx, `
INSERT INTO session_map (
    session_id,
    user_id,
    checkpoint_status,
    checkpoint_error,
    checkpoint_updated_at,
    updated_at
)
VALUES ($1, $2, 'failed', $3, now(), now())
ON CONFLICT (session_id)
DO UPDATE SET checkpoint_status = EXCLUDED.checkpoint_status,
             checkpoint_error = EXCLUDED.checkpoint_error,
             checkpoint_updated_at = EXCLUDED.checkpoint_updated_at,
             updated_at = now()
WHERE session_map.user_id = EXCLUDED.user_id
`, sessionID, userID, reason)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("session owner mismatch")
	}
	return nil
}

func archiveCommand(dirs []string) []string {
	var quoted []string
	for _, dir := range dirs {
		rel := strings.TrimPrefix(strings.TrimSpace(dir), "/")
		if rel == "" {
			continue
		}
		quoted = append(quoted, "'"+strings.ReplaceAll(rel, "'", "'\\''")+"'")
	}
	script := "set -eu; paths=''; " +
		"for p in " + strings.Join(quoted, " ") + "; do " +
		"[ -e \"/$p\" ] && paths=\"$paths $p\"; " +
		"done; " +
		"[ -n \"$paths\" ] || exit 0; " +
		"tar -C / -cf - $paths | zstd -q -c | base64 | tr -d '\\n'"
	return []string{"sh", "-lc", script}
}

var keyPartRE = regexp.MustCompile(`[^A-Za-z0-9_.-]+`)

func checkpointPrefix(userID, sessionID string) string {
	return fmt.Sprintf(
		"checkpoints/%s/%s/",
		safeKeyPart(userID, "user"),
		safeKeyPart(sessionID, "session"),
	)
}

func objectKey(userID, sessionID string) string {
	return fmt.Sprintf(
		"%s%s-%s.tar.zst",
		checkpointPrefix(userID, sessionID),
		time.Now().UTC().Format("20060102T150405Z"),
		uuid.NewString(),
	)
}

func removeSupersededCheckpointObjects(
	ctx context.Context,
	store checkpointObjectCleaner,
	bucket, prefix, currentKey string,
) error {
	var cleanupErrors []error
	for object := range store.ListObjects(ctx, bucket, minio.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: true,
	}) {
		if object.Err != nil {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("list checkpoint objects: %w", object.Err))
			continue
		}
		if object.Key == currentKey {
			continue
		}
		if err := store.RemoveObject(ctx, bucket, object.Key, minio.RemoveObjectOptions{}); err != nil {
			cleanupErrors = append(
				cleanupErrors,
				fmt.Errorf("remove checkpoint object %q: %w", object.Key, err),
			)
		}
	}
	return errors.Join(cleanupErrors...)
}

func safeKeyPart(value, fallback string) string {
	clean := strings.Trim(keyPartRE.ReplaceAllString(strings.TrimSpace(value), "-"), "-")
	if clean == "" {
		return fallback
	}
	return clean
}

func dirsFromEnv() []string {
	raw := strings.TrimSpace(os.Getenv("COCOLA_SESSION_CHECKPOINT_DIRS"))
	if raw == "" {
		return append([]string(nil), defaultDirs...)
	}
	var dirs []string
	for _, part := range strings.Split(raw, ",") {
		if dir := strings.TrimSpace(part); dir != "" {
			dirs = append(dirs, dir)
		}
	}
	if len(dirs) == 0 {
		return append([]string(nil), defaultDirs...)
	}
	return dirs
}

func envInt(name string, def int) int {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return def
	}
	return n
}
