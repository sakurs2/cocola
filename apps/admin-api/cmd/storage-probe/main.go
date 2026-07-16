package main

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const (
	defaultAddr           = ":8095"
	defaultStorageRoot    = "/storage"
	defaultMeasureTimeout = 10 * time.Second
)

type probeServer struct {
	root           string
	nodeName       string
	measureTimeout time.Duration
	measureSlot    chan struct{}
	now            func() time.Time
}

type filesystemResponse struct {
	NodeName       string    `json:"node_name"`
	TotalBytes     int64     `json:"total_bytes"`
	UsedBytes      int64     `json:"used_bytes"`
	AvailableBytes int64     `json:"available_bytes"`
	MeasuredAt     time.Time `json:"measured_at"`
}

type usageResponse struct {
	NodeName       string    `json:"node_name"`
	AllocatedBytes int64     `json:"allocated_bytes"`
	FileCount      int64     `json:"file_count"`
	DirectoryCount int64     `json:"directory_count"`
	MeasuredAt     time.Time `json:"measured_at"`
}

func main() {
	root := strings.TrimSpace(os.Getenv("COCOLA_STORAGE_ROOT"))
	if root == "" {
		root = defaultStorageRoot
	}
	measureTimeout := defaultMeasureTimeout
	if raw := strings.TrimSpace(os.Getenv("COCOLA_STORAGE_MEASURE_TIMEOUT")); raw != "" {
		if parsed, err := time.ParseDuration(raw); err == nil && parsed > 0 {
			measureTimeout = parsed
		}
	}
	probe, err := newProbeServer(root, strings.TrimSpace(os.Getenv("COCOLA_NODE_NAME")), measureTimeout)
	if err != nil {
		log.Fatal(err)
	}

	server := &http.Server{
		Addr:              envOr("COCOLA_STORAGE_PROBE_ADDR", defaultAddr),
		Handler:           probe.handler(),
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      measureTimeout + 5*time.Second,
		IdleTimeout:       30 * time.Second,
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	errCh := make(chan error, 1)
	go func() { errCh <- server.ListenAndServe() }()
	log.Printf("cocola storage probe listening on %s", server.Addr)
	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Printf("storage probe shutdown: %v", err)
		}
	}
}

func newProbeServer(root, nodeName string, measureTimeout time.Duration) (*probeServer, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(absRoot)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, errors.New("storage root is not a directory")
	}
	return &probeServer{
		root: absRoot, nodeName: nodeName, measureTimeout: measureTimeout,
		measureSlot: make(chan struct{}, 1), now: time.Now,
	}, nil
}

func (s *probeServer) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /v1/filesystem", s.filesystem)
	mux.HandleFunc("GET /v1/usage", s.usage)
	return mux
}

func (s *probeServer) filesystem(w http.ResponseWriter, _ *http.Request) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(s.root, &stat); err != nil {
		writeError(w, http.StatusInternalServerError, "filesystem measurement failed")
		return
	}
	blockSize := int64(stat.Bsize)
	total := int64(stat.Blocks) * blockSize
	free := int64(stat.Bfree) * blockSize
	available := int64(stat.Bavail) * blockSize
	writeJSON(w, http.StatusOK, filesystemResponse{
		NodeName: s.nodeName, TotalBytes: total, UsedBytes: total - free,
		AvailableBytes: available, MeasuredAt: s.now().UTC(),
	})
}

func (s *probeServer) usage(w http.ResponseWriter, r *http.Request) {
	select {
	case s.measureSlot <- struct{}{}:
		defer func() { <-s.measureSlot }()
	default:
		writeError(w, http.StatusTooManyRequests, "another storage measurement is running")
		return
	}
	target, err := s.resolveTarget(r.URL.Query().Get("path"))
	if err != nil {
		log.Printf("storage path resolution failed: %v", err)
		writeUsageError(w, err, http.StatusBadRequest, "invalid storage path")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), s.measureTimeout)
	defer cancel()
	result, err := measureUsage(ctx, target)
	if err != nil {
		log.Printf("storage usage measurement failed: %v", err)
		writeUsageError(w, err, http.StatusInternalServerError, "storage measurement failed")
		return
	}
	result.NodeName = s.nodeName
	result.MeasuredAt = s.now().UTC()
	writeJSON(w, http.StatusOK, result)
}

func (s *probeServer) resolveTarget(relative string) (string, error) {
	relative = strings.TrimSpace(relative)
	if relative == "" || filepath.IsAbs(relative) {
		return "", fs.ErrInvalid
	}
	clean := filepath.Clean(relative)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fs.ErrInvalid
	}
	target := filepath.Join(s.root, clean)
	resolvedRoot, err := filepath.EvalSymlinks(s.root)
	if err != nil {
		return "", err
	}
	resolvedTarget, err := filepath.EvalSymlinks(target)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(resolvedRoot, resolvedTarget)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fs.ErrInvalid
	}
	return resolvedTarget, nil
}

func measureUsage(ctx context.Context, root string) (usageResponse, error) {
	var out usageResponse
	err := filepath.WalkDir(root, func(_ string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if stat, ok := info.Sys().(*syscall.Stat_t); ok {
			out.AllocatedBytes += int64(stat.Blocks) * 512
		} else if info.Mode().IsRegular() {
			out.AllocatedBytes += info.Size()
		}
		if entry.IsDir() {
			out.DirectoryCount++
		} else {
			out.FileCount++
		}
		return nil
	})
	return out, err
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("content-type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeUsageError(w http.ResponseWriter, err error, fallbackStatus int, fallbackMessage string) {
	switch {
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		writeError(w, http.StatusGatewayTimeout, "storage measurement timed out")
	case errors.Is(err, fs.ErrNotExist):
		writeError(w, http.StatusNotFound, "storage path not found")
	case errors.Is(err, fs.ErrPermission):
		writeError(w, http.StatusForbidden, "storage path is not readable")
	default:
		writeError(w, fallbackStatus, fallbackMessage)
	}
}

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
