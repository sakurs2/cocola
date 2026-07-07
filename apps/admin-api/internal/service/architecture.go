package service

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type ArchitectureStatus string

const (
	ArchitectureHealthy   ArchitectureStatus = "healthy"
	ArchitectureDegraded  ArchitectureStatus = "degraded"
	ArchitectureUnhealthy ArchitectureStatus = "unhealthy"
	ArchitectureUnknown   ArchitectureStatus = "unknown"
)

type ArchitectureGraph struct {
	GeneratedAt time.Time          `json:"generated_at"`
	Nodes       []ArchitectureNode `json:"nodes"`
	Edges       []ArchitectureEdge `json:"edges"`
}

type ArchitectureNode struct {
	ID           string             `json:"id"`
	Label        string             `json:"label"`
	Kind         string             `json:"kind"`
	Layer        string             `json:"layer"`
	Status       ArchitectureStatus `json:"status"`
	Detail       string             `json:"detail,omitempty"`
	Endpoint     string             `json:"endpoint,omitempty"`
	AdminHref    string             `json:"admin_href,omitempty"`
	ExternalHref string             `json:"external_href,omitempty"`
	Metadata     map[string]any     `json:"metadata,omitempty"`
}

type ArchitectureEdge struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Label string `json:"label,omitempty"`
	Kind  string `json:"kind,omitempty"`
}

type ArchitectureHealthChecker interface {
	CheckHTTP(ctx context.Context, url string) bool
	CheckTCP(ctx context.Context, addr string) bool
}

type defaultArchitectureHealthChecker struct {
	httpClient *http.Client
	timeout    time.Duration
}

func (a *Admin) Architecture(ctx context.Context) (ArchitectureGraph, error) {
	checker := a.architectureChecker
	if checker == nil {
		checker = defaultArchitectureHealthChecker{
			httpClient: &http.Client{Timeout: 800 * time.Millisecond},
			timeout:    800 * time.Millisecond,
		}
	}
	return ArchitectureGraph{
		GeneratedAt: a.now().UTC(),
		Nodes:       a.architectureNodes(ctx, checker),
		Edges:       architectureEdges(),
	}, nil
}

func (a *Admin) architectureNodes(ctx context.Context, checker ArchitectureHealthChecker) []ArchitectureNode {
	gatewayURL := envURL("COCOLA_GATEWAY_URL", "http://127.0.0.1:8080")
	llmURL := llmGatewayURL()
	openSandboxURL := strings.TrimSpace(os.Getenv("COCOLA_OPENSANDBOX_URL"))
	minioURL := minioHealthBaseURL()

	agentAddr := envString("COCOLA_AGENT_ADDR", "127.0.0.1:50061")
	sandboxAddr := envString("COCOLA_SANDBOX_ADDR", "127.0.0.1:50051")
	redisAddr := strings.TrimSpace(os.Getenv("COCOLA_REDIS_ADDR"))
	pgAddr := postgresAddr(os.Getenv("COCOLA_PG_DSN"))

	gatewayStatus := httpStatus(ctx, checker, healthURL(gatewayURL, "/healthz"), true)
	agentStatus := tcpStatus(ctx, checker, agentAddr)
	llmStatus := httpStatus(ctx, checker, healthURL(llmURL, "/healthz"), llmURL != "")
	sandboxManagerStatus := tcpStatus(ctx, checker, sandboxAddr)
	openSandboxStatus := httpStatus(ctx, checker, openSandboxHealthURL(openSandboxURL), openSandboxURL != "")
	postgresStatus := tcpStatus(ctx, checker, pgAddr)
	redisStatus := tcpStatus(ctx, checker, redisAddr)
	minioStatus := httpStatus(ctx, checker, healthURL(minioURL, "/minio/health/live"), minioURL != "")

	nodes := []ArchitectureNode{
		{
			ID:        "web",
			Label:     "Web",
			Kind:      "frontend",
			Layer:     "Client / UI",
			Status:    ArchitectureHealthy,
			Detail:    "Current admin UI",
			AdminHref: "/admin",
		},
		{
			ID:        "gateway",
			Label:     "Gateway",
			Kind:      "service",
			Layer:     "Control Plane",
			Status:    gatewayStatus,
			Detail:    statusDetail(gatewayStatus, true),
			Endpoint:  displayEndpoint(gatewayURL),
			AdminHref: "/admin/component-logs",
		},
		{
			ID:        "admin-api",
			Label:     "Admin API",
			Kind:      "service",
			Layer:     "Control Plane",
			Status:    ArchitectureHealthy,
			Detail:    "Serving this view",
			AdminHref: "/admin/component-logs",
		},
		{
			ID:        "agent-runtime",
			Label:     "Agent Runtime",
			Kind:      "service",
			Layer:     "Runtime Plane",
			Status:    agentStatus,
			Detail:    statusDetail(agentStatus, agentAddr != ""),
			Endpoint:  agentAddr,
			AdminHref: "/admin/component-logs",
		},
		{
			ID:        "llm-gateway",
			Label:     "LLM Gateway",
			Kind:      "service",
			Layer:     "Runtime Plane",
			Status:    llmStatus,
			Detail:    statusDetail(llmStatus, llmURL != ""),
			Endpoint:  displayEndpoint(llmURL),
			AdminHref: "/admin/models",
		},
		{
			ID:        "sandbox-manager",
			Label:     "Sandbox Manager",
			Kind:      "service",
			Layer:     "Runtime Plane",
			Status:    sandboxManagerStatus,
			Detail:    statusDetail(sandboxManagerStatus, sandboxAddr != ""),
			Endpoint:  sandboxAddr,
			AdminHref: "/admin/component-logs",
		},
		{
			ID:        "opensandbox",
			Label:     "OpenSandbox",
			Kind:      "runtime",
			Layer:     "Sandbox Plane",
			Status:    openSandboxStatus,
			Detail:    statusDetail(openSandboxStatus, openSandboxURL != ""),
			Endpoint:  displayEndpoint(openSandboxURL),
			AdminHref: "/admin/sandbox-nodes",
		},
		a.userSandboxesNode(ctx),
		{
			ID:        "postgres",
			Label:     "Postgres",
			Kind:      "database",
			Layer:     "Infrastructure",
			Status:    postgresStatus,
			Detail:    statusDetail(postgresStatus, pgAddr != ""),
			Endpoint:  pgAddr,
			AdminHref: "/admin/settings",
		},
		{
			ID:        "redis",
			Label:     "Redis",
			Kind:      "cache",
			Layer:     "Infrastructure",
			Status:    redisStatus,
			Detail:    statusDetail(redisStatus, redisAddr != ""),
			Endpoint:  redisAddr,
			AdminHref: "/admin/settings",
		},
		{
			ID:           "minio",
			Label:        "MinIO",
			Kind:         "object-store",
			Layer:        "Infrastructure",
			Status:       minioStatus,
			Detail:       statusDetail(minioStatus, minioURL != ""),
			Endpoint:     displayEndpoint(minioURL),
			AdminHref:    "/admin/settings",
			ExternalHref: minioConsoleURL(),
		},
	}
	return nodes
}

func (a *Admin) userSandboxesNode(ctx context.Context) ArchitectureNode {
	node := ArchitectureNode{
		ID:        "user-sandboxes",
		Label:     "User Sandboxes",
		Kind:      "workload",
		Layer:     "Sandbox Plane",
		Status:    ArchitectureUnknown,
		Detail:    "Runtime inventory not configured",
		AdminHref: "/admin/sandboxes",
		Metadata:  map[string]any{},
	}
	if a.sandboxRuntimes == nil {
		return node
	}
	list, err := a.sandboxRuntimes.ListSandboxes(ctx)
	if err != nil {
		node.Status = ArchitectureUnknown
		node.Detail = "Runtime inventory unavailable"
		return node
	}
	running := 0
	for _, sandbox := range list.Sandboxes {
		if sandbox.Status == "running" || sandbox.LifecycleState == "active" {
			running++
		}
	}
	node.Status = ArchitectureHealthy
	if len(list.Sandboxes) == 0 {
		node.Detail = "No active sandboxes"
	} else {
		node.Detail = strconv.Itoa(running) + " running / " + strconv.Itoa(len(list.Sandboxes)) + " total"
	}
	node.Metadata = map[string]any{
		"running_sandboxes": running,
		"total_sandboxes":   len(list.Sandboxes),
	}
	return node
}

func architectureEdges() []ArchitectureEdge {
	return []ArchitectureEdge{
		{From: "web", To: "gateway", Label: "chat", Kind: "http"},
		{From: "web", To: "admin-api", Label: "admin", Kind: "http"},
		{From: "gateway", To: "agent-runtime", Label: "stream", Kind: "grpc"},
		{From: "gateway", To: "postgres", Label: "history", Kind: "sql"},
		{From: "gateway", To: "minio", Label: "artifacts", Kind: "s3"},
		{From: "admin-api", To: "postgres", Label: "config", Kind: "sql"},
		{From: "admin-api", To: "redis", Label: "events", Kind: "redis"},
		{From: "admin-api", To: "minio", Label: "skills", Kind: "s3"},
		{From: "admin-api", To: "gateway", Label: "scheduled tasks", Kind: "http"},
		{From: "agent-runtime", To: "admin-api", Label: "skills / MCP / prompt", Kind: "http"},
		{From: "agent-runtime", To: "sandbox-manager", Label: "sandbox", Kind: "grpc"},
		{From: "agent-runtime", To: "postgres", Label: "resume", Kind: "sql"},
		{From: "agent-runtime", To: "minio", Label: "attachments", Kind: "s3"},
		{From: "sandbox-manager", To: "opensandbox", Label: "provider", Kind: "http"},
		{From: "opensandbox", To: "user-sandboxes", Label: "creates", Kind: "runtime"},
		{From: "user-sandboxes", To: "llm-gateway", Label: "model API", Kind: "http"},
		{From: "llm-gateway", To: "redis", Label: "quota", Kind: "redis"},
		{From: "llm-gateway", To: "postgres", Label: "models", Kind: "sql"},
	}
}

func (c defaultArchitectureHealthChecker) CheckHTTP(ctx context.Context, rawURL string) bool {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return false
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return false
	}
	client := c.httpClient
	if client == nil {
		client = &http.Client{Timeout: 800 * time.Millisecond}
	}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 400
}

func (c defaultArchitectureHealthChecker) CheckTCP(ctx context.Context, addr string) bool {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return false
	}
	timeout := c.timeout
	if timeout <= 0 {
		timeout = 800 * time.Millisecond
	}
	dialer := net.Dialer{Timeout: timeout}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func httpStatus(ctx context.Context, checker ArchitectureHealthChecker, rawURL string, configured bool) ArchitectureStatus {
	if !configured || strings.TrimSpace(rawURL) == "" {
		return ArchitectureUnknown
	}
	if checker.CheckHTTP(ctx, rawURL) {
		return ArchitectureHealthy
	}
	return ArchitectureUnhealthy
}

func tcpStatus(ctx context.Context, checker ArchitectureHealthChecker, addr string) ArchitectureStatus {
	if strings.TrimSpace(addr) == "" {
		return ArchitectureUnknown
	}
	if checker.CheckTCP(ctx, addr) {
		return ArchitectureHealthy
	}
	return ArchitectureUnhealthy
}

func statusDetail(status ArchitectureStatus, configured bool) string {
	if !configured || status == ArchitectureUnknown {
		return "Not configured"
	}
	if status == ArchitectureHealthy {
		return "Reachable"
	}
	if status == ArchitectureDegraded {
		return "Partially available"
	}
	return "Unreachable"
}

func envString(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func envURL(key, fallback string) string {
	return strings.TrimRight(envString(key, fallback), "/")
}

func llmGatewayURL() string {
	if value := strings.TrimSpace(os.Getenv("COCOLA_LLM_GATEWAY_URL")); value != "" {
		return strings.TrimRight(value, "/")
	}
	if value := strings.TrimSpace(os.Getenv("COCOLA_SANDBOX_LLM_BASE_URL")); value != "" {
		return strings.TrimRight(value, "/")
	}
	host := envString("COCOLA_LLM_HOST", "127.0.0.1")
	port := envString("COCOLA_LLM_PORT", "8081")
	return "http://" + hostPort(host, port)
}

func minioHealthBaseURL() string {
	endpoint := strings.TrimSpace(os.Getenv("COCOLA_MINIO_ENDPOINT"))
	if endpoint == "" {
		return ""
	}
	if strings.HasPrefix(endpoint, "http://") || strings.HasPrefix(endpoint, "https://") {
		return strings.TrimRight(endpoint, "/")
	}
	scheme := "http"
	if os.Getenv("COCOLA_MINIO_USE_SSL") == "1" {
		scheme = "https"
	}
	return scheme + "://" + endpoint
}

func minioConsoleURL() string {
	hostPort := strings.TrimSpace(os.Getenv("COCOLA_MINIO_CONSOLE_URL"))
	if hostPort != "" {
		return hostPort
	}
	if port := strings.TrimSpace(os.Getenv("COCOLA_MINIO_CONSOLE_HOST_PORT")); port != "" {
		return "http://127.0.0.1:" + port
	}
	return ""
}

func openSandboxHealthURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	path := strings.TrimRight(parsed.Path, "/")
	path = strings.TrimRight(strings.TrimSuffix(path, "/v1"), "/")
	parsed.Path = path + "/health"
	return parsed.String()
}

func healthURL(base, suffix string) string {
	base = strings.TrimSpace(base)
	if base == "" {
		return ""
	}
	parsed, err := url.Parse(base)
	if err != nil {
		return ""
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	parsed.Path = strings.TrimRight(parsed.Path, "/") + suffix
	return parsed.String()
}

func displayEndpoint(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return raw
	}
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return strings.TrimRight(parsed.String(), "/")
}

func postgresAddr(dsn string) string {
	dsn = strings.TrimSpace(dsn)
	if dsn == "" {
		return ""
	}
	parsed, err := url.Parse(dsn)
	if err != nil {
		return ""
	}
	host := parsed.Hostname()
	if host == "" {
		return ""
	}
	port := parsed.Port()
	if port == "" {
		port = "5432"
	}
	return net.JoinHostPort(host, port)
}

func hostPort(host, port string) string {
	host = strings.TrimSpace(host)
	port = strings.TrimSpace(port)
	if strings.Contains(host, ":") {
		return host
	}
	return net.JoinHostPort(host, port)
}
