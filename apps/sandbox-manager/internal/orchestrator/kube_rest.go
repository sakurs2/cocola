package orchestrator

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
	return kubeconfigFileConfig(cfg)
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

func (c *kubeClient) listNodes(ctx context.Context) ([]kubeNode, error) {
	var out struct {
		Items []kubeNode `json:"items"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/v1/nodes", nil, &out, "application/json"); err != nil {
		return nil, err
	}
	return out.Items, nil
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

func (c *kubeClient) do(ctx context.Context, method, path string, body any, out any, contentType string) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.cfg.Server+path, rdr)
	if err != nil {
		return err
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
		Conditions []struct {
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
	Clusters       []map[string]map[string]string
	Contexts       []map[string]map[string]string
	Users          []map[string]map[string]string
}

func parseKubeconfig(raw []byte) (kubeConfig, error) {
	doc := kubeconfigDoc{
		Clusters: []map[string]map[string]string{},
		Contexts: []map[string]map[string]string{},
		Users:    []map[string]map[string]string{},
	}
	var section string
	var item map[string]map[string]string
	for _, line := range strings.Split(string(raw), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if !strings.HasPrefix(line, " ") && strings.HasSuffix(trimmed, ":") {
			section = strings.TrimSuffix(trimmed, ":")
			item = nil
			continue
		}
		if !strings.HasPrefix(line, " ") && strings.Contains(trimmed, ":") {
			key, value, _ := strings.Cut(trimmed, ":")
			if key == "current-context" {
				doc.CurrentContext = strings.Trim(strings.TrimSpace(value), `"`)
			}
			continue
		}
		if strings.HasPrefix(strings.TrimLeft(line, " "), "- ") {
			item = map[string]map[string]string{}
			switch section {
			case "clusters":
				doc.Clusters = append(doc.Clusters, item)
			case "contexts":
				doc.Contexts = append(doc.Contexts, item)
			case "users":
				doc.Users = append(doc.Users, item)
			}
			rest := strings.TrimSpace(strings.TrimPrefix(strings.TrimLeft(line, " "), "- "))
			if rest != "" {
				assignKubeconfigLine(item, rest)
			}
			continue
		}
		if item != nil {
			assignKubeconfigLine(item, trimmed)
		}
	}
	ctxName := doc.CurrentContext
	if ctxName == "" {
		return kubeConfig{}, fmt.Errorf("kubeconfig: current-context missing")
	}
	ctx := findNamed(doc.Contexts, ctxName)
	if ctx == nil {
		return kubeConfig{}, fmt.Errorf("kubeconfig: current context %q not found", ctxName)
	}
	cluster := findNamed(doc.Clusters, ctx["context.cluster"])
	if cluster == nil {
		return kubeConfig{}, fmt.Errorf("kubeconfig: cluster %q not found", ctx["context.cluster"])
	}
	cfg := kubeConfig{Server: normalizeKubeServer(strings.TrimRight(cluster["cluster.server"], "/"))}
	if ca := cluster["cluster.certificate-authority-data"]; ca != "" {
		if b, err := base64.StdEncoding.DecodeString(ca); err == nil {
			cfg.CAData = b
		}
	}
	if userName := ctx["context.user"]; userName != "" {
		if user := findNamed(doc.Users, userName); user != nil {
			cfg.BearerToken = user["user.token"]
			if cert := user["user.client-certificate-data"]; cert != "" {
				if b, err := base64.StdEncoding.DecodeString(cert); err == nil {
					cfg.ClientCertData = b
				}
			}
			if key := user["user.client-key-data"]; key != "" {
				if b, err := base64.StdEncoding.DecodeString(key); err == nil {
					cfg.ClientKeyData = b
				}
			}
		}
	}
	return cfg, nil
}

func assignKubeconfigLine(item map[string]map[string]string, line string) {
	key, value, ok := strings.Cut(line, ":")
	if !ok {
		return
	}
	key = strings.TrimSpace(key)
	value = strings.Trim(strings.TrimSpace(value), `"`)
	if key == "name" {
		item["name"] = map[string]string{"": value}
		return
	}
	for group := range item {
		if group != "name" && strings.HasPrefix(key, group+".") {
			item[key] = map[string]string{"": value}
			return
		}
	}
	switch key {
	case "cluster", "context", "user":
		item[key] = map[string]string{}
	default:
		for _, group := range []string{"cluster", "context", "user"} {
			if _, ok := item[group]; ok {
				item[group+"."+key] = map[string]string{"": value}
				return
			}
		}
	}
}

func findNamed(items []map[string]map[string]string, name string) map[string]string {
	for _, item := range items {
		if item["name"][""] == name {
			out := map[string]string{}
			for key, value := range item {
				if key == "name" {
					continue
				}
				if v, ok := value[""]; ok {
					out[key] = v
				}
			}
			return out
		}
	}
	return nil
}

func normalizeKubeServer(server string) string {
	server = strings.TrimSpace(server)
	if server == "" || strings.HasPrefix(server, "http://") || strings.HasPrefix(server, "https://") {
		return server
	}
	return "https://" + server
}

func nodeReady(n kubeNode) bool {
	for _, c := range n.Status.Conditions {
		if c.Type == "Ready" {
			return c.Status == "True"
		}
	}
	return false
}

func isFinishedPod(phase string) bool {
	return phase == "Succeeded" || phase == "Failed"
}

func envFirst(keys ...string) string {
	for _, key := range keys {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			return v
		}
	}
	return ""
}

func envBool(key string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func expandHome(path string) string {
	if path == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
	}
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(path, "~/"))
		}
	}
	return path
}
