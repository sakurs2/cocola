package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log"
	"mime"
	"net/http"
	"os"
	"os/signal"
	pathpkg "path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	defaultAddr           = ":8095"
	defaultStorageRoot    = "/storage"
	defaultMeasureTimeout = 10 * time.Second
	workspaceTimeout      = 10 * time.Second
	workspaceConcurrency  = 4
	workspacePageSize     = 200
	maxDirectoryEntries   = 5000
	maxTextPreviewBytes   = 1 << 20
	maxBinaryPreviewBytes = 10 << 20
)

var (
	errDirectoryTooLarge  = errors.New("workspace directory has too many entries")
	errPreviewUnsupported = errors.New("workspace file preview is unsupported")
	errPreviewTooLarge    = errors.New("workspace file is too large to preview")
	errSymlinkUnsupported = errors.New("workspace symlinks are not browsable")
	errInvalidCursor      = errors.New("invalid workspace cursor")
)

type probeServer struct {
	root           string
	nodeName       string
	measureTimeout time.Duration
	measureSlot    chan struct{}
	workspaceSlot  chan struct{}
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

type workspaceEntry struct {
	Name        string    `json:"name"`
	Path        string    `json:"path"`
	Kind        string    `json:"kind"`
	Size        int64     `json:"size"`
	ModifiedAt  time.Time `json:"modified_at"`
	Previewable bool      `json:"previewable"`
	PreviewKind string    `json:"preview_kind,omitempty"`
}

type workspaceEntriesResponse struct {
	Path       string           `json:"path"`
	Entries    []workspaceEntry `json:"entries"`
	NextCursor string           `json:"next_cursor"`
}

type workspaceCursor struct {
	Rank int    `json:"rank"`
	Name string `json:"name"`
}

type workspaceRootHandle struct {
	storage   *os.Root
	workspace *os.Root
}

func (h *workspaceRootHandle) Close() {
	_ = h.workspace.Close()
	_ = h.storage.Close()
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
		measureSlot: make(chan struct{}, 1), workspaceSlot: make(chan struct{}, workspaceConcurrency),
		now: time.Now,
	}, nil
}

func (s *probeServer) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /v1/filesystem", s.filesystem)
	mux.HandleFunc("GET /v1/usage", s.usage)
	mux.HandleFunc("GET /v1/workspace/entries", s.workspaceEntries)
	mux.HandleFunc("GET /v1/workspace/file", s.workspaceFile)
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

func (s *probeServer) workspaceEntries(w http.ResponseWriter, r *http.Request) {
	if !s.reserveWorkspace(w) {
		return
	}
	defer s.releaseWorkspace()

	ctx, cancel := context.WithTimeout(r.Context(), workspaceTimeout)
	defer cancel()
	handle, relative, err := s.openWorkspaceRoot(r.URL.Query().Get("root"), r.URL.Query().Get("path"))
	if err != nil {
		writeWorkspaceError(w, err)
		return
	}
	defer handle.Close()
	root := handle.workspace
	if err := rejectSymlinkPath(root, relative); err != nil {
		writeWorkspaceError(w, err)
		return
	}
	if err := ctx.Err(); err != nil {
		writeWorkspaceError(w, err)
		return
	}
	directory, err := root.Open(relative)
	if err != nil {
		writeWorkspaceError(w, err)
		return
	}
	defer directory.Close()
	info, err := directory.Stat()
	if err != nil {
		writeWorkspaceError(w, err)
		return
	}
	if !info.IsDir() {
		writeWorkspaceError(w, fs.ErrInvalid)
		return
	}
	items, err := directory.ReadDir(maxDirectoryEntries + 1)
	if err != nil && !errors.Is(err, io.EOF) {
		writeWorkspaceError(w, err)
		return
	}
	if len(items) > maxDirectoryEntries {
		writeWorkspaceError(w, errDirectoryTooLarge)
		return
	}
	entries := make([]workspaceEntry, 0, len(items))
	for _, item := range items {
		if err := ctx.Err(); err != nil {
			writeWorkspaceError(w, err)
			return
		}
		entryPath := item.Name()
		if relative != "." {
			entryPath = pathpkg.Join(relative, item.Name())
		}
		itemInfo, err := root.Lstat(entryPath)
		if err != nil {
			writeWorkspaceError(w, err)
			return
		}
		kind := workspaceEntryKind(item)
		previewKind, previewable := workspacePreview(item.Name(), kind, itemInfo.Size())
		entries = append(entries, workspaceEntry{
			Name: item.Name(), Path: entryPath, Kind: kind, Size: itemInfo.Size(),
			ModifiedAt: itemInfo.ModTime().UTC(), Previewable: previewable, PreviewKind: previewKind,
		})
	}
	sort.Slice(entries, func(i, j int) bool { return workspaceEntryLess(entries[i], entries[j]) })
	start, err := workspacePageStart(entries, r.URL.Query().Get("cursor"))
	if err != nil {
		writeWorkspaceError(w, err)
		return
	}
	end := min(start+workspacePageSize, len(entries))
	nextCursor := ""
	if end < len(entries) && end > start {
		nextCursor = encodeWorkspaceCursor(entries[end-1])
	}
	responsePath := ""
	if relative != "." {
		responsePath = relative
	}
	writeJSON(w, http.StatusOK, workspaceEntriesResponse{
		Path: responsePath, Entries: entries[start:end], NextCursor: nextCursor,
	})
}

func (s *probeServer) workspaceFile(w http.ResponseWriter, r *http.Request) {
	if !s.reserveWorkspace(w) {
		return
	}
	defer s.releaseWorkspace()

	ctx, cancel := context.WithTimeout(r.Context(), workspaceTimeout)
	defer cancel()
	handle, relative, err := s.openWorkspaceRoot(r.URL.Query().Get("root"), r.URL.Query().Get("path"))
	if err != nil {
		writeWorkspaceError(w, err)
		return
	}
	defer handle.Close()
	root := handle.workspace
	if relative == "." {
		writeWorkspaceError(w, fs.ErrInvalid)
		return
	}
	if err := rejectSymlinkPath(root, relative); err != nil {
		writeWorkspaceError(w, err)
		return
	}
	info, err := root.Lstat(relative)
	if err != nil {
		writeWorkspaceError(w, err)
		return
	}
	if !info.Mode().IsRegular() {
		writeWorkspaceError(w, errPreviewUnsupported)
		return
	}
	previewKind, previewable := workspacePreview(filepath.Base(relative), "file", info.Size())
	if !previewable {
		if previewKind != "" {
			writeWorkspaceError(w, errPreviewTooLarge)
		} else {
			writeWorkspaceError(w, errPreviewUnsupported)
		}
		return
	}
	limit := workspacePreviewLimit(previewKind)
	file, err := root.Open(relative)
	if err != nil {
		writeWorkspaceError(w, err)
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		writeWorkspaceError(w, err)
		return
	}
	if int64(len(data)) > limit {
		writeWorkspaceError(w, errPreviewTooLarge)
		return
	}
	if err := ctx.Err(); err != nil {
		writeWorkspaceError(w, err)
		return
	}
	w.Header().Set("cache-control", "no-store")
	w.Header().Set("x-content-type-options", "nosniff")
	w.Header().Set("content-disposition", "inline")
	w.Header().Set("content-type", workspaceContentType(previewKind, relative, data))
	w.Header().Set("content-length", strconv.Itoa(len(data)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (s *probeServer) reserveWorkspace(w http.ResponseWriter) bool {
	select {
	case s.workspaceSlot <- struct{}{}:
		return true
	default:
		writeError(w, http.StatusTooManyRequests, "too many workspace requests")
		return false
	}
}

func (s *probeServer) releaseWorkspace() { <-s.workspaceSlot }

func (s *probeServer) openWorkspaceRoot(rootRelative, requestedPath string) (*workspaceRootHandle, string, error) {
	cleanRoot, err := cleanWorkspacePath(rootRelative)
	if err != nil {
		return nil, "", err
	}
	if cleanRoot == "." {
		return nil, "", fs.ErrInvalid
	}
	storageRoot, err := os.OpenRoot(s.root)
	if err != nil {
		return nil, "", err
	}
	if err := rejectSymlinkPath(storageRoot, cleanRoot); err != nil {
		storageRoot.Close()
		return nil, "", err
	}
	root, err := storageRoot.OpenRoot(cleanRoot)
	if err != nil {
		storageRoot.Close()
		return nil, "", err
	}
	relative, err := cleanWorkspacePath(requestedPath)
	if err != nil {
		root.Close()
		storageRoot.Close()
		return nil, "", err
	}
	return &workspaceRootHandle{storage: storageRoot, workspace: root}, relative, nil
}

func cleanWorkspacePath(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ".", nil
	}
	if filepath.IsAbs(raw) || strings.ContainsRune(raw, '\x00') {
		return "", fs.ErrInvalid
	}
	clean := filepath.Clean(raw)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fs.ErrInvalid
	}
	return clean, nil
}

func rejectSymlinkPath(root *os.Root, relative string) error {
	if relative == "." {
		return nil
	}
	parts := strings.Split(relative, string(filepath.Separator))
	current := ""
	for _, part := range parts {
		if current == "" {
			current = part
		} else {
			current = filepath.Join(current, part)
		}
		info, err := root.Lstat(current)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errSymlinkUnsupported
		}
	}
	return nil
}

func workspaceEntryKind(entry fs.DirEntry) string {
	switch {
	case entry.Type()&os.ModeSymlink != 0:
		return "symlink"
	case entry.IsDir():
		return "directory"
	case entry.Type().IsRegular():
		return "file"
	default:
		return "other"
	}
}

func workspacePreview(name, kind string, size int64) (string, bool) {
	if kind != "file" || isSensitiveWorkspaceFile(name) {
		return "", false
	}
	ext := strings.ToLower(filepath.Ext(name))
	previewKind := ""
	switch ext {
	case ".md", ".markdown":
		previewKind = "markdown"
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".svg":
		previewKind = "image"
	case ".pdf":
		previewKind = "pdf"
	case ".bash", ".c", ".cpp", ".css", ".csv", ".diff", ".go", ".h", ".htm", ".html", ".java", ".js", ".jsx", ".json", ".kt", ".log", ".patch", ".py", ".rs", ".sh", ".toml", ".ts", ".tsx", ".txt", ".xml", ".yaml", ".yml", ".zsh":
		previewKind = "code"
	}
	if previewKind == "" {
		return "", false
	}
	return previewKind, size <= workspacePreviewLimit(previewKind)
}

func isSensitiveWorkspaceFile(name string) bool {
	lower := strings.ToLower(strings.TrimSpace(name))
	if strings.HasPrefix(lower, ".env") {
		return true
	}
	if strings.HasSuffix(lower, ".pem") || strings.HasSuffix(lower, ".key") {
		return true
	}
	switch lower {
	case "id_rsa", "id_ed25519", "credentials.json":
		return true
	default:
		return false
	}
}

func workspacePreviewLimit(kind string) int64 {
	if kind == "image" || kind == "pdf" {
		return maxBinaryPreviewBytes
	}
	return maxTextPreviewBytes
}

func workspaceContentType(kind, name string, data []byte) string {
	if kind == "code" || kind == "markdown" {
		return "text/plain; charset=utf-8"
	}
	if kind == "pdf" {
		return "application/pdf"
	}
	if kind == "image" {
		if detected := mime.TypeByExtension(strings.ToLower(filepath.Ext(name))); detected != "" {
			return detected
		}
		return http.DetectContentType(data)
	}
	return "application/octet-stream"
}

func workspaceEntryRank(entry workspaceEntry) int {
	if entry.Kind == "directory" {
		return 0
	}
	return 1
}

func workspaceEntryLess(left, right workspaceEntry) bool {
	leftRank, rightRank := workspaceEntryRank(left), workspaceEntryRank(right)
	if leftRank != rightRank {
		return leftRank < rightRank
	}
	leftName, rightName := strings.ToLower(left.Name), strings.ToLower(right.Name)
	if leftName != rightName {
		return leftName < rightName
	}
	return left.Name < right.Name
}

func encodeWorkspaceCursor(entry workspaceEntry) string {
	raw, _ := json.Marshal(workspaceCursor{Rank: workspaceEntryRank(entry), Name: entry.Name})
	return base64.RawURLEncoding.EncodeToString(raw)
}

func workspacePageStart(entries []workspaceEntry, cursor string) (int, error) {
	cursor = strings.TrimSpace(cursor)
	if cursor == "" {
		return 0, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return 0, errInvalidCursor
	}
	var decoded workspaceCursor
	if json.Unmarshal(raw, &decoded) != nil || decoded.Name == "" || (decoded.Rank != 0 && decoded.Rank != 1) {
		return 0, errInvalidCursor
	}
	for index, entry := range entries {
		if workspaceEntryRank(entry) == decoded.Rank && entry.Name == decoded.Name {
			return index + 1, nil
		}
	}
	return 0, errInvalidCursor
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

func writeWorkspaceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		writeError(w, http.StatusGatewayTimeout, "workspace request timed out")
	case errors.Is(err, fs.ErrNotExist):
		writeError(w, http.StatusNotFound, "workspace path not found")
	case errors.Is(err, fs.ErrPermission):
		writeError(w, http.StatusForbidden, "workspace path is not readable")
	case errors.Is(err, errPreviewTooLarge):
		writeError(w, http.StatusRequestEntityTooLarge, "workspace file is too large to preview")
	case errors.Is(err, errPreviewUnsupported), errors.Is(err, errSymlinkUnsupported):
		writeError(w, http.StatusUnsupportedMediaType, "workspace preview is unsupported")
	case errors.Is(err, errDirectoryTooLarge):
		writeError(w, http.StatusUnprocessableEntity, "workspace directory has too many entries")
	case errors.Is(err, errInvalidCursor), errors.Is(err, fs.ErrInvalid):
		writeError(w, http.StatusBadRequest, "invalid workspace request")
	default:
		writeError(w, http.StatusInternalServerError, "workspace request failed")
	}
}

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
