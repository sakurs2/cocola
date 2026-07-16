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
	"strconv"
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

func (c *kubeClient) patchNodeAnnotation(ctx context.Context, name, key string, value any) error {
	body := map[string]any{
		"metadata": map[string]any{
			"annotations": map[string]any{
				key: value,
			},
		},
	}
	return c.do(ctx, http.MethodPatch, "/api/v1/nodes/"+url.PathEscape(name), body, nil, "application/merge-patch+json")
}

func (c *kubeClient) listSandboxPods(ctx context.Context) ([]kubePod, error) {
	return c.listPods(ctx, c.cfg.SandboxNamespace, c.cfg.PodSelector)
}

func (c *kubeClient) listPods(ctx context.Context, namespace, labelSelector string) ([]kubePod, error) {
	path := "/api/v1/namespaces/" + url.PathEscape(namespace) + "/pods"
	q := url.Values{}
	if labelSelector != "" {
		q.Set("labelSelector", labelSelector)
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

// BatchSandbox is the OpenSandbox CRD that owns a sandbox pod. Deleting the pod
// directly is futile (its controller recreates it via ownerReference); the pod
// is only torn down for good by deleting this CRD object, whose name equals the
// sandbox id.
const (
	batchSandboxGroup   = "sandbox.opensandbox.io"
	batchSandboxVersion = "v1alpha1"
	batchSandboxPlural  = "batchsandboxes"
)

// deleteBatchSandbox removes the BatchSandbox CRD object named after the sandbox
// id; Kubernetes garbage-collects the owned pod. A missing object surfaces as
// ErrNotFound (mapped by c.do), which callers treat as already-gone.
func (c *kubeClient) deleteBatchSandbox(ctx context.Context, namespace, name string) error {
	path := "/apis/" + batchSandboxGroup + "/" + batchSandboxVersion +
		"/namespaces/" + url.PathEscape(namespace) + "/" + batchSandboxPlural + "/" + url.PathEscape(name)
	return c.do(ctx, http.MethodDelete, path, nil, nil, "application/json")
}

type sessionPVC struct {
	Name           string
	Namespace      string
	Phase          string
	VolumeName     string
	StorageClass   string
	StorageID      string
	Generation     int64
	NodeName       string
	RequestedBytes int64
}

func (c *kubeClient) listSessionPVCs(ctx context.Context, namespace string) ([]sessionPVC, error) {
	path := "/api/v1/namespaces/" + url.PathEscape(namespace) + "/persistentvolumeclaims"
	q := url.Values{}
	q.Set("labelSelector", "app.kubernetes.io/managed-by=cocola")
	path += "?" + q.Encode()
	var out struct {
		Items []struct {
			Metadata kubeMeta `json:"metadata"`
			Spec     struct {
				VolumeName       string `json:"volumeName"`
				StorageClassName string `json:"storageClassName"`
			} `json:"spec"`
			Status struct {
				Phase string `json:"phase"`
			} `json:"status"`
		} `json:"items"`
	}
	if err := c.do(ctx, http.MethodGet, path, nil, &out, "application/json"); err != nil {
		return nil, err
	}
	pvcs := make([]sessionPVC, 0, len(out.Items))
	for _, pvc := range out.Items {
		if pvc.Metadata.Labels["app.kubernetes.io/managed-by"] == "cocola" {
			phase := pvc.Status.Phase
			if pvc.Metadata.DeletionTimestamp != nil {
				phase = "Terminating"
			}
			generation, _ := strconv.ParseInt(pvc.Metadata.Labels["cocola.dev/generation"], 10, 64)
			requestedBytes, _ := strconv.ParseInt(pvc.Metadata.Labels["cocola.dev/requested-bytes"], 10, 64)
			pvcs = append(pvcs, sessionPVC{
				Name: pvc.Metadata.Name, Namespace: pvc.Metadata.Namespace, Phase: phase,
				VolumeName: pvc.Spec.VolumeName, StorageClass: pvc.Spec.StorageClassName,
				StorageID:  pvc.Metadata.Labels["cocola.dev/storage-id"],
				Generation: generation, NodeName: pvc.Metadata.Labels["cocola.dev/node-name"],
				RequestedBytes: requestedBytes,
			})
		}
	}
	return pvcs, nil
}

func (c *kubeClient) getSessionPVC(ctx context.Context, namespace, name string) (sessionPVC, bool, error) {
	path := "/api/v1/namespaces/" + url.PathEscape(namespace) + "/persistentvolumeclaims/" + url.PathEscape(name)
	var out struct {
		Metadata kubeMeta `json:"metadata"`
		Spec     struct {
			VolumeName       string `json:"volumeName"`
			StorageClassName string `json:"storageClassName"`
		} `json:"spec"`
		Status struct {
			Phase string `json:"phase"`
		} `json:"status"`
	}
	if err := c.do(ctx, http.MethodGet, path, nil, &out, "application/json"); err != nil {
		if errors.Is(err, ErrNotFound) {
			return sessionPVC{}, false, nil
		}
		return sessionPVC{}, false, err
	}
	phase := out.Status.Phase
	if out.Metadata.DeletionTimestamp != nil {
		phase = "Terminating"
	}
	generation, _ := strconv.ParseInt(out.Metadata.Labels["cocola.dev/generation"], 10, 64)
	requestedBytes, _ := strconv.ParseInt(out.Metadata.Labels["cocola.dev/requested-bytes"], 10, 64)
	return sessionPVC{
		Name: out.Metadata.Name, Namespace: out.Metadata.Namespace, Phase: phase,
		VolumeName: out.Spec.VolumeName, StorageClass: out.Spec.StorageClassName,
		StorageID:  out.Metadata.Labels["cocola.dev/storage-id"],
		Generation: generation, NodeName: out.Metadata.Labels["cocola.dev/node-name"],
		RequestedBytes: requestedBytes,
	}, true, nil
}

type sessionPV struct {
	Name         string
	StorageClass string
	LocalPath    string
}

func (c *kubeClient) getSessionPV(ctx context.Context, name string) (sessionPV, error) {
	var out struct {
		Metadata kubeMeta `json:"metadata"`
		Spec     struct {
			StorageClassName string `json:"storageClassName"`
			Local            *struct {
				Path string `json:"path"`
			} `json:"local"`
			HostPath *struct {
				Path string `json:"path"`
			} `json:"hostPath"`
		} `json:"spec"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/v1/persistentvolumes/"+url.PathEscape(name), nil, &out, "application/json"); err != nil {
		return sessionPV{}, err
	}
	path := ""
	if out.Spec.Local != nil {
		path = out.Spec.Local.Path
	} else if out.Spec.HostPath != nil {
		path = out.Spec.HostPath.Path
	}
	return sessionPV{Name: out.Metadata.Name, StorageClass: out.Spec.StorageClassName, LocalPath: path}, nil
}

type storageProbeFilesystem struct {
	NodeName       string    `json:"node_name"`
	TotalBytes     int64     `json:"total_bytes"`
	UsedBytes      int64     `json:"used_bytes"`
	AvailableBytes int64     `json:"available_bytes"`
	MeasuredAt     time.Time `json:"measured_at"`
}

type storageProbeUsage struct {
	NodeName       string    `json:"node_name"`
	AllocatedBytes int64     `json:"allocated_bytes"`
	FileCount      int64     `json:"file_count"`
	DirectoryCount int64     `json:"directory_count"`
	MeasuredAt     time.Time `json:"measured_at"`
}

func (c *kubeClient) storageProbeFilesystem(ctx context.Context, namespace, podName string) (storageProbeFilesystem, error) {
	var out storageProbeFilesystem
	err := c.do(ctx, http.MethodGet, c.storageProbeProxyPath(namespace, podName, "/v1/filesystem", nil), nil, &out, "application/json")
	return out, err
}

func (c *kubeClient) storageProbeUsage(ctx context.Context, namespace, podName, relativePath string) (storageProbeUsage, error) {
	query := url.Values{"path": []string{relativePath}}
	var out storageProbeUsage
	err := c.do(ctx, http.MethodGet, c.storageProbeProxyPath(namespace, podName, "/v1/usage", query), nil, &out, "application/json")
	return out, err
}

func (c *kubeClient) storageProbeProxyPath(namespace, podName, endpoint string, query url.Values) string {
	path := "/api/v1/namespaces/" + url.PathEscape(namespace) + "/pods/" + url.PathEscape(podName) + ":8095/proxy" + endpoint
	if encoded := query.Encode(); encoded != "" {
		path += "?" + encoded
	}
	return path
}

func (c *kubeClient) deletePVC(ctx context.Context, namespace, name string) error {
	path := "/api/v1/namespaces/" + url.PathEscape(namespace) + "/persistentvolumeclaims/" + url.PathEscape(name)
	body := map[string]any{"propagationPolicy": "Foreground"}
	err := c.do(ctx, http.MethodDelete, path, body, nil, "application/json")
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	return err
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
	Name              string            `json:"name"`
	Namespace         string            `json:"namespace"`
	Labels            map[string]string `json:"labels"`
	Annotations       map[string]string `json:"annotations"`
	DeletionTimestamp *time.Time        `json:"deletionTimestamp"`
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
