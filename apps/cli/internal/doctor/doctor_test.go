package doctor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cocola-project/cocola/apps/cli/internal/assets"
	"github.com/cocola-project/cocola/apps/cli/internal/config"
)

func TestRunChecksHealthyServicesVolumesImagesAndPostgres(t *testing.T) {
	paths := setupDoctor(t, false)
	report := Run(context.Background(), paths)
	if !report.OK {
		t.Fatalf("doctor report = %+v", report)
	}
	for _, expected := range []string{
		"service sandbox-manager", "service forgejo", "service minio-init",
		"internal SCM endpoint", "postgres credentials", "image source", "required images",
	} {
		check, ok := findCheck(report, expected)
		if !ok || check.Status != StatusPass {
			t.Fatalf("check %q = %+v, present=%v", expected, check, ok)
		}
	}
}

func TestRunFailsWhenPostgresUsesDifferentCredentials(t *testing.T) {
	paths := setupDoctor(t, true)
	report := Run(context.Background(), paths)
	check, ok := findCheck(report, "postgres credentials")
	if report.OK || !ok || check.Status != StatusFail ||
		!strings.Contains(check.Message, "does not match the current Cocola configuration") {
		t.Fatalf("doctor report = %+v", report)
	}
}

func setupDoctor(t *testing.T, postgresMismatch bool) config.Paths {
	t.Helper()
	home := filepath.Join(t.TempDir(), "cocola")
	paths, err := config.ResolvePaths(home)
	if err != nil {
		t.Fatal(err)
	}
	options := config.Defaults("v0.1.0")
	options.Home = home
	options.AdminPassword = "test-password"
	if _, err := config.WriteInstallation(paths, options, assets.Compose); err != nil {
		t.Fatal(err)
	}
	if _, err := config.MarkStarted(paths); err != nil {
		t.Fatal(err)
	}

	directory := t.TempDir()
	dockerPath := filepath.Join(directory, "docker")
	script := `#!/bin/sh
if [ "$1" = info ] && [ "$2" = --format ]; then printf '%s\n' "$DOCKER_ROOT"; exit 0; fi
if [ "$1" = info ]; then exit 0; fi
if [ "$1 $2 $3" = "compose version --short" ]; then printf '2.23.1\n'; exit 0; fi
if [ "$1 $2" = "volume inspect" ]; then exit 0; fi
if [ "$1 $2" = "image inspect" ]; then exit 0; fi
if [ "$1 $2" = "ps -q" ]; then printf '%s\n' 'forgejo-container'; exit 0; fi
if [ "$1 $2 $3" = "port forgejo-container 3000/tcp" ]; then printf '%s\n' '127.0.0.1:3001'; exit 0; fi
case "$*" in
  *'config --quiet'*) exit 0 ;;
  *'config --images'*) printf '%s\n' 'redis:7.4.10-alpine3.21' 'ghcr.io/sakurs2/cocola-web:v0.1.0'; exit 0 ;;
  *'ps --all --format json'*) printf '%s\n' "$SERVICE_STATUS_JSON"; exit 0 ;;
  *'exec -T postgres sh -ec'*)
    if [ "$POSTGRES_MISMATCH" = 1 ]; then
      printf '%s\n' 'password authentication failed for user "cocola"' >&2
      exit 2
    fi
    exit 0
    ;;
esac
exit 1
`
	if err := os.WriteFile(dockerPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("COCOLA_DOCKER_BIN", dockerPath)
	t.Setenv("COCOLA_DOCKER_SOCKET_SOURCE", "/var/run/docker.sock")
	t.Setenv("DOCKER_ROOT", directory)
	if postgresMismatch {
		t.Setenv("POSTGRES_MISMATCH", "1")
	}
	t.Setenv("SERVICE_STATUS_JSON", `[
  {"Service":"redis","State":"running","Health":"healthy","Status":"Up"},
  {"Service":"postgres","State":"running","Health":"healthy","Status":"Up"},
  {"Service":"forgejo-db-init","State":"exited","ExitCode":0,"Status":"Exited (0)"},
  {"Service":"forgejo","State":"running","Health":"healthy","Status":"Up"},
  {"Service":"forgejo-init","State":"exited","ExitCode":0,"Status":"Exited (0)"},
  {"Service":"minio","State":"running","Health":"healthy","Status":"Up"},
  {"Service":"minio-init","State":"exited","ExitCode":0,"Status":"Exited (0)"},
  {"Service":"opensandbox-server","State":"running","Health":"healthy","Status":"Up"},
  {"Service":"sandbox-manager","State":"running","Health":"healthy","Status":"Up"},
  {"Service":"host-agent","State":"running","Health":"healthy","Status":"Up"},
  {"Service":"llm-gateway","State":"running","Health":"healthy","Status":"Up"},
  {"Service":"admin-api","State":"running","Health":"healthy","Status":"Up"},
  {"Service":"agent-runtime","State":"running","Health":"healthy","Status":"Up"},
  {"Service":"gateway","State":"running","Health":"healthy","Status":"Up"},
  {"Service":"web","State":"running","Health":"healthy","Status":"Up"}
]`)
	return paths
}

func findCheck(report Report, name string) (Check, bool) {
	for _, check := range report.Checks {
		if check.Name == name {
			return check, true
		}
	}
	return Check{}, false
}
