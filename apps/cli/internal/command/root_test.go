package command

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVersionJSON(t *testing.T) {
	var output, errors bytes.Buffer
	err := Execute(context.Background(), []string{"version", "--json"}, IO{
		In: &bytes.Buffer{}, Out: &output, Err: &errors,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"version"`) || strings.Contains(output.String(), "\x1b[") {
		t.Fatalf("output = %q", output.String())
	}
}

func TestNonInteractiveInstallWritesEmbeddedRelease(t *testing.T) {
	home := filepath.Join(t.TempDir(), "cocola")
	var output, errors bytes.Buffer
	err := Execute(context.Background(), []string{
		"install", "--home", home, "--yes", "--admin-password", "test-password",
	}, IO{In: &bytes.Buffer{}, Out: &output, Err: &errors})
	if err != nil {
		t.Fatalf("install: %v, stderr=%s", err, errors.String())
	}
	compose, err := os.ReadFile(filepath.Join(home, "compose.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(compose), "build:") {
		t.Fatal("embedded release compose must not build from source")
	}
	if !strings.Contains(string(compose), "cocola-gateway:${COCOLA_VERSION}") {
		t.Fatal("embedded release compose does not use versioned images")
	}
	if !strings.Contains(output.String(), "cocola up") {
		t.Fatalf("install output must explain how to start Cocola: %q", output.String())
	}
}

func TestInteractiveCommandFailsClearlyWithoutTTY(t *testing.T) {
	var output, errors bytes.Buffer
	err := Execute(context.Background(), []string{"install"}, IO{
		In: &bytes.Buffer{}, Out: &output, Err: &errors,
	})
	if err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("error = %v", err)
	}
}
