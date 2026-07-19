package config

import (
	"crypto/rand"
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

	"k8s.io/apimachinery/pkg/api/resource"
)

const DefaultRegistry = "ghcr.io/sakurs2"

const (
	defaultAgentMaxTurns       = "200"
	defaultToolStepTimeoutSecs = "600"
	defaultLLMTimeoutSecs      = "600"
	defaultSandboxTokenTTL     = "604800"
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
}

type State struct {
	Version            string `json:"version"`
	ManagedOpenSandbox bool   `json:"managed_opensandbox"`
	SandboxImage       string `json:"sandbox_image"`
	WebPort            int    `json:"web_port"`
	GatewayPort        int    `json:"gateway_port"`
}

type Credentials struct {
	AdminUsername string
	AdminEmail    string
	AdminPassword string
}

type secrets struct {
	auth, authJS, admin, model, config, postgres, minio string
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
	if strings.TrimSpace(o.Registry) == "" || strings.ContainsAny(o.Registry, " \t\r\n") ||
		strings.Contains(o.Registry, "://") || strings.HasPrefix(o.Registry, "/") ||
		strings.HasSuffix(o.Registry, "/") || strings.Contains(o.Registry, "//") {
		return errors.New("registry must be a non-empty host/path without whitespace")
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
	if o.AdminPassword != "" && len(o.AdminPassword) < 8 {
		return errors.New("admin password must contain at least 8 characters")
	}
	if strings.ContainsAny(o.AdminPassword, "\r\n") {
		return errors.New("admin password cannot contain newlines")
	}
	ports := map[string]int{"web": o.WebPort, "gateway": o.GatewayPort, "llm gateway": o.LLMPort}
	seen := map[int]string{}
	for name, port := range ports {
		if port < 1 || port > 65535 {
			return fmt.Errorf("%s port must be between 1 and 65535", name)
		}
		if previous, exists := seen[port]; exists {
			return fmt.Errorf("%s and %s cannot use the same port %d", previous, name, port)
		}
		seen[port] = name
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
	quantity, err := resource.ParseQuantity(strings.TrimSpace(o.SessionVolumeSize))
	if err != nil || quantity.Sign() <= 0 || quantity.Value() <= 0 {
		return errors.New("session volume size must be a positive Kubernetes quantity")
	}
	return nil
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

	state := State{
		Version: options.Version, ManagedOpenSandbox: options.ManagedOpenSandbox,
		SandboxImage: strings.TrimSuffix(options.Registry, "/") + "/cocola-sandbox-runtime:" + options.Version,
		WebPort:      options.WebPort, GatewayPort: options.GatewayPort,
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
	return state, nil
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
	values := [][2]string{
		{"COCOLA_VERSION", o.Version}, {"COCOLA_IMAGE_REGISTRY", strings.TrimSuffix(o.Registry, "/")},
		{"COCOLA_HOME", paths.Home}, {"COCOLA_SANDBOX_ROOT", paths.SandboxRoot},
		{"COCOLA_WEB_HOST_PORT", strconv.Itoa(o.WebPort)}, {"COCOLA_GATEWAY_HOST_PORT", strconv.Itoa(o.GatewayPort)},
		{"COCOLA_PUBLIC_ORIGINS", fmt.Sprintf("http://127.0.0.1:%d,http://localhost:%d", o.WebPort, o.WebPort)},
		{"COCOLA_LLM_HOST_PORT", strconv.Itoa(o.LLMPort)}, {"COCOLA_OPENSANDBOX_MANAGED", managed},
		{"COCOLA_OPENSANDBOX_URL", opensandboxURL}, {"COCOLA_SANDBOX_LLM_BASE_URL", sandboxLLMBaseURL},
		{"COCOLA_AUTH_SECRET", s.auth},
		{"AUTH_SECRET", s.authJS}, {"COCOLA_ADMIN_KEY", s.admin},
		{"COCOLA_MODEL_SECRET_KEY", s.model}, {"COCOLA_CONFIG_SECRET_KEY", s.config},
		{"COCOLA_PG_PASSWORD", s.postgres}, {"COCOLA_MINIO_ROOT_PASSWORD", s.minio},
		{"COCOLA_SESSION_VOLUME_SIZE", o.SessionVolumeSize},
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
	values := make([]string, 7)
	for index := range values {
		value, err := randomSecret(32)
		if err != nil {
			return secrets{}, err
		}
		values[index] = value
	}
	return secrets{
		auth: values[0], authJS: values[1], admin: values[2], model: values[3],
		config: values[4], postgres: values[5], minio: values[6],
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
