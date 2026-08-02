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
