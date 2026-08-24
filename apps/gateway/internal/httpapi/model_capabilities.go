package httpapi

import (
	"context"
	"fmt"
	"strings"

	"github.com/cocola-project/cocola/apps/gateway/internal/chatrun"
)

type modelRouteCapabilityReader interface {
	AnthropicServerWebToolsEnabled(context.Context, string) (bool, error)
}

func readAnthropicServerWebToolsEnabled(
	ctx context.Context,
	store chatrun.Store,
	modelRouteID string,
) (bool, error) {
	modelRouteID = strings.TrimSpace(modelRouteID)
	if modelRouteID == "" {
		return false, nil
	}
	reader, ok := store.(modelRouteCapabilityReader)
	if !ok {
		return false, nil
	}
	enabled, err := reader.AnthropicServerWebToolsEnabled(ctx, modelRouteID)
	if err != nil {
		return false, err
	}
	return enabled, nil
}

func (a *API) anthropicServerWebToolsEnabled(ctx context.Context, modelRouteID string) bool {
	enabled, err := readAnthropicServerWebToolsEnabled(ctx, a.runs.store, modelRouteID)
	if err != nil {
		a.log.Warn(fmt.Sprintf(
			"model route capability read failed: model_route_id=%q capability=%q error=%v",
			modelRouteID,
			"anthropic_server_web_tools_enabled",
			err,
		))
		return false
	}
	return enabled
}
