package service

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type kubeConfig struct {
	Server           string
	BearerToken      string
	CAData           []byte
	ClientCertData   []byte
	ClientKeyData    []byte
	InsecureSkipTLS  bool
	SandboxNamespace string
	PodSelector      string
}

type kubeClient struct {
	cfg  kubeConfig
	http *http.Client
}

func newKubeClient(cfg kubeConfig) *kubeClient {
	tr := &http.Transport{}
	tlsCfg := &tls.Config{InsecureSkipVerify: cfg.InsecureSkipTLS} //nolint:gosec // operator-controlled kubeconfig
	if len(cfg.CAData) > 0 {
		pool := x509.NewCertPool()
		if pool.AppendCertsFromPEM(cfg.CAData) {
			tlsCfg.RootCAs = pool
		}
	}
	if len(cfg.ClientCertData) > 0 && len(cfg.ClientKeyData) > 0 {
		if cert, err := tls.X509KeyPair(cfg.ClientCertData, cfg.ClientKeyData); err == nil {
			tlsCfg.Certificates = []tls.Certificate{cert}
		}
	}
	tr.TLSClientConfig = tlsCfg
	return &kubeClient{cfg: cfg, http: &http.Client{Timeout: 20 * time.Second, Transport: tr}}
}

func kubeConfigFromEnv() (kubeConfig, bool, error) {
	cfg := kubeConfig{
		Server:           strings.TrimRight(os.Getenv("COCOLA_K8S_API_SERVER"), "/"),
		BearerToken:      strings.TrimSpace(os.Getenv("COCOLA_K8S_BEARER_TOKEN")),
		SandboxNamespace: envFirst("COCOLA_SANDBOX_NODE_NAMESPACE", "COCOLA_OPENSANDBOX_K8S_NAMESPACE", "OPENSANDBOX_NAMESPACE"),
		PodSelector:      strings.TrimSpace(os.Getenv("COCOLA_SANDBOX_NODE_POD_SELECTOR")),
		InsecureSkipTLS:  envBool("COCOLA_K8S_INSECURE_SKIP_TLS_VERIFY"),
	}
	if cfg.SandboxNamespace == "" {
		cfg.SandboxNamespace = "opensandbox"
	}
	if cfg.PodSelector == "" {
		cfg.PodSelector = "opensandbox.io/id"
	}
	if caFile := strings.TrimSpace(os.Getenv("COCOLA_K8S_CA_FILE")); caFile != "" {
		b, err := os.ReadFile(expandHome(caFile))
		if err != nil {
			return kubeConfig{}, false, err
		}
		cfg.CAData = b
	}
	if cfg.Server != "" {
		return cfg, true, nil
	}
	if ic, ok, err := inClusterKubeConfig(cfg); ok || err != nil {
		return ic, ok, err
	}
	kc, ok, err := kubeconfigFileConfig(cfg)
	return kc, ok, err
}

func inClusterKubeConfig(base kubeConfig) (kubeConfig, bool, error) {
	host := strings.TrimSpace(os.Getenv("KUBERNETES_SERVICE_HOST"))
	port := strings.TrimSpace(os.Getenv("KUBERNETES_SERVICE_PORT"))
	if host == "" || port == "" {
		return kubeConfig{}, false, nil
	}
	cfg := base
	cfg.Server = "https://" + host + ":" + port
	token, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/token")
	if err != nil {
		return kubeConfig{}, false, err
	}
	cfg.BearerToken = strings.TrimSpace(string(token))
	if ca, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"); err == nil {
		cfg.CAData = ca
	}
	return cfg, true, nil
}

func kubeconfigFileConfig(base kubeConfig) (kubeConfig, bool, error) {
	path := strings.TrimSpace(os.Getenv("COCOLA_KUBECONFIG"))
	if path == "" {
		path = strings.TrimSpace(os.Getenv("KUBECONFIG"))
	}
	if path == "" {
		path = "~/.kube/config"
	}
	first := strings.Split(path, string(os.PathListSeparator))[0]
	raw, err := os.ReadFile(expandHome(first))
	if errors.Is(err, os.ErrNotExist) {
		return kubeConfig{}, false, nil
	}
	if err != nil {
		return kubeConfig{}, false, err
	}
	cfg, err := parseKubeconfig(raw)
	if err != nil {
		return kubeConfig{}, false, err
	}
	cfg.SandboxNamespace = base.SandboxNamespace
	cfg.PodSelector = base.PodSelector
	if base.InsecureSkipTLS {
		cfg.InsecureSkipTLS = true
	}
	return cfg, cfg.Server != "", nil
}

func (c *kubeClient) snapshot(ctx context.Context) ([]kubeNode, []kubePod, error) {
	nodes, err := c.listNodes(ctx)
	if err != nil {
		return nil, nil, err
	}
	pods, err := c.listSandboxPods(ctx)
	if err != nil {
		return nil, nil, err
	}
	return nodes, pods, nil
}

func (c *kubeClient) listNodes(ctx context.Context) ([]kubeNode, error) {
	var out struct {
		Items []kubeNode `json:"items"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/v1/nodes", nil, &out, "application/json"); err != nil {
		return nil, err
	}
	return out.Items, nil
}

func (c *kubeClient) getNode(ctx context.Context, name string) (kubeNode, error) {
	var out kubeNode
	if err := c.do(ctx, http.MethodGet, "/api/v1/nodes/"+url.PathEscape(name), nil, &out, "application/json"); err != nil {
		return kubeNode{}, err
	}
	return out, nil
}

func (c *kubeClient) patchNodeState(ctx context.Context, name string, unschedulable bool, mode string) error {
	var modeValue any = mode
	if mode == "" {
		modeValue = nil
	}
	body := map[string]any{
		"metadata": map[string]any{
			"annotations": map[string]any{
				sandboxNodeModeAnnotation: modeValue,
			},
		},
		"spec": map[string]any{"unschedulable": unschedulable},
	}
	return c.do(ctx, http.MethodPatch, "/api/v1/nodes/"+url.PathEscape(name), body, nil, "application/merge-patch+json")
}

func (c *kubeClient) listSandboxPods(ctx context.Context) ([]kubePod, error) {
	path := "/api/v1/namespaces/" + url.PathEscape(c.cfg.SandboxNamespace) + "/pods"
	q := url.Values{}
	if c.cfg.PodSelector != "" {
		q.Set("labelSelector", c.cfg.PodSelector)
	}
	if enc := q.Encode(); enc != "" {
		path += "?" + enc
	}
	var out struct {
		Items []kubePod `json:"items"`
	}
	if err := c.do(ctx, http.MethodGet, path, nil, &out, "application/json"); err != nil {
		return nil, err
	}
	return out.Items, nil
}

func (c *kubeClient) evictPod(ctx context.Context, namespace, name string) error {
	body := map[string]any{
		"apiVersion": "policy/v1",
		"kind":       "Eviction",
		"metadata": map[string]string{
			"name":      name,
			"namespace": namespace,
		},
	}
	path := "/api/v1/namespaces/" + url.PathEscape(namespace) + "/pods/" + url.PathEscape(name) + "/eviction"
	return c.do(ctx, http.MethodPost, path, body, nil, "application/json")
}

func (c *kubeClient) do(ctx context.Context, method, path string, body any, out any, contentType string) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return ErrInvalidArg
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.cfg.Server+path, rdr)
	if err != nil {
		return ErrInvalidArg
	}
	req.Header.Set("accept", "application/json")
	if body != nil {
		req.Header.Set("content-type", contentType)
	}
	if c.cfg.BearerToken != "" {
		req.Header.Set("authorization", "Bearer "+c.cfg.BearerToken)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("kubernetes api: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return ErrNotFound
	}
	if resp.StatusCode == http.StatusConflict {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("%w: kubernetes conflict: %s", ErrConflict, strings.TrimSpace(string(b)))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("kubernetes api status %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("kubernetes decode: %w", err)
	}
	return nil
}

type kubeNode struct {
	Metadata kubeMeta `json:"metadata"`
	Spec     struct {
		Unschedulable bool `json:"unschedulable"`
	} `json:"spec"`
	Status struct {
		Capacity    map[string]string `json:"capacity"`
		Allocatable map[string]string `json:"allocatable"`
		Conditions  []struct {
			Type    string `json:"type"`
			Status  string `json:"status"`
			Reason  string `json:"reason"`
			Message string `json:"message"`
		} `json:"conditions"`
	} `json:"status"`
}

type kubePod struct {
	Metadata kubeMeta `json:"metadata"`
	Spec     struct {
		NodeName string `json:"nodeName"`
	} `json:"spec"`
	Status struct {
		Phase string `json:"phase"`
	} `json:"status"`
}

type kubeMeta struct {
	Name        string            `json:"name"`
	Namespace   string            `json:"namespace"`
	Labels      map[string]string `json:"labels"`
	Annotations map[string]string `json:"annotations"`
}

type kubeconfigDoc struct {
	CurrentContext string
	Clusters       map[string]map[string]string
	Users          map[string]map[string]string
	Contexts       map[string]map[string]string
}

func parseKubeconfig(raw []byte) (kubeConfig, error) {
	doc := kubeconfigDoc{
		Clusters: map[string]map[string]string{},
		Users:    map[string]map[string]string{},
		Contexts: map[string]map[string]string{},
	}
	section := ""
	var itemName string
	var item map[string]string
	var nested string
	flush := func() {
		if itemName == "" || item == nil {
			return
		}
		switch section {
		case "clusters":
			doc.Clusters[itemName] = item
		case "users":
			doc.Users[itemName] = item
		case "contexts":
			doc.Contexts[itemName] = item
		}
	}
	for _, rawLine := range strings.Split(string(raw), "\n") {
		line := stripYAMLComment(rawLine)
		if strings.TrimSpace(line) == "" {
			continue
		}
		trim := strings.TrimSpace(line)
		if !strings.HasPrefix(rawLine, " ") && !strings.HasPrefix(trim, "- ") && strings.HasSuffix(trim, ":") {
			flush()
			section = strings.TrimSuffix(trim, ":")
			itemName, item, nested = "", nil, ""
			continue
		}
		if strings.HasPrefix(trim, "current-context:") {
			doc.CurrentContext = yamlValue(trim)
			continue
		}
		if section != "clusters" && section != "users" && section != "contexts" {
			continue
		}
		if strings.HasPrefix(trim, "- ") {
			flush()
			itemName = ""
			item = map[string]string{}
			nested = ""
			rest := strings.TrimSpace(strings.TrimPrefix(trim, "- "))
			if strings.HasPrefix(rest, "name:") {
				itemName = yamlValue(rest)
			} else if strings.HasSuffix(rest, ":") {
				nested = strings.TrimSuffix(rest, ":")
			} else if idx := strings.Index(rest, ":"); idx >= 0 {
				key := strings.TrimSpace(rest[:idx])
				item[key] = strings.Trim(strings.TrimSpace(rest[idx+1:]), `"'`)
			}
			continue
		}
		if item == nil {
			continue
		}
		if strings.HasPrefix(trim, "name:") {
			itemName = yamlValue(trim)
			continue
		}
		if strings.HasSuffix(trim, ":") {
			nested = strings.TrimSuffix(trim, ":")
			continue
		}
		if idx := strings.Index(trim, ":"); idx >= 0 {
			key := strings.TrimSpace(trim[:idx])
			val := strings.Trim(strings.TrimSpace(trim[idx+1:]), `"'`)
			if nested != "" {
				key = nested + "." + key
			}
			item[key] = val
		}
	}
	flush()
	ctxName := doc.CurrentContext
	if ctxName == "" && len(doc.Contexts) == 1 {
		for k := range doc.Contexts {
			ctxName = k
		}
	}
	ctx := doc.Contexts[ctxName]
	if ctx == nil {
		return kubeConfig{}, fmt.Errorf("kubeconfig: current context %q not found", ctxName)
	}
	cluster := doc.Clusters[ctx["context.cluster"]]
	user := doc.Users[ctx["context.user"]]
	if cluster == nil {
		return kubeConfig{}, fmt.Errorf("kubeconfig: cluster %q not found", ctx["context.cluster"])
	}
	cfg := kubeConfig{Server: normalizeKubeServer(strings.TrimRight(cluster["cluster.server"], "/"))}
	cfg.CAData = decodeDataOrFile(cluster["cluster.certificate-authority-data"], cluster["cluster.certificate-authority"])
	cfg.BearerToken = user["user.token"]
	cfg.ClientCertData = decodeDataOrFile(user["user.client-certificate-data"], user["user.client-certificate"])
	cfg.ClientKeyData = decodeDataOrFile(user["user.client-key-data"], user["user.client-key"])
	cfg.InsecureSkipTLS = cluster["cluster.insecure-skip-tls-verify"] == "true"
	return cfg, nil
}

func stripYAMLComment(line string) string {
	if idx := strings.Index(line, " #"); idx >= 0 {
		return line[:idx]
	}
	return line
}

func yamlValue(line string) string {
	idx := strings.Index(line, ":")
	if idx < 0 {
		return ""
	}
	return strings.Trim(strings.TrimSpace(line[idx+1:]), `"'`)
}

func decodeDataOrFile(data, file string) []byte {
	if data != "" {
		if b, err := base64.StdEncoding.DecodeString(data); err == nil {
			return b
		}
	}
	if file != "" {
		if b, err := os.ReadFile(expandHome(file)); err == nil {
			return b
		}
	}
	return nil
}

func normalizeKubeServer(server string) string {
	u, err := url.Parse(server)
	if err != nil || u.Hostname() != "0.0.0.0" {
		return server
	}
	host := "127.0.0.1"
	if port := u.Port(); port != "" {
		host += ":" + port
	}
	u.Host = host
	return u.String()
}

func expandHome(path string) string {
	if path == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
	}
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

func envFirst(keys ...string) string {
	for _, k := range keys {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	return ""
}

func envBool(key string) bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}
