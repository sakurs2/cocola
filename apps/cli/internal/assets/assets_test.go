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
