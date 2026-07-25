package httpapi

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/cocola-project/cocola/apps/gateway/internal/agent"
)

const DefaultAgentRuntimeID = "claude-code"

type AgentRuntimeProductConfig struct {
	DefaultID     string `json:"default_id"`
	PickerEnabled bool   `json:"picker_enabled"`
}

type WikiProductConfig struct {
	Enabled           bool     `json:"enabled"`
	MaxFileBytes      int64    `json:"max_file_bytes"`
	AllowedExtensions []string `json:"allowed_extensions"`
}

type ProductConfig struct {
	AgentRuntime AgentRuntimeProductConfig `json:"agent_runtime"`
	Wiki         WikiProductConfig         `json:"wiki"`
}

func DefaultProductConfig() ProductConfig {
	return ProductConfig{
		AgentRuntime: AgentRuntimeProductConfig{DefaultID: DefaultAgentRuntimeID},
		Wiki: WikiProductConfig{
			Enabled:      true,
			MaxFileBytes: DefaultWikiMaxFileBytes,
			AllowedExtensions: []string{
				".csv", ".docx", ".json", ".md", ".pdf", ".pptx",
				".txt", ".xlsx", ".yaml", ".yml",
			},
		},
	}
}

func (c ProductConfig) Validate(runtimes []agent.Runtime) error {
	if c.Wiki.Enabled && c.Wiki.MaxFileBytes <= 0 {
		return fmt.Errorf("Wiki max file bytes must be positive")
	}
	defaultID := strings.TrimSpace(c.AgentRuntime.DefaultID)
	if defaultID == "" {
		return fmt.Errorf("default agent runtime id is required")
	}
	for _, runtime := range runtimes {
		if runtime.ID == defaultID {
			return nil
		}
	}
	return fmt.Errorf("default agent runtime %q is not available", defaultID)
}

func (a *API) WithProductConfig(config ProductConfig) *API {
	config.AgentRuntime.DefaultID = strings.TrimSpace(config.AgentRuntime.DefaultID)
	a.productConfig = config
	return a
}

func (a *API) getProductConfig(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, a.productConfig)
}
