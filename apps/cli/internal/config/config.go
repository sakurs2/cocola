package config

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"k8s.io/apimachinery/pkg/api/resource"
)

// CurrentSchemaVersion versions the CLI-owned deployment files independently
// from the Cocola release. Every incompatible config change must add an
// explicit migration before this value is incremented.
const CurrentSchemaVersion = 5

const (
	defaultAgentRuntimeID       = "claude-code"
	defaultRuntimePickerEnabled = "false"
	defaultAgentMaxTurns        = "200"
	defaultToolStepTimeoutSecs  = "600"
	defaultLLMTimeoutSecs       = "600"
	defaultSandboxTokenTTL      = "604800"
	defaultInternalSCMAPIURL    = "http://forgejo:3000"
	defaultInternalSCMHostPort  = 3001
)

var ErrAlreadyInstalled = errors.New("cocola is already installed in this directory")

type Paths struct {
	Home        string
	Environment string
	Compose     string
	State       string
	SandboxRoot string
}

type Options struct {
	Home                   string
	Version                string
	Registry               string
	RegistryExplicit       bool
	PublicURL              string
	AdminUsername          string
	AdminEmail             string
	AdminPassword          string
	WebPort                int
	GatewayPort            int
	LLMPort                int
	ManagedOpenSandbox     bool
	ExternalOpenSandboxURL string
	SandboxLLMBaseURL      string
	SessionVolumeSize      string
	InternalSCM            InternalSCMEndpoint
}

// InternalSCMEndpoint keeps the three network views of the embedded source
// control service together. APIURL is container-internal, HostPort is the
// loopback binding owned by Compose, and SandboxCloneURL is the address a task
// sandbox can actually reach.
type InternalSCMEndpoint struct {
	APIURL          string `json:"api_url"`
	HostPort        int    `json:"host_port"`
	SandboxCloneURL string `json:"sandbox_clone_url"`
}

type State struct {
	ConfigSchemaVersion    int                 `json:"config_schema_version"`
	Version                string              `json:"version"`
	LastSuccessfulVersion  string              `json:"last_successful_version,omitempty"`
	DeploymentRevision     string              `json:"deployment_revision"`
	LastSuccessfulRevision string              `json:"last_successful_revision,omitempty"`
	GHCREndpoint           string              `json:"ghcr_endpoint"`
	ManagedOpenSandbox     bool                `json:"managed_opensandbox"`
	SandboxImage           string              `json:"sandbox_image"`
	ManagedRuntimeImages   []string            `json:"managed_runtime_images"`
	PublicURL              string              `json:"public_url"`
	WebPort                int                 `json:"web_port"`
	GatewayPort            int                 `json:"gateway_port"`
	LLMPort                int                 `json:"llm_port"`
	InternalSCM            InternalSCMEndpoint `json:"internal_scm"`
	PendingUpgrade         *PendingUpgrade     `json:"pending_upgrade,omitempty"`
}

type PendingUpgrade struct {
	FromVersion       string `json:"from_version"`
	ToVersion         string `json:"to_version"`
	FromRevision      string `json:"from_revision,omitempty"`
	ToRevision        string `json:"to_revision"`
	FromImageRegistry string `json:"from_image_registry,omitempty"`
	ToImageRegistry   string `json:"to_image_registry"`
	FromGHCREndpoint  string `json:"from_ghcr_endpoint,omitempty"`
	ToGHCREndpoint    string `json:"to_ghcr_endpoint"`
	BackupDir         string `json:"backup_dir"`
	PreparedAt        string `json:"prepared_at"`
}

type Credentials struct {
	AdminUsername string
	AdminEmail    string
	AdminPassword string
}

type secrets struct {
	auth, authJS, admin, model, config, postgres, minio string
	forgejoDB, forgejo, scm                             string
	openVikingRoot, memoryLLMService                    string
}

func DefaultHome() string {
	if value := strings.TrimSpace(os.Getenv("COCOLA_HOME")); value != "" {
		return value
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".cocola"
	}
	return filepath.Join(home, ".cocola")
}

func Defaults(imageTag string) Options {
	return Options{
		Home: DefaultHome(), Version: imageTag, Registry: DefaultRegistry,
		AdminUsername: "admin", AdminEmail: "admin@cocola.local",
		WebPort: 3000, GatewayPort: 8080, LLMPort: 18091,
		ManagedOpenSandbox: true, SessionVolumeSize: "2Gi",
		InternalSCM: InternalSCMEndpoint{
			APIURL: defaultInternalSCMAPIURL, HostPort: defaultInternalSCMHostPort,
		},
	}
}

func ResolvePaths(home string) (Paths, error) {
	if strings.TrimSpace(home) == "" {
		return Paths{}, errors.New("installation directory is required")
	}
	expanded := home
	if home == "~" || strings.HasPrefix(home, "~/") {
		userHome, err := os.UserHomeDir()
		if err != nil {
			return Paths{}, fmt.Errorf("resolve user home: %w", err)
		}
		expanded = filepath.Join(userHome, strings.TrimPrefix(home, "~/"))
	}
	absolute, err := filepath.Abs(expanded)
	if err != nil {
		return Paths{}, fmt.Errorf("resolve installation directory: %w", err)
	}
	return Paths{
		Home: absolute, Environment: filepath.Join(absolute, "config.env"),
		Compose:     filepath.Join(absolute, "compose.yaml"),
		State:       filepath.Join(absolute, "state.json"),
		SandboxRoot: filepath.Join(absolute, "sandboxes"),
	}, nil
}

func (o Options) Validate() error {
	if _, err := ResolvePaths(o.Home); err != nil {
		return err
	}
	if strings.TrimSpace(o.Version) == "" {
		return errors.New("version is required")
	}
	if !validImagePart(o.Version) {
		return errors.New("version contains characters that are invalid in an image tag")
	}
	registry := o.resolvedImageRegistry()
	if o.RegistryExplicit || strings.TrimSuffix(strings.TrimSpace(o.Registry), "/") != DefaultRegistry {
		registry = o.Registry
	}
	if err := validateImageRegistry(registry); err != nil {
		return err
	}
	if strings.TrimSpace(o.AdminUsername) == "" || strings.ContainsAny(o.AdminUsername, " \t\r\n") {
		return errors.New("admin username is required")
	}
	address, err := mail.ParseAddress(o.AdminEmail)
	if err != nil {
		return fmt.Errorf("invalid admin email: %w", err)
	}
	if address.Address != o.AdminEmail {
		return errors.New("admin email must not include a display name")
	}
	if o.AdminPassword != "" {
		if strings.TrimSpace(o.AdminPassword) == "" {
			return errors.New("admin password cannot be blank")
		}
		if utf8.RuneCountInString(o.AdminPassword) < 8 {
			return errors.New("admin password must contain at least 8 characters")
		}
		if len([]byte(o.AdminPassword)) > 72 {
			return errors.New("admin password cannot exceed 72 bytes")
		}
		if strings.ContainsAny(o.AdminPassword, "\r\n") {
			return errors.New("admin password cannot contain newlines")
		}
	}
	internalSCM := o.resolvedInternalSCM()
	ports := []struct {
		name string
		port int
	}{
		{name: "web", port: o.WebPort},
		{name: "gateway", port: o.GatewayPort},
		{name: "llm gateway", port: o.LLMPort},
		{name: "internal SCM", port: internalSCM.HostPort},
	}
	seen := map[int]string{}
	for _, candidate := range ports {
		if candidate.port < 1 || candidate.port > 65535 {
			return fmt.Errorf("%s port must be between 1 and 65535", candidate.name)
		}
		if previous, exists := seen[candidate.port]; exists {
			return fmt.Errorf(
				"%s and %s cannot use the same port %d",
				previous, candidate.name, candidate.port,
			)
		}
		seen[candidate.port] = candidate.name
	}
	if _, err := o.PublicOrigin(); err != nil {
		return err
	}
	if !o.ManagedOpenSandbox {
		parsed, err := url.ParseRequestURI(o.ExternalOpenSandboxURL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			return errors.New("external OpenSandbox URL must be an absolute http(s) URL")
		}
		if strings.TrimSpace(o.SandboxLLMBaseURL) == "" {
			return errors.New("sandbox LLM base URL is required with external OpenSandbox")
		}
	}
	if o.SandboxLLMBaseURL != "" {
		parsed, err := url.ParseRequestURI(o.SandboxLLMBaseURL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			return errors.New("sandbox LLM base URL must be an absolute http(s) URL")
		}
	}
	if !o.ManagedOpenSandbox && strings.TrimSpace(o.InternalSCM.SandboxCloneURL) == "" {
		return errors.New("sandbox Internal SCM URL is required with external OpenSandbox")
	}
	if err := validateInternalSCMEndpoint(internalSCM); err != nil {
		return err
	}
	quantity, err := resource.ParseQuantity(strings.TrimSpace(o.SessionVolumeSize))
	if err != nil || quantity.Sign() <= 0 || quantity.Value() <= 0 {
		return errors.New("session volume size must be a positive Kubernetes quantity")
	}
	return nil
}

func (o Options) resolvedImageRegistry() string {
	registry := strings.TrimSuffix(strings.TrimSpace(o.Registry), "/")
	if registry == "" {
		return DefaultRegistry
	}
	return registry
}

func (o Options) resolvedInternalSCM() InternalSCMEndpoint {
	value := o.InternalSCM
	if strings.TrimSpace(value.APIURL) == "" {
		value.APIURL = defaultInternalSCMAPIURL
	}
	if value.HostPort == 0 {
		value.HostPort = defaultInternalSCMHostPort
	}
	if strings.TrimSpace(value.SandboxCloneURL) == "" && o.ManagedOpenSandbox {
		value.SandboxCloneURL = fmt.Sprintf("http://host.docker.internal:%d", value.HostPort)
	}
	return value
}

func validateInternalSCMEndpoint(value InternalSCMEndpoint) error {
	endpoints := []struct {
		label string
		raw   string
	}{
		{label: "internal SCM API URL", raw: value.APIURL},
		{label: "sandbox Internal SCM URL", raw: value.SandboxCloneURL},
	}
	for _, endpoint := range endpoints {
		label, raw := endpoint.label, endpoint.raw
		parsed, err := url.ParseRequestURI(strings.TrimSpace(raw))
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") ||
			parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
			return fmt.Errorf("%s must be an absolute http(s) URL without credentials, query, or fragment", label)
		}
	}
	return nil
}

func (o Options) PublicOrigin() (string, error) {
	value := strings.TrimSpace(o.PublicURL)
	if value == "" {
		value = fmt.Sprintf("http://localhost:%d", o.WebPort)
	}
	return NormalizePublicURL(value)
}

func NormalizePublicURL(value string) (string, error) {
	parsed, err := url.ParseRequestURI(strings.TrimSpace(value))
	if err != nil {
		return "", errors.New("public URL must be a browser-reachable http(s) origin without a path, query, credentials, wildcard, or bind-only host")
	}
	scheme := strings.ToLower(parsed.Scheme)
	hostname := parsed.Hostname()
	if parsed.Host == "" || (scheme != "http" && scheme != "https") ||
		parsed.User != nil || (parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" ||
		parsed.Fragment != "" || strings.Contains(hostname, "*") || hostname == "0.0.0.0" || hostname == "::" {
		return "", errors.New("public URL must be a browser-reachable http(s) origin without a path, query, credentials, wildcard, or bind-only host")
	}
	return scheme + "://" + strings.ToLower(parsed.Host), nil
}

func validImagePart(value string) bool {
	if len(value) == 0 || len(value) > 128 {
		return false
	}
	for index, char := range value {
		if index == 0 && !((char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') || char == '_') {
			return false
		}
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') || char == '.' || char == '_' || char == '-' {
			continue
		}
		return false
	}
	return true
}

func WriteInstallation(paths Paths, options Options, compose []byte) (Credentials, error) {
	if _, err := os.Stat(paths.Environment); err == nil {
		return Credentials{}, ErrAlreadyInstalled
	} else if !errors.Is(err, os.ErrNotExist) {
		return Credentials{}, fmt.Errorf("inspect existing installation: %w", err)
	}
	if err := options.Validate(); err != nil {
		return Credentials{}, err
	}
	generated, err := newSecrets()
	if err != nil {
		return Credentials{}, err
	}
	password := options.AdminPassword
	if password == "" {
		password, err = randomSecret(18)
		if err != nil {
			return Credentials{}, err
		}
	}
	if err := os.MkdirAll(paths.Home, 0o700); err != nil {
		return Credentials{}, fmt.Errorf("create installation directory: %w", err)
	}
	if err := os.MkdirAll(paths.SandboxRoot, 0o700); err != nil {
		return Credentials{}, fmt.Errorf("create sandbox directory: %w", err)
	}

	images, err := ResolveImageReferences(ImageResolutionOptions{
		Version: options.Version, CocolaRegistry: options.resolvedImageRegistry(),
		GHCREndpoint: DefaultGHCREndpoint,
	})
	if err != nil {
		return Credentials{}, err
	}
	state := State{
		ConfigSchemaVersion: CurrentSchemaVersion,
		Version:             options.Version, ManagedOpenSandbox: options.ManagedOpenSandbox,
		GHCREndpoint:         DefaultGHCREndpoint,
		SandboxImage:         images.SandboxRuntime,
		ManagedRuntimeImages: images.ManagedRuntimeImages(),
		PublicURL:            publicURLOrDefault(options),
		WebPort:              options.WebPort, GatewayPort: options.GatewayPort, LLMPort: options.LLMPort,
		InternalSCM:        options.resolvedInternalSCM(),
		DeploymentRevision: deploymentRevision(compose),
	}
	stateJSON, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return Credentials{}, fmt.Errorf("encode installation state: %w", err)
	}
	stateJSON = append(stateJSON, '\n')
	if err := atomicWrite(paths.Compose, compose, 0o644); err != nil {
		return Credentials{}, err
	}
	if err := atomicWrite(paths.State, stateJSON, 0o600); err != nil {
		return Credentials{}, err
	}
	environment := renderEnvironment(paths, options, generated, password)
	if err := atomicWrite(paths.Environment, []byte(environment), 0o600); err != nil {
		return Credentials{}, err
	}
	return Credentials{
		AdminUsername: options.AdminUsername,
		AdminEmail:    options.AdminEmail,
		AdminPassword: password,
	}, nil
}

func Load(paths Paths) (State, error) {
	data, err := os.ReadFile(paths.State)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return State{}, fmt.Errorf("cocola is not installed in %s; run cocola install", paths.Home)
		}
		return State{}, fmt.Errorf("read installation state: %w", err)
	}
	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return State{}, fmt.Errorf("decode installation state: %w", err)
	}
	if state.ConfigSchemaVersion > CurrentSchemaVersion {
		return State{}, fmt.Errorf(
			"installation config schema %d is newer than this CLI supports (%d); update the Cocola CLI",
			state.ConfigSchemaVersion,
			CurrentSchemaVersion,
		)
	}
	return state, nil
}

func publicURLOrDefault(options Options) string {
	publicURL, err := options.PublicOrigin()
	if err != nil {
		return fmt.Sprintf("http://localhost:%d", options.WebPort)
	}
	return publicURL
}

func deploymentRevision(compose []byte) string {
	digest := sha256.Sum256(compose)
	return fmt.Sprintf("sha256:%x", digest)
}

func writeState(paths Paths, state State) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode installation state: %w", err)
	}
	return atomicWrite(paths.State, append(data, '\n'), 0o600)
}

func renderEnvironment(paths Paths, o Options, s secrets, password string) string {
	managed := "0"
	opensandboxURL := o.ExternalOpenSandboxURL
	if o.ManagedOpenSandbox {
		managed = "1"
		opensandboxURL = "http://opensandbox-server:8090/v1"
	}
	sandboxLLMBaseURL := o.SandboxLLMBaseURL
	if sandboxLLMBaseURL == "" {
		sandboxLLMBaseURL = fmt.Sprintf("http://host.docker.internal:%d", o.LLMPort)
	}
	publicOrigin, _ := o.PublicOrigin()
	internalSCM := o.resolvedInternalSCM()
	images, _ := ResolveImageReferences(ImageResolutionOptions{
		Version: o.Version, CocolaRegistry: o.resolvedImageRegistry(),
		GHCREndpoint: DefaultGHCREndpoint,
	})
	publicOrigins := []string{
		fmt.Sprintf("http://127.0.0.1:%d", o.WebPort),
		fmt.Sprintf("http://localhost:%d", o.WebPort),
	}
	if publicOrigin != publicOrigins[0] && publicOrigin != publicOrigins[1] {
		publicOrigins = append(publicOrigins, publicOrigin)
	}
	values := [][2]string{
		{"COCOLA_VERSION", o.Version},
		{"COCOLA_IMAGE_REGISTRY", images.Registry},
		{"COCOLA_REDIS_IMAGE", images.Redis}, {"COCOLA_POSTGRES_IMAGE", images.Postgres},
		{"COCOLA_FORGEJO_IMAGE", images.Forgejo}, {"COCOLA_MINIO_IMAGE", images.MinIO},
		{"COCOLA_MINIO_MC_IMAGE", images.MinIOClient}, {"COCOLA_OPENVIKING_IMAGE", images.OpenViking},
		{"COCOLA_OPENSANDBOX_IMAGE", images.OpenSandboxServer},
		{"COCOLA_OPENSANDBOX_EXECD_IMAGE", images.OpenSandboxExecd},
		{"COCOLA_OPENSANDBOX_EGRESS_IMAGE", images.OpenSandboxEgress},
		{"COCOLA_HOME", paths.Home}, {"COCOLA_SANDBOX_ROOT", paths.SandboxRoot},
		{"COCOLA_WEB_HOST", "0.0.0.0"}, {"COCOLA_WEB_HOST_PORT", strconv.Itoa(o.WebPort)},
		{"COCOLA_GATEWAY_HOST_PORT", strconv.Itoa(o.GatewayPort)},
		{"COCOLA_PUBLIC_ORIGINS", strings.Join(publicOrigins, ",")},
		{"COCOLA_LLM_HOST_PORT", strconv.Itoa(o.LLMPort)}, {"COCOLA_OPENSANDBOX_MANAGED", managed},
		{"COCOLA_OPENSANDBOX_URL", opensandboxURL}, {"COCOLA_SANDBOX_LLM_BASE_URL", sandboxLLMBaseURL},
		{"COCOLA_AUTH_SECRET", s.auth},
		{"AUTH_SECRET", s.authJS}, {"COCOLA_ADMIN_KEY", s.admin},
		{"COCOLA_MODEL_SECRET_KEY", s.model}, {"COCOLA_CONFIG_SECRET_KEY", s.config},
		{"COCOLA_PG_PASSWORD", s.postgres}, {"COCOLA_MINIO_ROOT_PASSWORD", s.minio},
		{"COCOLA_OPENVIKING_URL", "http://openviking:1933"},
		{"COCOLA_OPENVIKING_ROOT_API_KEY", s.openVikingRoot},
		{"COCOLA_MEMORY_LLM_SERVICE_TOKEN", s.memoryLLMService},
		{"COCOLA_MEMORY_EMBEDDING_DIMENSION", "1024"},
		{"COCOLA_FORGEJO_DB_PASSWORD", s.forgejoDB}, {"COCOLA_FORGEJO_PASSWORD", s.forgejo},
		{"COCOLA_FORGEJO_HOST_PORT", strconv.Itoa(internalSCM.HostPort)},
		{"COCOLA_FORGEJO_API_URL", internalSCM.APIURL},
		{"COCOLA_FORGEJO_CLONE_URL", internalSCM.SandboxCloneURL},
		{"COCOLA_SCM_SECRET_KEY", s.scm},
		{"COCOLA_SCM_SECRET_KEY_FILE", ""},
		{"COCOLA_SANDBOX_PROJECT_BROKER_URL", fmt.Sprintf("http://host.docker.internal:%d", o.GatewayPort)},
		{"COCOLA_SANDBOX_SKILL_BROKER_URL", fmt.Sprintf("http://host.docker.internal:%d", o.GatewayPort)},
		{"COCOLA_SKILL_PUBLISH_ENABLED", "false"},
		{"COCOLA_PROJECT_MAX_REPOSITORY_MB", "512"},
		{"COCOLA_FEATURE_LOCAL_PROJECTS", "true"},
		{"COCOLA_FEATURE_GITHUB_MANIFEST_CONNECTOR", "true"},
		{"COCOLA_FEATURE_GITHUB_AGENT_WRITE", "true"},
		{"COCOLA_SESSION_VOLUME_SIZE", o.SessionVolumeSize},
		{"COCOLA_SANDBOX_PROFILE", "coding"},
		{"COCOLA_AGENT_RUNTIME_DEFAULT_ID", defaultAgentRuntimeID},
		{"COCOLA_AGENT_RUNTIME_PICKER_ENABLED", defaultRuntimePickerEnabled},
		{"COCOLA_AGENT_MAX_TURNS", defaultAgentMaxTurns},
		{"COCOLA_AGENT_TOOL_STEP_TIMEOUT_SECS", defaultToolStepTimeoutSecs},
		{"COCOLA_LLM_TIMEOUT_SECS", defaultLLMTimeoutSecs},
		{"COCOLA_SANDBOX_TOKEN_TTL_SECONDS", defaultSandboxTokenTTL},
		{"COCOLA_BOOTSTRAP_ADMIN_USERNAME", o.AdminUsername}, {"COCOLA_BOOTSTRAP_ADMIN_EMAIL", o.AdminEmail},
		{"COCOLA_BOOTSTRAP_ADMIN_PASSWORD", password}, {"COCOLA_BOOTSTRAP_ADMIN_RESET", "false"},
	}
	var output strings.Builder
	output.WriteString("# Generated by cocola CLI. Keep this file private.\n")
	for _, item := range values {
		if item[0] == "COCOLA_AGENT_RUNTIME_PICKER_ENABLED" {
			output.WriteString("# Keep this disabled in production. Non-Claude Code runtimes are still experimental and are not ready for general use.\n")
		}
		fmt.Fprintf(&output, "%s=%s\n", item[0], quoteEnv(item[1]))
	}
	return output.String()
}

func quoteEnv(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, "\"", "\\\"")
	value = strings.ReplaceAll(value, "$", "$$")
	value = strings.ReplaceAll(value, "\n", "\\n")
	return "\"" + value + "\""
}

func newSecrets() (secrets, error) {
	values := make([]string, 11)
	for index := range values {
		value, err := randomSecret(32)
		if err != nil {
			return secrets{}, err
		}
		values[index] = value
	}
	scmBytes := make([]byte, 32)
	if _, err := rand.Read(scmBytes); err != nil {
		return secrets{}, fmt.Errorf("generate scm secret: %w", err)
	}
	return secrets{
		auth: values[0], authJS: values[1], admin: values[2], model: values[3],
		config: values[4], postgres: values[5], minio: values[6],
		forgejoDB: values[7], forgejo: values[8],
		openVikingRoot: values[9], memoryLLMService: values[10],
		scm: base64.StdEncoding.EncodeToString(scmBytes),
	}, nil
}

func randomSecret(bytes int) (string, error) {
	buffer := make([]byte, bytes)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("generate secret: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}

func atomicWrite(path string, data []byte, mode os.FileMode) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), ".cocola-*")
	if err != nil {
		return fmt.Errorf("create temporary file for %s: %w", path, err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(mode); err != nil {
		temporary.Close()
		return fmt.Errorf("set permissions on %s: %w", path, err)
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return fmt.Errorf("sync %s: %w", path, err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close %s: %w", path, err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("install %s: %w", path, err)
	}
	return nil
}
