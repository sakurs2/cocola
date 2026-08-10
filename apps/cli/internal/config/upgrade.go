package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	backupDirectoryName    = "backups"
	legacyCNMirrorRegistry = "ghcr.nju.edu.cn/sakurs2"
)

var environmentKeyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

type UpgradeResult struct {
	Updated           bool   `json:"updated"`
	FromVersion       string `json:"from_version"`
	ToVersion         string `json:"to_version"`
	FromImageRegistry string `json:"from_image_registry"`
	ToImageRegistry   string `json:"to_image_registry"`
	BackupDir         string `json:"backup_dir,omitempty"`
}

type UpgradeOptions struct {
	Version  string
	Registry *string
}

type backupManifest struct {
	Existing map[string]bool `json:"existing"`
}

type environmentDocument struct {
	lines   []string
	values  map[string]string
	indices map[string][]int
}

// PrepareUpgrade migrates CLI-owned deployment files without changing running
// containers. It preserves operator-owned values, replaces only CLI-managed
// deployment and image values, and records an exact backup for start-time rollback.
func PrepareUpgrade(paths Paths, targetVersion string, compose []byte) (UpgradeResult, error) {
	return PrepareUpgradeWithOptions(paths, UpgradeOptions{Version: targetVersion}, compose)
}

func PrepareUpgradeWithOptions(paths Paths, options UpgradeOptions, compose []byte) (UpgradeResult, error) {
	targetVersion := strings.TrimSpace(options.Version)
	if !validImagePart(targetVersion) {
		return UpgradeResult{}, errors.New("target version contains characters that are invalid in an image tag")
	}
	if options.Registry != nil {
		if err := validateImageRegistry(*options.Registry); err != nil {
			return UpgradeResult{}, err
		}
	}
	state, err := Load(paths)
	if err != nil {
		return UpgradeResult{}, err
	}
	targetRevision := deploymentRevision(compose)
	if state.PendingUpgrade != nil {
		pending := state.PendingUpgrade
		requestedRegistryMatches := options.Registry == nil || pending.ToImageRegistry == strings.TrimSuffix(strings.TrimSpace(*options.Registry), "/")
		if state.ConfigSchemaVersion == CurrentSchemaVersion && pending.ToImageRegistry != "" &&
			pending.ToVersion == targetVersion && pending.ToRevision == targetRevision && requestedRegistryMatches {
			return UpgradeResult{
				Updated: true, FromVersion: pending.FromVersion,
				ToVersion: pending.ToVersion, FromImageRegistry: pending.FromImageRegistry,
				ToImageRegistry: pending.ToImageRegistry, BackupDir: pending.BackupDir,
			}, nil
		}
		if _, err := RollbackUpgrade(paths); err != nil {
			return UpgradeResult{}, fmt.Errorf("replace pending upgrade: %w", err)
		}
		state, err = Load(paths)
		if err != nil {
			return UpgradeResult{}, err
		}
	}

	environment, err := os.ReadFile(paths.Environment)
	if err != nil {
		return UpgradeResult{}, fmt.Errorf("read deployment environment: %w", err)
	}
	migratedEnvironment, derived, err := migrateEnvironmentWithOptions(paths, state, targetVersion, options.Registry, environment)
	if err != nil {
		return UpgradeResult{}, err
	}
	fromVersion := strings.TrimSpace(state.LastSuccessfulVersion)
	if fromVersion == "" {
		fromVersion = strings.TrimSpace(state.Version)
	}
	if fromVersion == "" {
		fromVersion = derived.version
	}
	fromRevision := state.LastSuccessfulRevision
	if fromRevision == "" {
		fromRevision = state.DeploymentRevision
	}
	if fromRevision == "" {
		currentCompose, readErr := os.ReadFile(paths.Compose)
		if readErr == nil {
			fromRevision = deploymentRevision(currentCompose)
		}
	}

	needsUpdate := state.ConfigSchemaVersion != CurrentSchemaVersion ||
		state.Version != targetVersion || state.DeploymentRevision != targetRevision ||
		state.SandboxImage != derived.images.SandboxRuntime ||
		!slices.Equal(state.ManagedRuntimeImages, derived.images.ManagedRuntimeImages()) ||
		!bytes.Equal(environment, migratedEnvironment) || !fileEquals(paths.Compose, compose)
	if !needsUpdate {
		return UpgradeResult{
			FromVersion: fromVersion, ToVersion: targetVersion,
			FromImageRegistry: derived.currentRegistry, ToImageRegistry: derived.images.Registry,
		}, nil
	}

	backupDir, err := createBackup(paths, fromVersion, targetVersion)
	if err != nil {
		return UpgradeResult{}, err
	}
	state.ConfigSchemaVersion = CurrentSchemaVersion
	state.Version = targetVersion
	state.LastSuccessfulVersion = fromVersion
	state.DeploymentRevision = targetRevision
	state.LastSuccessfulRevision = fromRevision
	state.ManagedOpenSandbox = derived.managedOpenSandbox
	state.SandboxImage = derived.images.SandboxRuntime
	state.ManagedRuntimeImages = derived.images.ManagedRuntimeImages()
	state.PublicURL = derived.publicURL
	state.WebPort = derived.webPort
	state.GatewayPort = derived.gatewayPort
	state.LLMPort = derived.llmPort
	state.InternalSCM = derived.internalSCM
	state.PendingUpgrade = &PendingUpgrade{
		FromVersion: fromVersion, ToVersion: targetVersion,
		FromRevision: fromRevision, ToRevision: targetRevision,
		FromImageRegistry: derived.currentRegistry, ToImageRegistry: derived.images.Registry,
		BackupDir: backupDir, PreparedAt: time.Now().UTC().Format(time.RFC3339),
	}

	writeErr := writeUpgradeFiles(paths, compose, migratedEnvironment, state)
	if writeErr != nil {
		if restoreErr := restoreBackup(paths, backupDir); restoreErr != nil {
			return UpgradeResult{}, errors.Join(writeErr, fmt.Errorf("restore deployment backup: %w", restoreErr))
		}
		return UpgradeResult{}, writeErr
	}
	return UpgradeResult{
		Updated: true, FromVersion: fromVersion, ToVersion: targetVersion,
		FromImageRegistry: derived.currentRegistry, ToImageRegistry: derived.images.Registry,
		BackupDir: backupDir,
	}, nil
}

// MarkStarted commits the configured release only after Compose and its health
// checks succeed. Deployment backups are intentionally retained for manual data
// recovery, but they are no longer considered pending rollbacks.
func MarkStarted(paths Paths) (State, error) {
	state, err := Load(paths)
	if err != nil {
		return State{}, err
	}
	if state.ConfigSchemaVersion != CurrentSchemaVersion {
		return State{}, errors.New("deployment configuration must be migrated with cocola install before it can be started")
	}
	state.ConfigSchemaVersion = CurrentSchemaVersion
	state.LastSuccessfulVersion = state.Version
	state.LastSuccessfulRevision = state.DeploymentRevision
	state.PendingUpgrade = nil
	if err := writeState(paths, state); err != nil {
		return State{}, err
	}
	return state, nil
}

// RollbackUpgrade restores the exact CLI-owned files captured before a pending
// upgrade. Persistent volumes and the optional database dump are never deleted.
func RollbackUpgrade(paths Paths) (string, error) {
	state, err := Load(paths)
	if err != nil {
		return "", err
	}
	if state.PendingUpgrade == nil {
		return "", errors.New("no Cocola upgrade is pending")
	}
	backupDir := state.PendingUpgrade.BackupDir
	if err := validateBackupDirectory(paths, backupDir); err != nil {
		return "", err
	}
	if err := restoreBackup(paths, backupDir); err != nil {
		return "", err
	}
	return backupDir, nil
}

func BackupPaths(paths Paths, backupDir string) (Paths, error) {
	if err := validateBackupDirectory(paths, backupDir); err != nil {
		return Paths{}, err
	}
	return Paths{
		Home:        paths.Home,
		Environment: filepath.Join(backupDir, filepath.Base(paths.Environment)),
		Compose:     filepath.Join(backupDir, filepath.Base(paths.Compose)),
		State:       filepath.Join(backupDir, filepath.Base(paths.State)),
		SandboxRoot: paths.SandboxRoot,
	}, nil
}

func DatabaseBackupPath(backupDir string) string {
	return filepath.Join(backupDir, "postgres.dump")
}

func ForgejoDatabaseBackupPath(backupDir string) string {
	return filepath.Join(backupDir, "forgejo-postgres.dump")
}

func ForgejoDataBackupPath(backupDir string) string {
	return filepath.Join(backupDir, "forgejo-data.tar.gz")
}

func writeUpgradeFiles(paths Paths, compose, environment []byte, state State) error {
	if err := atomicWrite(paths.Compose, compose, 0o644); err != nil {
		return err
	}
	if err := atomicWrite(paths.Environment, environment, 0o600); err != nil {
		return err
	}
	return writeState(paths, state)
}

func createBackup(paths Paths, fromVersion, toVersion string) (string, error) {
	root := filepath.Join(paths.Home, backupDirectoryName)
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", fmt.Errorf("create deployment backup directory: %w", err)
	}
	name := fmt.Sprintf(
		"upgrade-%s-%s-to-%s",
		time.Now().UTC().Format("20060102T150405.000000000Z"),
		safePathPart(fromVersion), safePathPart(toVersion),
	)
	backupDir := filepath.Join(root, name)
	if err := os.Mkdir(backupDir, 0o700); err != nil {
		return "", fmt.Errorf("create deployment backup: %w", err)
	}
	manifest := backupManifest{Existing: make(map[string]bool)}
	for _, source := range managedFiles(paths) {
		name := filepath.Base(source)
		data, err := os.ReadFile(source)
		if errors.Is(err, os.ErrNotExist) {
			manifest.Existing[name] = false
			continue
		}
		if err != nil {
			return "", fmt.Errorf("back up %s: %w", source, err)
		}
		manifest.Existing[name] = true
		mode := os.FileMode(0o600)
		if name == filepath.Base(paths.Compose) {
			mode = 0o644
		}
		if err := atomicWrite(filepath.Join(backupDir, name), data, mode); err != nil {
			return "", err
		}
	}
	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode deployment backup manifest: %w", err)
	}
	if err := atomicWrite(filepath.Join(backupDir, "manifest.json"), append(manifestData, '\n'), 0o600); err != nil {
		return "", err
	}
	return backupDir, nil
}

func restoreBackup(paths Paths, backupDir string) error {
	if err := validateBackupDirectory(paths, backupDir); err != nil {
		return err
	}
	manifestData, err := os.ReadFile(filepath.Join(backupDir, "manifest.json"))
	if err != nil {
		return fmt.Errorf("read deployment backup manifest: %w", err)
	}
	var manifest backupManifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		return fmt.Errorf("decode deployment backup manifest: %w", err)
	}
	for _, target := range managedFiles(paths) {
		name := filepath.Base(target)
		if !manifest.Existing[name] {
			if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("remove newly generated %s: %w", target, err)
			}
			continue
		}
		data, err := os.ReadFile(filepath.Join(backupDir, name))
		if err != nil {
			return fmt.Errorf("read backed up %s: %w", name, err)
		}
		mode := os.FileMode(0o600)
		if name == filepath.Base(paths.Compose) {
			mode = 0o644
		}
		if err := atomicWrite(target, data, mode); err != nil {
			return err
		}
	}
	return nil
}

func validateBackupDirectory(paths Paths, backupDir string) error {
	backupRoot := filepath.Join(paths.Home, backupDirectoryName)
	relative, err := filepath.Rel(backupRoot, backupDir)
	if err != nil || relative == "." || relative == "" || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return errors.New("pending upgrade backup is outside the Cocola backup directory")
	}
	return nil
}

func managedFiles(paths Paths) []string {
	return []string{paths.Environment, paths.Compose, paths.State}
}

func fileEquals(path string, expected []byte) bool {
	actual, err := os.ReadFile(path)
	return err == nil && bytes.Equal(actual, expected)
}

func safePathPart(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	var output strings.Builder
	for _, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') || char == '.' || char == '-' || char == '_' {
			output.WriteRune(char)
		} else {
			output.WriteByte('-')
		}
	}
	return output.String()
}

type derivedEnvironment struct {
	version            string
	currentRegistry    string
	images             ImageReferences
	publicURL          string
	webPort            int
	gatewayPort        int
	llmPort            int
	internalSCM        InternalSCMEndpoint
	managedOpenSandbox bool
}

func migrateEnvironment(paths Paths, state State, targetVersion string, data []byte) ([]byte, derivedEnvironment, error) {
	return migrateEnvironmentWithOptions(paths, state, targetVersion, nil, data)
}

func migrateEnvironmentWithOptions(
	paths Paths,
	state State,
	targetVersion string,
	requestedRegistry *string,
	data []byte,
) ([]byte, derivedEnvironment, error) {
	document, err := parseEnvironment(data)
	if err != nil {
		return nil, derivedEnvironment{}, err
	}
	defaults := Defaults(targetVersion)
	webPort, err := environmentPort(document, "COCOLA_WEB_HOST_PORT", state.WebPort, defaults.WebPort)
	if err != nil {
		return nil, derivedEnvironment{}, err
	}
	gatewayPort, err := environmentPort(document, "COCOLA_GATEWAY_HOST_PORT", state.GatewayPort, defaults.GatewayPort)
	if err != nil {
		return nil, derivedEnvironment{}, err
	}
	llmPort, err := environmentPort(document, "COCOLA_LLM_HOST_PORT", state.LLMPort, defaults.LLMPort)
	if err != nil {
		return nil, derivedEnvironment{}, err
	}
	internalSCMPort, err := environmentPort(
		document, "COCOLA_FORGEJO_HOST_PORT", state.InternalSCM.HostPort,
		defaults.InternalSCM.HostPort,
	)
	if err != nil {
		return nil, derivedEnvironment{}, err
	}
	currentRegistry := strings.TrimSuffix(environmentValue(document, "COCOLA_IMAGE_REGISTRY", DefaultRegistry), "/")
	if currentRegistry == "" {
		return nil, derivedEnvironment{}, errors.New("COCOLA_IMAGE_REGISTRY cannot be empty")
	}
	registry := currentRegistry
	if requestedRegistry != nil {
		registry = strings.TrimSuffix(strings.TrimSpace(*requestedRegistry), "/")
	} else if currentRegistry == legacyCNMirrorRegistry {
		registry = DefaultRegistry
	}
	images, err := ResolveImageReferences(targetVersion, registry)
	if err != nil {
		return nil, derivedEnvironment{}, err
	}
	managed := state.ManagedOpenSandbox
	if value, ok := document.values["COCOLA_OPENSANDBOX_MANAGED"]; ok {
		managed = value == "1" || strings.EqualFold(value, "true")
	}
	publicOrigins := environmentValue(
		document,
		"COCOLA_PUBLIC_ORIGINS",
		fmt.Sprintf("http://127.0.0.1:%d,http://localhost:%d", webPort, webPort),
	)
	publicURL := state.PublicURL
	if publicURL == "" {
		publicURL = preferredPublicURL(publicOrigins, webPort)
	}
	sandboxCloneURL := environmentValue(
		document, "COCOLA_FORGEJO_CLONE_URL", state.InternalSCM.SandboxCloneURL,
	)
	if strings.TrimSpace(sandboxCloneURL) == "" && !managed {
		return nil, derivedEnvironment{}, errors.New(
			"COCOLA_FORGEJO_CLONE_URL is required when OpenSandbox is externally managed",
		)
	}
	internalSCM := InternalSCMEndpoint{
		APIURL:          environmentValue(document, "COCOLA_FORGEJO_API_URL", state.InternalSCM.APIURL),
		HostPort:        internalSCMPort,
		SandboxCloneURL: sandboxCloneURL,
	}
	if strings.TrimSpace(internalSCM.APIURL) == "" {
		internalSCM.APIURL = defaultInternalSCMAPIURL
	}
	if strings.TrimSpace(internalSCM.SandboxCloneURL) == "" {
		internalSCM.SandboxCloneURL = fmt.Sprintf(
			"http://host.docker.internal:%d", internalSCM.HostPort,
		)
	}
	if err := validateInternalSCMEndpoint(internalSCM); err != nil {
		return nil, derivedEnvironment{}, err
	}

	generated, err := newSecrets()
	if err != nil {
		return nil, derivedEnvironment{}, err
	}
	password, err := randomSecret(18)
	if err != nil {
		return nil, derivedEnvironment{}, err
	}
	managedValue := "0"
	opensandboxURL := environmentValue(document, "COCOLA_OPENSANDBOX_URL", "")
	if managed {
		managedValue = "1"
		if opensandboxURL == "" {
			opensandboxURL = "http://opensandbox-server:8090/v1"
		}
	}
	values := [][2]string{
		{"COCOLA_VERSION", targetVersion},
		{"COCOLA_IMAGE_REGISTRY", images.Registry},
		{"COCOLA_REDIS_IMAGE", images.Redis},
		{"COCOLA_POSTGRES_IMAGE", images.Postgres},
		{"COCOLA_FORGEJO_IMAGE", images.Forgejo},
		{"COCOLA_MINIO_IMAGE", images.MinIO},
		{"COCOLA_MINIO_MC_IMAGE", images.MinIOClient},
		{"COCOLA_OPENVIKING_IMAGE", images.OpenViking},
		{"COCOLA_OPENSANDBOX_IMAGE", images.OpenSandboxServer},
		{"COCOLA_OPENSANDBOX_EXECD_IMAGE", images.OpenSandboxExecd},
		{"COCOLA_OPENSANDBOX_EGRESS_IMAGE", images.OpenSandboxEgress},
		{"COCOLA_HOME", paths.Home},
		{"COCOLA_SANDBOX_ROOT", paths.SandboxRoot},
		{"COCOLA_WEB_HOST", "0.0.0.0"},
		{"COCOLA_WEB_HOST_PORT", strconv.Itoa(webPort)},
		{"COCOLA_GATEWAY_HOST_PORT", strconv.Itoa(gatewayPort)},
		{"COCOLA_PUBLIC_ORIGINS", publicOrigins},
		{"COCOLA_LLM_HOST_PORT", strconv.Itoa(llmPort)},
		{"COCOLA_OPENSANDBOX_MANAGED", managedValue},
		{"COCOLA_OPENSANDBOX_URL", opensandboxURL},
		{"COCOLA_SANDBOX_LLM_BASE_URL", fmt.Sprintf("http://host.docker.internal:%d", llmPort)},
		{"COCOLA_AUTH_SECRET", generated.auth},
		{"AUTH_SECRET", generated.authJS},
		{"COCOLA_ADMIN_KEY", generated.admin},
		{"COCOLA_MODEL_SECRET_KEY", generated.model},
		{"COCOLA_CONFIG_SECRET_KEY", generated.config},
		{"COCOLA_PG_PASSWORD", generated.postgres},
		{"COCOLA_MINIO_ROOT_PASSWORD", generated.minio},
		{"COCOLA_OPENVIKING_URL", "http://openviking:1933"},
		{"COCOLA_OPENVIKING_ROOT_API_KEY", generated.openVikingRoot},
		{"COCOLA_MEMORY_LLM_SERVICE_TOKEN", generated.memoryLLMService},
		{"COCOLA_MEMORY_EMBEDDING_DIMENSION", "1024"},
		{"COCOLA_FORGEJO_DB_PASSWORD", generated.forgejoDB},
		{"COCOLA_FORGEJO_PASSWORD", generated.forgejo},
		{"COCOLA_FORGEJO_HOST_PORT", strconv.Itoa(internalSCM.HostPort)},
		{"COCOLA_FORGEJO_API_URL", internalSCM.APIURL},
		{"COCOLA_FORGEJO_CLONE_URL", internalSCM.SandboxCloneURL},
		{"COCOLA_SCM_SECRET_KEY", generated.scm},
		{"COCOLA_SCM_SECRET_KEY_FILE", ""},
		{"COCOLA_SANDBOX_PROJECT_BROKER_URL", fmt.Sprintf("http://host.docker.internal:%d", gatewayPort)},
		{"COCOLA_SANDBOX_SKILL_BROKER_URL", fmt.Sprintf("http://host.docker.internal:%d", gatewayPort)},
		{"COCOLA_SKILL_PUBLISH_ENABLED", "false"},
		{"COCOLA_PROJECT_MAX_REPOSITORY_MB", "512"},
		{"COCOLA_FEATURE_LOCAL_PROJECTS", "true"},
		{"COCOLA_FEATURE_GITHUB_MANIFEST_CONNECTOR", "true"},
		{"COCOLA_FEATURE_GITHUB_AGENT_WRITE", "true"},
		{"COCOLA_SESSION_VOLUME_SIZE", "2Gi"},
		{"COCOLA_SANDBOX_PROFILE", "coding"},
		{"COCOLA_AGENT_RUNTIME_DEFAULT_ID", defaultAgentRuntimeID},
		{"COCOLA_AGENT_RUNTIME_PICKER_ENABLED", defaultRuntimePickerEnabled},
		{"COCOLA_AGENT_MAX_TURNS", defaultAgentMaxTurns},
		{"COCOLA_AGENT_TOOL_STEP_TIMEOUT_SECS", defaultToolStepTimeoutSecs},
		{"COCOLA_LLM_TIMEOUT_SECS", defaultLLMTimeoutSecs},
		{"COCOLA_SANDBOX_TOKEN_TTL_SECONDS", defaultSandboxTokenTTL},
		{"COCOLA_BOOTSTRAP_ADMIN_USERNAME", "admin"},
		{"COCOLA_BOOTSTRAP_ADMIN_EMAIL", "admin@cocola.local"},
		{"COCOLA_BOOTSTRAP_ADMIN_PASSWORD", password},
		{"COCOLA_BOOTSTRAP_ADMIN_RESET", "false"},
	}
	for _, item := range values {
		if migrationReplacesValue(item[0]) {
			document.set(item[0], item[1])
		} else {
			document.ensure(item[0], item[1])
		}
	}
	document.remove("COCOLA_IMAGE_SOURCE")
	return document.render(), derivedEnvironment{
		version: targetVersion, currentRegistry: currentRegistry,
		images: images, publicURL: publicURL,
		webPort: webPort, gatewayPort: gatewayPort, llmPort: llmPort,
		internalSCM:        internalSCM,
		managedOpenSandbox: managed,
	}, nil
}

func migrationReplacesValue(key string) bool {
	switch key {
	case "COCOLA_VERSION", "COCOLA_HOME", "COCOLA_SANDBOX_ROOT",
		"COCOLA_IMAGE_REGISTRY", "COCOLA_REDIS_IMAGE",
		"COCOLA_POSTGRES_IMAGE", "COCOLA_FORGEJO_IMAGE", "COCOLA_MINIO_IMAGE",
		"COCOLA_MINIO_MC_IMAGE", "COCOLA_OPENVIKING_IMAGE", "COCOLA_OPENSANDBOX_IMAGE",
		"COCOLA_OPENSANDBOX_EXECD_IMAGE", "COCOLA_OPENSANDBOX_EGRESS_IMAGE":
		return true
	default:
		return false
	}
}

func parseEnvironment(data []byte) (*environmentDocument, error) {
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	document := &environmentDocument{
		lines: lines, values: make(map[string]string), indices: make(map[string][]int),
	}
	for index, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, encoded, ok := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !ok || !environmentKeyPattern.MatchString(key) {
			return nil, fmt.Errorf("invalid deployment environment line %d", index+1)
		}
		value, err := decodeEnvironmentValue(strings.TrimSpace(encoded))
		if err != nil {
			return nil, fmt.Errorf("decode %s on line %d: %w", key, index+1, err)
		}
		document.values[key] = value
		document.indices[key] = append(document.indices[key], index)
	}
	return document, nil
}

func decodeEnvironmentValue(value string) (string, error) {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "\"") {
		quoted, trailing, err := splitDoubleQuotedValue(value)
		if err != nil {
			return "", err
		}
		if trailing != "" && !strings.HasPrefix(trailing, "#") {
			return "", errors.New("unexpected content after quoted value")
		}
		decoded, err := strconv.Unquote(quoted)
		if err != nil {
			return "", err
		}
		return strings.ReplaceAll(decoded, "$$", "$"), nil
	}
	if strings.HasPrefix(value, "'") {
		closing := strings.LastIndex(value[1:], "'")
		if closing < 0 {
			return "", errors.New("unterminated single-quoted value")
		}
		closing++
		trailing := strings.TrimSpace(value[closing+1:])
		if trailing != "" && !strings.HasPrefix(trailing, "#") {
			return "", errors.New("unexpected content after quoted value")
		}
		return value[1:closing], nil
	}
	if comment := inlineCommentIndex(value); comment >= 0 {
		value = value[:comment]
	}
	return strings.TrimSpace(value), nil
}

func splitDoubleQuotedValue(value string) (string, string, error) {
	escaped := false
	for index := 1; index < len(value); index++ {
		switch {
		case escaped:
			escaped = false
		case value[index] == '\\':
			escaped = true
		case value[index] == '"':
			return value[:index+1], strings.TrimSpace(value[index+1:]), nil
		}
	}
	return "", "", errors.New("unterminated double-quoted value")
}

func inlineCommentIndex(value string) int {
	for index := 1; index < len(value); index++ {
		if value[index] == '#' && (value[index-1] == ' ' || value[index-1] == '\t') {
			return index
		}
	}
	return -1
}

func (document *environmentDocument) set(key, value string) {
	line := key + "=" + quoteEnv(value)
	if indices := document.indices[key]; len(indices) > 0 {
		for _, index := range indices {
			document.lines[index] = line
		}
	} else {
		document.lines = append(document.lines, line)
		document.indices[key] = []int{len(document.lines) - 1}
	}
	document.values[key] = value
}

func (document *environmentDocument) ensure(key, value string) {
	if _, ok := document.values[key]; !ok {
		document.set(key, value)
	}
}

func (document *environmentDocument) remove(key string) {
	for _, index := range document.indices[key] {
		document.lines[index] = ""
	}
	delete(document.values, key)
	delete(document.indices, key)
}

func (document *environmentDocument) render() []byte {
	return []byte(strings.Join(document.lines, "\n") + "\n")
}

func environmentValue(document *environmentDocument, key, fallback string) string {
	if value, ok := document.values[key]; ok {
		return value
	}
	return fallback
}

func environmentPort(document *environmentDocument, key string, stateValue, fallback int) (int, error) {
	value := stateValue
	if value == 0 {
		value = fallback
	}
	if raw, ok := document.values[key]; ok {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 65535 {
			return 0, fmt.Errorf("%s must be a port between 1 and 65535", key)
		}
		value = parsed
	}
	return value, nil
}

func preferredPublicURL(origins string, webPort int) string {
	local := fmt.Sprintf("http://localhost:%d", webPort)
	for _, origin := range strings.Split(origins, ",") {
		origin = strings.TrimSpace(origin)
		if origin == "" || strings.Contains(origin, "127.0.0.1") || strings.Contains(origin, "localhost") {
			continue
		}
		if normalized, err := NormalizePublicURL(origin); err == nil {
			return normalized
		}
	}
	return local
}
