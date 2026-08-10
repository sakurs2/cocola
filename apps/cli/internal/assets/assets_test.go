package assets

import (
	"bytes"
	"testing"
)

func TestComposeDeploysInternalOpenVikingWithoutHostPort(t *testing.T) {
	for _, required := range [][]byte{
		[]byte("  openviking:"),
		[]byte("${COCOLA_OPENVIKING_IMAGE}"),
		[]byte("COCOLA_OPENVIKING_ROOT_API_KEY"),
		[]byte("COCOLA_MEMORY_LLM_SERVICE_TOKEN"),
		[]byte(`"memory":{"version":"v2"}`),
		[]byte(`"prefix":"openviking-memory-v2/"`),
		[]byte("openvikingdata_memory_v2:/app/.openviking/data"),
		[]byte("COCOLA_MEMORY_BOOTSTRAP_URL"),
		[]byte(`"metrics":{"enabled":true}`),
	} {
		if !bytes.Contains(Compose, required) {
			t.Fatalf("production compose is missing Memory dependency %q", required)
		}
	}
	start := bytes.Index(Compose, []byte("  openviking:\n"))
	end := bytes.Index(Compose[start:], []byte("\n  opensandbox-server:\n"))
	if start < 0 || end < 0 {
		t.Fatal("OpenViking service block is missing")
	}
	block := Compose[start : start+end]
	if bytes.Contains(block, []byte("ports:")) {
		t.Fatal("production OpenViking must not publish a host port")
	}
	if !bytes.Contains(block, []byte("healthcheck:\n      disable: true")) {
		t.Fatal("disabled Memory must disable the image healthcheck so it does not block compose --wait")
	}
	if !bytes.Contains(block, []byte("openviking-entrypoint")) {
		t.Fatal("OpenViking must start only after the configured embedding route is usable")
	}
	if !bytes.Contains(Compose, []byte("mc rm --recursive --force local/cocola/openviking/")) {
		t.Fatal("production upgrade must remove the incompatible legacy Memory object prefix")
	}
	if !bytes.Contains(Compose, []byte("mc rm --recursive --force local/cocola/openviking-v2/")) {
		t.Fatal("production upgrade must remove the incompatible Memory V2 preview prefix")
	}
}

func TestComposeUsesOnlyExplicitCLIResolvedImageVariables(t *testing.T) {
	for _, required := range [][]byte{
		[]byte("${COCOLA_REDIS_IMAGE}"),
		[]byte("${COCOLA_POSTGRES_IMAGE}"),
		[]byte("${COCOLA_FORGEJO_IMAGE}"),
		[]byte("${COCOLA_MINIO_IMAGE}"),
		[]byte("${COCOLA_MINIO_MC_IMAGE}"),
		[]byte("${COCOLA_OPENVIKING_IMAGE}"),
		[]byte("${COCOLA_OPENSANDBOX_IMAGE}"),
		[]byte("${COCOLA_OPENSANDBOX_EXECD_IMAGE}"),
		[]byte("${COCOLA_OPENSANDBOX_EGRESS_IMAGE}"),
	} {
		if !bytes.Contains(Compose, required) {
			t.Fatalf("production compose is missing image variable %q", required)
		}
	}
	for _, forbidden := range [][]byte{
		[]byte("image: redis:"), []byte("image: postgres:"), []byte("image: codeberg.org/"),
		[]byte("image: minio/"), []byte("image: ghcr.io/volcengine/"),
		[]byte("image: opensandbox/"),
	} {
		if bytes.Contains(Compose, forbidden) {
			t.Fatalf("production compose still contains implicit or fixed pull reference %q", forbidden)
		}
	}
}

func TestComposeStopsMinIOSecretFlagParsing(t *testing.T) {
	// Generated URL-safe secrets may begin with a hyphen. The explicit argument
	// terminator keeps mc from interpreting that secret as a command flag.
	if !bytes.Contains(Compose, []byte("mc alias set -- local")) {
		t.Fatal("production compose must terminate mc flags before positional credentials")
	}
}

func TestComposeWaitsForAgentRuntimeReadiness(t *testing.T) {
	for _, required := range [][]byte{
		[]byte("socket.create_connection(('127.0.0.1', 50061), 2)"),
		[]byte("agent-runtime:\n        condition: service_healthy"),
	} {
		if !bytes.Contains(Compose, required) {
			t.Fatalf("production compose must wait for Agent Runtime readiness: missing %q", required)
		}
	}
}

func TestComposeWaitsForSandboxManagerReadiness(t *testing.T) {
	for _, required := range [][]byte{
		[]byte("http://localhost:9092/healthz"),
		[]byte("sandbox-manager:\n        condition: service_healthy"),
	} {
		if !bytes.Contains(Compose, required) {
			t.Fatalf("production compose must wait for Sandbox Manager readiness: missing %q", required)
		}
	}
}

func TestComposeConfiguresGatewayTerminalResolver(t *testing.T) {
	start := bytes.Index(Compose, []byte("  gateway:\n"))
	end := bytes.Index(Compose, []byte("  web:\n"))
	if start < 0 || end <= start {
		t.Fatal("production compose gateway service block is missing")
	}
	gateway := Compose[start:end]
	for _, required := range [][]byte{
		[]byte("sandbox-manager:\n        condition: service_healthy"),
		[]byte("COCOLA_SANDBOX_ADDR: sandbox-manager:50051"),
	} {
		if !bytes.Contains(gateway, required) {
			t.Fatalf("production gateway terminal resolver is not configured: missing %q", required)
		}
	}
}

func TestComposeAuthenticatesPostgreSQLHealthChecks(t *testing.T) {
	for _, required := range [][]byte{
		[]byte("PGPASSWORD=$$POSTGRES_PASSWORD"),
		[]byte("psql -h 127.0.0.1"),
		[]byte("-tAc 'SELECT 1'"),
	} {
		if !bytes.Contains(Compose, required) {
			t.Fatalf("production compose must authenticate PostgreSQL health checks: missing %q", required)
		}
	}
}
