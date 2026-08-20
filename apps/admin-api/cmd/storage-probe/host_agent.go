package main

import (
	"bufio"
	"context"
	"crypto/subtle"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const hostAgentKeyHeader = "X-Cocola-Admin-Key"

var componentLogServices = []struct {
	Name    string
	Label   string
	Service string
}{
	{Name: "web.log", Label: "Web", Service: "web"},
	{Name: "gateway.log", Label: "Gateway", Service: "gateway"},
	{Name: "admin-api.log", Label: "Admin API", Service: "admin-api"},
	{Name: "agent-runtime.log", Label: "Agent Runtime", Service: "agent-runtime"},
	{Name: "llm-gateway.log", Label: "LLM Gateway", Service: "llm-gateway"},
	{Name: "sandbox-manager.log", Label: "Sandbox Manager", Service: "sandbox-manager"},
}

type sessionRoot struct {
	Path       string    `json:"path"`
	ModifiedAt time.Time `json:"modified_at"`
}

type nodeInfoResponse struct {
	Name              string            `json:"name"`
	Ready             bool              `json:"ready"`
	DiskPressure      bool              `json:"disk_pressure"`
	CPUCapacity       string            `json:"cpu_capacity"`
	MemoryCapacity    string            `json:"memory_capacity"`
	CPUAllocatable    string            `json:"cpu_allocatable"`
	MemoryAllocatable string            `json:"memory_allocatable"`
	Reason            string            `json:"reason,omitempty"`
	Labels            map[string]string `json:"labels,omitempty"`
}

type componentLogFile struct {
	Name  string `json:"name"`
	Label string `json:"label"`
	Size  int64  `json:"size"`
}

type componentLogsResponse struct {
	Files    []componentLogFile `json:"files"`
	Selected string             `json:"selected"`
	Lines    []string           `json:"lines"`
}

func (s *probeServer) health(w http.ResponseWriter, r *http.Request) {
	if _, err := os.ReadDir(s.root); err != nil {
		writeError(w, http.StatusServiceUnavailable, "storage root is unavailable")
		return
	}
	if s.hostAgentKey != "" {
		if s.docker == nil {
			writeError(w, http.StatusServiceUnavailable, "docker API is unavailable")
			return
		}
		if _, err := s.docker.Info(r.Context()); err != nil {
			writeError(w, http.StatusServiceUnavailable, "docker API is unavailable")
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *probeServer) requireHostAgentKey(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" || s.hostAgentKey == "" {
			next.ServeHTTP(w, r)
			return
		}
		provided := r.Header.Get(hostAgentKeyHeader)
		if subtle.ConstantTimeCompare([]byte(provided), []byte(s.hostAgentKey)) != 1 {
			writeError(w, http.StatusUnauthorized, "host agent authentication required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *probeServer) sessionRoots(w http.ResponseWriter, _ *http.Request) {
	usersRoot := filepath.Join(s.root, "users")
	users, err := os.ReadDir(usersRoot)
	if errors.Is(err, fs.ErrNotExist) {
		writeJSON(w, http.StatusOK, map[string][]sessionRoot{"sessions": {}})
		return
	}
	if err != nil {
		log.Printf("session storage scan failed: path=%q: %v", usersRoot, err)
		writeError(w, http.StatusInternalServerError, "session storage scan failed")
		return
	}
	out := make([]sessionRoot, 0)
	for _, user := range users {
		if !safeDirectoryEntry(user) {
			continue
		}
		sessionsPath := filepath.Join(usersRoot, user.Name(), "sessions")
		sessions, readErr := os.ReadDir(sessionsPath)
		if errors.Is(readErr, fs.ErrNotExist) {
			continue
		}
		if readErr != nil {
			log.Printf("session storage scan failed: path=%q: %v", sessionsPath, readErr)
			writeError(w, http.StatusInternalServerError, "session storage scan failed")
			return
		}
		for _, session := range sessions {
			if !safeDirectoryEntry(session) {
				continue
			}
			info, infoErr := session.Info()
			if infoErr != nil {
				continue
			}
			out = append(out, sessionRoot{
				Path:       filepath.ToSlash(filepath.Join("users", user.Name(), "sessions", session.Name())),
				ModifiedAt: info.ModTime().UTC(),
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	writeJSON(w, http.StatusOK, map[string][]sessionRoot{"sessions": out})
}

func safeDirectoryEntry(entry fs.DirEntry) bool {
	return entry.IsDir() && entry.Type()&os.ModeSymlink == 0 && entry.Name() != "." && entry.Name() != ".."
}

func (s *probeServer) deleteSessionRoot(w http.ResponseWriter, r *http.Request) {
	relative := filepath.FromSlash(strings.TrimSpace(r.URL.Query().Get("path")))
	if !validSessionRootPath(relative) {
		writeError(w, http.StatusBadRequest, "invalid session storage path")
		return
	}
	target, err := s.resolveTarget(relative)
	if errors.Is(err, fs.ErrNotExist) {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid session storage path")
		return
	}
	if err := os.RemoveAll(target); err != nil {
		writeError(w, http.StatusInternalServerError, "session storage deletion failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func validSessionRootPath(relative string) bool {
	if relative == "" || filepath.IsAbs(relative) {
		return false
	}
	clean := filepath.Clean(relative)
	parts := strings.Split(clean, string(filepath.Separator))
	return len(parts) == 4 && parts[0] == "users" && parts[1] != "" && parts[2] == "sessions" && parts[3] != "" &&
		parts[1] != "." && parts[1] != ".." && parts[3] != "." && parts[3] != ".."
}

func (s *probeServer) nodeInfo(w http.ResponseWriter, r *http.Request) {
	name := s.nodeName
	cpu := runtime.NumCPU()
	memoryBytes := readMemoryTotal()
	reason := "Compose host is reachable"
	labels := map[string]string{"cocola.dev/runtime-mode": "compose", "kubernetes.io/arch": runtime.GOARCH}
	if s.docker != nil {
		info, err := s.docker.Info(r.Context())
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "docker host is unavailable")
			return
		}
		if name == "" && strings.TrimSpace(info.Name) != "" {
			name = info.Name
		}
		if strings.TrimSpace(info.Name) != "" {
			labels["cocola.dev/docker-host"] = info.Name
		}
		if info.NCPU > 0 {
			cpu = info.NCPU
		}
		if info.MemTotal > 0 {
			memoryBytes = info.MemTotal
		}
		if info.OperatingSystem != "" {
			labels["cocola.dev/operating-system"] = info.OperatingSystem
		}
	}
	if name == "" {
		name = "local"
	}
	filesystem := s.filesystemSnapshot()
	memory := formatKubeMemory(memoryBytes)
	writeJSON(w, http.StatusOK, nodeInfoResponse{
		Name: name, Ready: true, DiskPressure: filesystem.diskPressure,
		CPUCapacity: strconv.Itoa(cpu), CPUAllocatable: strconv.Itoa(cpu),
		MemoryCapacity: memory, MemoryAllocatable: memory, Reason: reason, Labels: labels,
	})
}

type filesystemSnapshot struct{ diskPressure bool }

func (s *probeServer) filesystemSnapshot() filesystemSnapshot {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(s.root, &stat); err != nil || stat.Blocks <= 0 {
		return filesystemSnapshot{}
	}
	blockSize := int64(stat.Bsize)
	total := int64(stat.Blocks) * blockSize
	available := int64(stat.Bavail) * blockSize
	return filesystemSnapshot{diskPressure: available < 2<<30 || available*100/total < 5}
}

func readMemoryTotal() int64 {
	file, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 2 && fields[0] == "MemTotal:" {
			value, _ := strconv.ParseInt(fields[1], 10, 64)
			return value * 1024
		}
	}
	return 0
}

func formatKubeMemory(bytes int64) string {
	if bytes <= 0 {
		return ""
	}
	return strconv.FormatInt(bytes/1024, 10) + "Ki"
}

func (s *probeServer) componentLogs(w http.ResponseWriter, r *http.Request) {
	if s.docker == nil {
		writeError(w, http.StatusServiceUnavailable, "docker logs are unavailable")
		return
	}
	available := make([]componentLogFile, 0, len(componentLogServices))
	containers := map[string]string{}
	for _, source := range componentLogServices {
		id, err := s.docker.ContainerForService(r.Context(), s.composeProject, source.Service)
		if err != nil {
			writeError(w, http.StatusBadGateway, "docker container lookup failed")
			return
		}
		if id == "" {
			continue
		}
		containers[source.Name] = id
		available = append(available, componentLogFile{Name: source.Name, Label: source.Label})
	}
	selected := strings.TrimSpace(r.URL.Query().Get("file"))
	if _, ok := containers[selected]; !ok {
		selected = ""
		if len(available) > 0 {
			selected = available[0].Name
		}
	}
	lines := []string{}
	if selected != "" {
		limit := clampInt(r.URL.Query().Get("lines"), 1, 2000, 500)
		raw, err := s.docker.Logs(r.Context(), containers[selected], limit)
		if err != nil {
			writeError(w, http.StatusBadGateway, "docker log read failed")
			return
		}
		lines = splitLogLines(raw, limit)
	}
	writeJSON(w, http.StatusOK, componentLogsResponse{Files: available, Selected: selected, Lines: lines})
}

func clampInt(raw string, minValue, maxValue, fallback int) int {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return fallback
	}
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

func splitLogLines(raw []byte, limit int) []string {
	text := strings.TrimRight(string(raw), "\r\n")
	if text == "" {
		return []string{}
	}
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	if len(lines) > limit {
		lines = lines[len(lines)-limit:]
	}
	return lines
}

type dockerInfo struct {
	Name            string `json:"Name"`
	NCPU            int    `json:"NCPU"`
	MemTotal        int64  `json:"MemTotal"`
	OperatingSystem string `json:"OperatingSystem"`
}

type dockerAPI interface {
	Info(ctx context.Context) (dockerInfo, error)
	ContainerForService(ctx context.Context, project, service string) (string, error)
	Logs(ctx context.Context, containerID string, tail int) ([]byte, error)
}

type dockerClient struct{ http *http.Client }

func newDockerClient(socketPath string) (*dockerClient, error) {
	info, err := os.Stat(socketPath)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSocket == 0 {
		return nil, errors.New("docker socket is not a unix socket")
	}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
		},
	}
	return &dockerClient{http: &http.Client{Transport: transport, Timeout: 15 * time.Second}}, nil
}

func (c *dockerClient) Info(ctx context.Context) (dockerInfo, error) {
	var out dockerInfo
	err := c.getJSON(ctx, "/info", &out)
	return out, err
}

func (c *dockerClient) ContainerForService(ctx context.Context, project, service string) (string, error) {
	filters, _ := json.Marshal(map[string][]string{"label": {
		"com.docker.compose.project=" + project,
		"com.docker.compose.service=" + service,
	}})
	var containers []struct {
		ID    string `json:"Id"`
		State string `json:"State"`
	}
	path := "/containers/json?all=1&filters=" + url.QueryEscape(string(filters))
	if err := c.getJSON(ctx, path, &containers); err != nil {
		return "", err
	}
	for _, container := range containers {
		if container.State == "running" {
			return container.ID, nil
		}
	}
	if len(containers) > 0 {
		return containers[0].ID, nil
	}
	return "", nil
}

func (c *dockerClient) Logs(ctx context.Context, containerID string, tail int) ([]byte, error) {
	path := "/containers/" + url.PathEscape(containerID) + "/logs?stdout=1&stderr=1&timestamps=1&tail=" + strconv.Itoa(tail)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://docker"+path, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("docker logs: status %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if strings.HasPrefix(resp.Header.Get("Content-Type"), "application/vnd.docker.raw-stream") || looksLikeDockerMultiplexed(raw) {
		return demuxDockerStream(raw), nil
	}
	return raw, nil
}

func looksLikeDockerMultiplexed(raw []byte) bool {
	frames := 0
	for len(raw) >= 8 {
		if raw[0] < 1 || raw[0] > 3 || raw[1] != 0 || raw[2] != 0 || raw[3] != 0 {
			return false
		}
		length := int(binary.BigEndian.Uint32(raw[4:8]))
		if length > len(raw)-8 {
			return false
		}
		raw = raw[8+length:]
		frames++
	}
	return frames > 0 && len(raw) == 0
}

func (c *dockerClient) getJSON(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://docker"+path, nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("docker API: status %d", resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(out)
}

func demuxDockerStream(raw []byte) []byte {
	out := make([]byte, 0, len(raw))
	for len(raw) >= 8 {
		length := int(binary.BigEndian.Uint32(raw[4:8]))
		if length < 0 || length > len(raw)-8 {
			return raw
		}
		out = append(out, raw[8:8+length]...)
		raw = raw[8+length:]
	}
	if len(raw) > 0 {
		out = append(out, raw...)
	}
	return out
}
