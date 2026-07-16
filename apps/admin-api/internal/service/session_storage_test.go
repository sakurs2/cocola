package service

import (
	"errors"
	"net/http"
	"testing"
)

func TestRelativeStoragePath(t *testing.T) {
	tests := []struct {
		name    string
		target  string
		want    string
		wantErr bool
	}{
		{name: "volume directory", target: "/var/lib/cocola/storage/pvc-a", want: "pvc-a"},
		{name: "nested directory", target: "/var/lib/cocola/storage/team/pvc-a", want: "team/pvc-a"},
		{name: "root", target: "/var/lib/cocola/storage", wantErr: true},
		{name: "sibling prefix", target: "/var/lib/cocola/storage-old/pvc-a", wantErr: true},
		{name: "parent", target: "/var/lib/cocola", wantErr: true},
		{name: "relative", target: "pvc-a", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := relativeStoragePath("/var/lib/cocola/storage", tt.target)
			if (err != nil) != tt.wantErr {
				t.Fatalf("relativeStoragePath() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("relativeStoragePath() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCleanWorkspaceRequestPath(t *testing.T) {
	tests := []struct {
		name      string
		raw       string
		allowRoot bool
		want      string
		wantErr   bool
	}{
		{name: "root", allowRoot: true},
		{name: "nested", raw: "src/../README.md", want: "README.md"},
		{name: "absolute", raw: "/etc/passwd", wantErr: true},
		{name: "parent", raw: "../runtime/claude", wantErr: true},
		{name: "empty file", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := cleanWorkspaceRequestPath(tt.raw, tt.allowRoot)
			if (err != nil) != tt.wantErr || got != tt.want {
				t.Fatalf("cleanWorkspaceRequestPath(%q) = %q, %v", tt.raw, got, err)
			}
		})
	}
}

func TestMapWorkspaceProbeError(t *testing.T) {
	tests := []struct {
		status int
		want   error
	}{
		{status: http.StatusBadRequest, want: ErrInvalidArg},
		{status: http.StatusNotFound, want: ErrWorkspaceNotFound},
		{status: http.StatusRequestEntityTooLarge, want: ErrWorkspaceFileTooLarge},
		{status: http.StatusUnsupportedMediaType, want: ErrWorkspacePreviewUnsupported},
		{status: http.StatusUnprocessableEntity, want: ErrWorkspaceDirectoryTooLarge},
		{status: http.StatusTooManyRequests, want: ErrTooManyRequests},
		{status: http.StatusGatewayTimeout, want: ErrWorkspaceNodeUnavailable},
	}
	for _, tt := range tests {
		got := mapWorkspaceProbeError(&kubeStatusError{StatusCode: tt.status})
		if !errors.Is(got, tt.want) {
			t.Fatalf("status %d mapped to %v, want %v", tt.status, got, tt.want)
		}
	}
	if got := mapWorkspaceProbeError(errors.New("kubernetes connection failed")); !errors.Is(got, ErrWorkspaceNodeUnavailable) {
		t.Fatalf("infrastructure error mapped to %v", got)
	}
}
