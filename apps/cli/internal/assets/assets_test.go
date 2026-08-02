package assets

import (
	"bytes"
	"testing"
)

func TestComposeDoesNotDeployOpenViking(t *testing.T) {
	for _, forbidden := range [][]byte{
		[]byte("openviking:"),
		[]byte("COCOLA_OPENVIKING_"),
		[]byte("COCOLA_MEMORY_LLM_SERVICE_TOKEN"),
	} {
		if bytes.Contains(Compose, forbidden) {
			t.Fatalf("production compose still contains disabled Memory dependency %q", forbidden)
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
