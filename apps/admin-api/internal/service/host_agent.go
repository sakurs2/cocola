package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const hostAgentKeyHeader = "X-Cocola-Admin-Key"

type hostAgentClient struct {
	baseURL string
	key     string
	http    *http.Client
}

type hostAgentNode struct {
	Name              string            `json:"name"`
	Ready             bool              `json:"ready"`
	DiskPressure      bool              `json:"disk_pressure"`
	CPUCapacity       string            `json:"cpu_capacity"`
	MemoryCapacity    string            `json:"memory_capacity"`
	CPUAllocatable    string            `json:"cpu_allocatable"`
	MemoryAllocatable string            `json:"memory_allocatable"`
	Reason            string            `json:"reason"`
	Labels            map[string]string `json:"labels"`
}

type hostAgentSessionRoot struct {
	Path       string    `json:"path"`
	ModifiedAt time.Time `json:"modified_at"`
}

type hostAgentFilesystem struct {
	NodeName       string    `json:"node_name"`
	TotalBytes     int64     `json:"total_bytes"`
	UsedBytes      int64     `json:"used_bytes"`
	AvailableBytes int64     `json:"available_bytes"`
	MeasuredAt     time.Time `json:"measured_at"`
}

type hostAgentUsage struct {
	NodeName       string    `json:"node_name"`
	AllocatedBytes int64     `json:"allocated_bytes"`
	FileCount      int64     `json:"file_count"`
	DirectoryCount int64     `json:"directory_count"`
	MeasuredAt     time.Time `json:"measured_at"`
}

type hostAgentWorkspaceEntries struct {
	Path       string `json:"path"`
	NextCursor string `json:"next_cursor"`
	Entries    []struct {
		Name        string    `json:"name"`
		Path        string    `json:"path"`
		Kind        string    `json:"kind"`
		Size        int64     `json:"size"`
		ModifiedAt  time.Time `json:"modified_at"`
		Previewable bool      `json:"previewable"`
		PreviewKind string    `json:"preview_kind"`
	} `json:"entries"`
}

type hostAgentStatusError struct {
	StatusCode int
}

func (e *hostAgentStatusError) Error() string {
	return fmt.Sprintf("host agent returned status %d", e.StatusCode)
}

func newHostAgentClientFromEnv() (*hostAgentClient, bool) {
	baseURL := strings.TrimRight(strings.TrimSpace(getenvFirst("COCOLA_HOST_AGENT_URL")), "/")
	if baseURL == "" {
		return nil, false
	}
	return &hostAgentClient{
		baseURL: baseURL,
		key:     strings.TrimSpace(getenvFirst("COCOLA_HOST_AGENT_KEY", "COCOLA_ADMIN_KEY")),
		http:    &http.Client{Timeout: 15 * time.Second},
	}, true
}

func (c *hostAgentClient) Node(ctx context.Context) (hostAgentNode, error) {
	var out hostAgentNode
	err := c.doJSON(ctx, http.MethodGet, "/v1/node", nil, &out)
	return out, err
}

func (c *hostAgentClient) SessionRoots(ctx context.Context) ([]hostAgentSessionRoot, error) {
	var out struct {
		Sessions []hostAgentSessionRoot `json:"sessions"`
	}
	err := c.doJSON(ctx, http.MethodGet, "/v1/session-roots", nil, &out)
	return out.Sessions, err
}

func (c *hostAgentClient) Filesystem(ctx context.Context) (hostAgentFilesystem, error) {
	var out hostAgentFilesystem
	err := c.doJSON(ctx, http.MethodGet, "/v1/filesystem", nil, &out)
	return out, err
}

func (c *hostAgentClient) Usage(ctx context.Context, path string) (hostAgentUsage, error) {
	var out hostAgentUsage
	err := c.doJSON(ctx, http.MethodGet, "/v1/usage?path="+url.QueryEscape(path), nil, &out)
	return out, err
}

func (c *hostAgentClient) DeleteSessionRoot(ctx context.Context, path string) error {
	return c.doJSON(ctx, http.MethodDelete, "/v1/session-root?path="+url.QueryEscape(path), nil, nil)
}

func (c *hostAgentClient) WorkspaceEntries(
	ctx context.Context,
	root, path, cursor string,
) (hostAgentWorkspaceEntries, error) {
	query := url.Values{"root": {root}}
	if path != "" {
		query.Set("path", path)
	}
	if cursor != "" {
		query.Set("cursor", cursor)
	}
	var out hostAgentWorkspaceEntries
	err := c.doJSON(ctx, http.MethodGet, "/v1/workspace/entries?"+query.Encode(), nil, &out)
	return out, err
}

func (c *hostAgentClient) WorkspaceFile(ctx context.Context, root, path string) ([]byte, string, error) {
	query := url.Values{"root": {root}, "path": {path}}
	req, err := c.request(ctx, http.MethodGet, "/v1/workspace/file?"+query.Encode(), nil)
	if err != nil {
		return nil, "", err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", &hostAgentStatusError{StatusCode: resp.StatusCode}
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, (10<<20)+1))
	if err != nil {
		return nil, "", err
	}
	if len(data) > 10<<20 {
		return nil, "", &hostAgentStatusError{StatusCode: http.StatusRequestEntityTooLarge}
	}
	return data, resp.Header.Get("Content-Type"), nil
}

func (c *hostAgentClient) doJSON(ctx context.Context, method, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := c.request(ctx, method, path, reader)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &hostAgentStatusError{StatusCode: resp.StatusCode}
	}
	if out == nil || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(out); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

func (c *hostAgentClient) request(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return nil, err
	}
	if c.key != "" {
		req.Header.Set(hostAgentKeyHeader, c.key)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

func getenvFirst(keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
}
