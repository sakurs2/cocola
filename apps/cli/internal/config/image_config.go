package config

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
)

// ValidateConfiguredImages verifies that state.json and the generated concrete
// image references in config.env describe the same deployment. It performs no
// registry requests; Docker pull remains the authority for remote availability.
func ValidateConfiguredImages(paths Paths, state State) error {
	endpoint, err := EffectiveGHCREndpoint(state)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(paths.Environment)
	if err != nil {
		return fmt.Errorf("read deployment environment: %w", err)
	}
	document, err := parseEnvironment(data)
	if err != nil {
		return err
	}
	registry := strings.TrimSuffix(environmentValue(document, "COCOLA_IMAGE_REGISTRY", ""), "/")
	if registry == "" {
		return errors.New("COCOLA_IMAGE_REGISTRY cannot be empty")
	}
	refs, err := ResolveImageReferences(ImageResolutionOptions{
		Version: state.Version, CocolaRegistry: registry, GHCREndpoint: endpoint,
	})
	if err != nil {
		return err
	}
	expected := map[string]string{
		"COCOLA_IMAGE_REGISTRY":           refs.Registry,
		"COCOLA_REDIS_IMAGE":              refs.Redis,
		"COCOLA_POSTGRES_IMAGE":           refs.Postgres,
		"COCOLA_FORGEJO_IMAGE":            refs.Forgejo,
		"COCOLA_MINIO_IMAGE":              refs.MinIO,
		"COCOLA_MINIO_MC_IMAGE":           refs.MinIOClient,
		"COCOLA_OPENVIKING_IMAGE":         refs.OpenViking,
		"COCOLA_OPENSANDBOX_IMAGE":        refs.OpenSandboxServer,
		"COCOLA_OPENSANDBOX_EXECD_IMAGE":  refs.OpenSandboxExecd,
		"COCOLA_OPENSANDBOX_EGRESS_IMAGE": refs.OpenSandboxEgress,
	}
	for key, value := range expected {
		if actual, ok := document.values[key]; !ok || actual != value {
			return fmt.Errorf("%s does not match the configured GHCR endpoint", key)
		}
	}
	if state.SandboxImage != refs.SandboxRuntime ||
		!slices.Equal(state.ManagedRuntimeImages, refs.ManagedRuntimeImages()) {
		return errors.New("managed runtime images do not match the configured GHCR endpoint")
	}
	return nil
}

// EffectiveGHCREndpoint returns the endpoint used by the deployment files that
// are currently configured. State.GHCREndpoint remains the last successfully
// started endpoint until MarkStarted commits a pending candidate.
func EffectiveGHCREndpoint(state State) (string, error) {
	endpoint := state.GHCREndpoint
	if pending := state.PendingUpgrade; pending != nil && strings.TrimSpace(pending.ToGHCREndpoint) != "" {
		endpoint = pending.ToGHCREndpoint
	}
	return NormalizeGHCREndpoint(endpoint)
}
