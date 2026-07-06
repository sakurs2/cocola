package service

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"

	"github.com/cocola-project/cocola/apps/admin-api/internal/store"
)

const (
	MCPTransportStdio = "stdio"
	MCPTransportHTTP  = "http"
	MCPTransportSSE   = "sse"
)

var mcpURLVarPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

type MCPServerInput struct {
	ID             string
	Name           string
	Description    string
	Transport      string
	Command        string
	Args           *[]string
	URL            string
	URLVars        map[string]string
	Env            map[string]string
	Headers        map[string]string
	ClearURLVars   bool
	ClearEnv       bool
	ClearHeaders   bool
	Enabled        *bool
	DefaultEnabled *bool
	Source         string
	Status         string
	Actor          string
}

type MCPServerView struct {
	store.MCPServer
	EffectiveEnabled bool `json:"effective_enabled"`
	PreferenceSet    bool `json:"preference_set"`
}

type MCPRuntimeConfig struct {
	MCPServers map[string]map[string]any `json:"mcp_servers"`
}

func (a *Admin) CreateMCPServer(ctx context.Context, in MCPServerInput) (store.MCPServer, error) {
	id := normalizeID(in.ID)
	transport := normalizeMCPTransport(in.Transport)
	if id == "" || strings.TrimSpace(in.Name) == "" || !validMCPTransport(transport) {
		return store.MCPServer{}, ErrInvalidArg
	}
	now := a.now().UTC()
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	args := []string{}
	if in.Args != nil {
		args = cleanStringList(*in.Args)
	}
	server := store.MCPServer{
		ID:             id,
		Name:           strings.TrimSpace(in.Name),
		Description:    strings.TrimSpace(in.Description),
		Transport:      transport,
		Command:        strings.TrimSpace(in.Command),
		URL:            strings.TrimSpace(in.URL),
		Enabled:        enabled,
		DefaultEnabled: boolValue(in.DefaultEnabled, false),
		Source:         defaultString(strings.TrimSpace(in.Source), "admin"),
		Status:         defaultString(strings.TrimSpace(in.Status), "active"),
		CreatedAt:      now,
		UpdatedAt:      now,
		CreatedBy:      in.Actor,
		UpdatedBy:      in.Actor,
	}
	var err error
	server.ArgsJSON, err = jsonRawOne(args)
	if err != nil {
		return store.MCPServer{}, err
	}
	server.EnvCiphertextJSON, server.EnvHintJSON, err = encryptSecretMap(a.configSecret(), nil, nil, in.Env, false)
	if err != nil {
		return store.MCPServer{}, err
	}
	server.URLVarCiphertextJSON, server.URLVarHintJSON, err = encryptSecretMap(a.configSecret(), nil, nil, in.URLVars, false)
	if err != nil {
		return store.MCPServer{}, err
	}
	server.HeaderCiphertextJSON, server.HeaderHintJSON, err = encryptSecretMap(a.configSecret(), nil, nil, in.Headers, false)
	if err != nil {
		return store.MCPServer{}, err
	}
	if err := validateMCPServerReady(server); err != nil {
		return store.MCPServer{}, err
	}
	if err := a.store.CreateMCPServer(ctx, server); err != nil {
		return store.MCPServer{}, err
	}
	a.audit(ctx, in.Actor, "mcp.create", server.ID, "transport="+server.Transport)
	return server, nil
}

func (a *Admin) ListMCPServers(ctx context.Context, onlyEnabled bool) ([]store.MCPServer, error) {
	return a.store.ListMCPServers(ctx, onlyEnabled)
}

func (a *Admin) GetMCPServer(ctx context.Context, id string) (store.MCPServer, error) {
	return a.store.GetMCPServer(ctx, normalizeID(id))
}

func (a *Admin) UpdateMCPServer(ctx context.Context, id string, in MCPServerInput) (store.MCPServer, error) {
	server, err := a.store.GetMCPServer(ctx, normalizeID(id))
	if err != nil {
		return store.MCPServer{}, err
	}
	if strings.TrimSpace(in.Name) != "" {
		server.Name = strings.TrimSpace(in.Name)
	}
	if in.Description != "" {
		server.Description = strings.TrimSpace(in.Description)
	}
	if in.Transport != "" {
		transport := normalizeMCPTransport(in.Transport)
		if !validMCPTransport(transport) {
			return store.MCPServer{}, ErrInvalidArg
		}
		server.Transport = transport
	}
	if in.Command != "" || server.Transport == MCPTransportStdio {
		server.Command = strings.TrimSpace(in.Command)
	}
	if in.Args != nil {
		server.ArgsJSON, err = jsonRawOne(cleanStringList(*in.Args))
		if err != nil {
			return store.MCPServer{}, err
		}
	}
	if in.URL != "" || server.Transport != MCPTransportStdio {
		server.URL = strings.TrimSpace(in.URL)
	}
	if in.Enabled != nil {
		server.Enabled = *in.Enabled
	}
	if in.DefaultEnabled != nil {
		server.DefaultEnabled = *in.DefaultEnabled
	}
	if in.Source != "" {
		server.Source = strings.TrimSpace(in.Source)
	}
	if in.Status != "" {
		server.Status = strings.TrimSpace(in.Status)
	}
	server.EnvCiphertextJSON, server.EnvHintJSON, err = encryptSecretMap(
		a.configSecret(), server.EnvCiphertextJSON, server.EnvHintJSON, in.Env, in.ClearEnv,
	)
	if err != nil {
		return store.MCPServer{}, err
	}
	server.URLVarCiphertextJSON, server.URLVarHintJSON, err = encryptSecretMap(
		a.configSecret(), server.URLVarCiphertextJSON, server.URLVarHintJSON, in.URLVars, in.ClearURLVars,
	)
	if err != nil {
		return store.MCPServer{}, err
	}
	server.HeaderCiphertextJSON, server.HeaderHintJSON, err = encryptSecretMap(
		a.configSecret(), server.HeaderCiphertextJSON, server.HeaderHintJSON, in.Headers, in.ClearHeaders,
	)
	if err != nil {
		return store.MCPServer{}, err
	}
	server.UpdatedAt = a.now().UTC()
	server.UpdatedBy = in.Actor
	if err := validateMCPServerReady(server); err != nil {
		return store.MCPServer{}, err
	}
	if err := a.store.UpdateMCPServer(ctx, server); err != nil {
		return store.MCPServer{}, err
	}
	a.audit(ctx, in.Actor, "mcp.update", server.ID, "enabled="+boolText(server.Enabled))
	return server, nil
}

func (a *Admin) SetMCPServerEnabled(ctx context.Context, id string, enabled bool, actor string) (store.MCPServer, error) {
	server, err := a.store.GetMCPServer(ctx, normalizeID(id))
	if err != nil {
		return store.MCPServer{}, err
	}
	server.Enabled = enabled
	server.UpdatedAt = a.now().UTC()
	server.UpdatedBy = actor
	if err := a.store.UpdateMCPServer(ctx, server); err != nil {
		return store.MCPServer{}, err
	}
	a.audit(ctx, actor, "mcp.toggle", server.ID, "enabled="+boolText(enabled))
	return server, nil
}

func (a *Admin) DeleteMCPServer(ctx context.Context, id, actor string) error {
	id = normalizeID(id)
	if err := a.store.DeleteMCPServer(ctx, id); err != nil {
		return err
	}
	a.audit(ctx, actor, "mcp.delete", id, "")
	return nil
}

func (a *Admin) ListUserMCPCatalog(ctx context.Context, userID string) ([]MCPServerView, error) {
	servers, err := a.store.ListMCPServers(ctx, true)
	if err != nil {
		return nil, err
	}
	prefs, err := a.store.ListUserMCPPreferences(ctx, userID)
	if err != nil {
		return nil, err
	}
	prefMap := map[string]bool{}
	prefSet := map[string]bool{}
	for _, pref := range prefs {
		prefMap[pref.MCPID] = pref.Enabled
		prefSet[pref.MCPID] = true
	}
	out := make([]MCPServerView, 0, len(servers))
	for _, server := range servers {
		enabled := server.DefaultEnabled
		if prefSet[server.ID] {
			enabled = prefMap[server.ID]
		}
		out = append(out, MCPServerView{
			MCPServer:        server,
			EffectiveEnabled: enabled,
			PreferenceSet:    prefSet[server.ID],
		})
	}
	return out, nil
}

func (a *Admin) SetUserMCPEnabled(ctx context.Context, userID, mcpID string, enabled bool) error {
	server, err := a.store.GetMCPServer(ctx, normalizeID(mcpID))
	if err != nil {
		return err
	}
	if !server.Enabled {
		return ErrNotFound
	}
	return a.store.SetUserMCPPreference(ctx, store.UserMCPPreference{
		UserID:    userID,
		MCPID:     server.ID,
		Enabled:   enabled,
		UpdatedAt: a.now().UTC(),
	})
}

func (a *Admin) ListEffectiveMCPRuntimeConfig(ctx context.Context, userID string) (MCPRuntimeConfig, error) {
	views, err := a.ListUserMCPCatalog(ctx, userID)
	if err != nil {
		return MCPRuntimeConfig{}, err
	}
	out := MCPRuntimeConfig{MCPServers: map[string]map[string]any{}}
	for _, view := range views {
		if !view.EffectiveEnabled || !view.Enabled {
			continue
		}
		cfg, err := a.mcpServerRuntimeConfig(view.MCPServer)
		if err != nil {
			return MCPRuntimeConfig{}, err
		}
		out.MCPServers[view.ID] = cfg
	}
	return out, nil
}

func (a *Admin) mcpServerRuntimeConfig(server store.MCPServer) (map[string]any, error) {
	switch server.Transport {
	case MCPTransportStdio:
		args, err := argsFromJSON(server.ArgsJSON)
		if err != nil {
			return nil, err
		}
		env, err := decryptSecretMap(a.configSecret(), server.EnvCiphertextJSON)
		if err != nil {
			return nil, err
		}
		cfg := map[string]any{"type": "stdio", "command": server.Command}
		if len(args) > 0 {
			cfg["args"] = args
		}
		if len(env) > 0 {
			cfg["env"] = env
		}
		return cfg, nil
	case MCPTransportHTTP, MCPTransportSSE:
		urlVars, err := decryptSecretMap(a.configSecret(), server.URLVarCiphertextJSON)
		if err != nil {
			return nil, err
		}
		renderedURL, err := renderMCPURLTemplate(server.URL, urlVars)
		if err != nil {
			return nil, err
		}
		headers, err := decryptSecretMap(a.configSecret(), server.HeaderCiphertextJSON)
		if err != nil {
			return nil, err
		}
		cfg := map[string]any{"type": server.Transport, "url": renderedURL}
		if len(headers) > 0 {
			cfg["headers"] = headers
		}
		return cfg, nil
	default:
		return nil, ErrInvalidArg
	}
}

func normalizeMCPTransport(v string) string {
	return strings.ToLower(strings.TrimSpace(v))
}

func validMCPTransport(v string) bool {
	return v == MCPTransportStdio || v == MCPTransportHTTP || v == MCPTransportSSE
}

func validateMCPServerReady(server store.MCPServer) error {
	switch server.Transport {
	case MCPTransportStdio:
		if strings.TrimSpace(server.Command) == "" {
			return ErrInvalidArg
		}
	case MCPTransportHTTP, MCPTransportSSE:
		if strings.TrimSpace(server.URL) == "" {
			return ErrInvalidArg
		}
	default:
		return ErrInvalidArg
	}
	return nil
}

func renderMCPURLTemplate(tmpl string, vars map[string]string) (string, error) {
	missing := false
	rendered := mcpURLVarPattern.ReplaceAllStringFunc(tmpl, func(match string) string {
		name := strings.TrimSuffix(strings.TrimPrefix(match, "${"), "}")
		value, ok := vars[name]
		if !ok || value == "" {
			missing = true
			return match
		}
		return value
	})
	if missing {
		return "", ErrInvalidArg
	}
	return rendered, nil
}

func encryptSecretMap(secret string, existingCipher, existingHint []byte, updates map[string]string, clear bool) ([]byte, []byte, error) {
	if clear {
		return []byte("{}"), []byte("{}"), nil
	}
	ciphers := mapFromJSON(existingCipher)
	hints := mapFromJSON(existingHint)
	if updates == nil {
		return jsonRaw(ciphers, hints)
	}
	for key, value := range updates {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		ciphertext, err := encryptModelSecret(secret, value)
		if err != nil {
			return nil, nil, err
		}
		ciphers[key] = ciphertext
		hints[key] = maskAPIKey(value)
	}
	return jsonRaw(ciphers, hints)
}

func decryptSecretMap(secret string, raw []byte) (map[string]string, error) {
	ciphers := mapFromJSON(raw)
	out := make(map[string]string, len(ciphers))
	for key, ciphertext := range ciphers {
		plain, err := decryptModelSecret(secret, ciphertext)
		if err != nil {
			return nil, err
		}
		if plain != "" {
			out[key] = plain
		}
	}
	return out, nil
}

func mapFromJSON(raw []byte) map[string]string {
	out := map[string]string{}
	if len(raw) == 0 {
		return out
	}
	_ = json.Unmarshal(raw, &out)
	return out
}

func argsFromJSON(raw []byte) ([]string, error) {
	var args []string
	if len(raw) == 0 {
		return nil, nil
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, err
	}
	return cleanStringList(args), nil
}

func cleanStringList(in []string) []string {
	out := make([]string, 0, len(in))
	for _, item := range in {
		item = strings.TrimSpace(item)
		if item != "" {
			out = append(out, item)
		}
	}
	return out
}

func jsonRaw(values ...any) ([]byte, []byte, error) {
	if len(values) != 2 {
		return nil, nil, ErrInvalidArg
	}
	left, err := json.Marshal(values[0])
	if err != nil {
		return nil, nil, err
	}
	right, err := json.Marshal(values[1])
	if err != nil {
		return nil, nil, err
	}
	return left, right, nil
}

func jsonRawOne(value any) ([]byte, error) {
	return json.Marshal(value)
}

func boolValue(v *bool, fallback bool) bool {
	if v == nil {
		return fallback
	}
	return *v
}

func defaultString(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

func boolText(v bool) string {
	if v {
		return "true"
	}
	return "false"
}
