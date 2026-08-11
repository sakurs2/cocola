# fix: route Web usage and quota requests to the internal LLM Gateway

- Change time: 2026-08-11 19:24 (+08:00)

## Reason

The Profile **Usage & quota** panel returned `502` with `llm-gateway unreachable: fetch failed` in managed Docker deployments. The Web container did not receive `COCOLA_LLM_GATEWAY_URL`, so its server-side quota and usage routes fell back to `127.0.0.1:8081`. Inside the Web container, that address refers to the Web process rather than the LLM Gateway service.

## Changes

- `apps/cli/internal/assets/compose.yaml`: inject the Compose-internal `http://llm-gateway:8080` endpoint into the Web service.
- `apps/cli/internal/assets/assets_test.go`: verify the setting specifically within the Web service block so another service's configuration cannot satisfy the regression test.
- The change affects only internal request routing; it does not change quota data, authentication, ports, or persistent storage.
