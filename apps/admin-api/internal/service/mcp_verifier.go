package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	sandboxv1 "github.com/cocola-project/cocola/packages/proto/gen/go/cocola/sandbox/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	mcpVerificationTimeout = 45 * time.Second
	mcpVerifierOutputLimit = 64 << 10
)

type mcpSandboxRunner interface {
	Acquire(ctx context.Context, sessionID, image string) (string, error)
	Exec(ctx context.Context, sandboxID string, stdin []byte, timeout time.Duration) ([]byte, int32, error)
	Release(ctx context.Context, sessionID string) error
}

// SandboxMCPVerifier checks one MCP definition inside the same sandbox image
// and network boundary used by real agent sessions.
type SandboxMCPVerifier struct {
	runner mcpSandboxRunner
	image  string
}

func NewSandboxMCPVerifier(address, image string) (*SandboxMCPVerifier, error) {
	address = strings.TrimSpace(address)
	if address == "" {
		return nil, ErrNotConfigured
	}
	conn, err := grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}
	return &SandboxMCPVerifier{
		runner: &grpcMCPSandboxRunner{client: sandboxv1.NewSandboxServiceClient(conn)},
		image:  strings.TrimSpace(image),
	}, nil
}

func (v *SandboxMCPVerifier) Verify(ctx context.Context, serverID string, config map[string]any) (MCPVerificationResult, error) {
	if v == nil || v.runner == nil {
		return MCPVerificationResult{}, ErrNotConfigured
	}
	payload, err := json.Marshal(map[string]any{"id": serverID, "config": config})
	if err != nil {
		return MCPVerificationResult{}, err
	}

	verifyCtx, cancel := context.WithTimeout(ctx, mcpVerificationTimeout)
	defer cancel()
	sessionID := "mcp-verify-" + newID()
	sandboxID, err := v.runner.Acquire(verifyCtx, sessionID, v.image)
	if err != nil {
		return MCPVerificationResult{}, fmt.Errorf("temporary sandbox unavailable")
	}
	defer func() {
		releaseCtx, releaseCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer releaseCancel()
		_ = v.runner.Release(releaseCtx, sessionID)
	}()

	stdout, exitCode, err := v.runner.Exec(verifyCtx, sandboxID, payload, mcpVerificationTimeout)
	if err != nil {
		if errors.Is(verifyCtx.Err(), context.DeadlineExceeded) {
			return MCPVerificationResult{}, fmt.Errorf("verification timed out after 45 seconds")
		}
		return MCPVerificationResult{}, fmt.Errorf("sandbox verification failed")
	}
	result, message, err := decodeMCPCheckOutput(stdout)
	if err != nil {
		return MCPVerificationResult{}, fmt.Errorf("invalid verifier response")
	}
	if exitCode != 0 || result.Status != "connected" {
		if strings.TrimSpace(message) == "" {
			message = "server did not complete the MCP handshake"
		}
		return MCPVerificationResult{}, errors.New(message)
	}
	return result, nil
}

type mcpCheckOutput struct {
	Status        string `json:"status"`
	ServerName    string `json:"server_name"`
	ServerVersion string `json:"server_version"`
	ToolCount     int    `json:"tool_count"`
	Error         string `json:"error"`
}

func decodeMCPCheckOutput(stdout []byte) (MCPVerificationResult, string, error) {
	lines := bytes.Split(bytes.TrimSpace(stdout), []byte("\n"))
	for i := len(lines) - 1; i >= 0; i-- {
		var out mcpCheckOutput
		if err := json.Unmarshal(lines[i], &out); err != nil || strings.TrimSpace(out.Status) == "" {
			continue
		}
		return MCPVerificationResult{
			Status:        out.Status,
			ServerName:    out.ServerName,
			ServerVersion: out.ServerVersion,
			ToolCount:     out.ToolCount,
		}, strings.TrimSpace(out.Error), nil
	}
	return MCPVerificationResult{}, "", io.ErrUnexpectedEOF
}

type grpcMCPSandboxRunner struct {
	client sandboxv1.SandboxServiceClient
}

func (r *grpcMCPSandboxRunner) Acquire(ctx context.Context, sessionID, image string) (string, error) {
	resp, err := r.client.Acquire(ctx, &sandboxv1.AcquireRequest{
		SessionId: sessionID,
		UserId:    "system:mcp-verifier",
		Image:     image,
	})
	if err != nil || resp.GetSandbox().GetId() == "" {
		return "", err
	}
	return resp.GetSandbox().GetId(), nil
}

func (r *grpcMCPSandboxRunner) Exec(ctx context.Context, sandboxID string, stdin []byte, timeout time.Duration) ([]byte, int32, error) {
	stream, err := r.client.Exec(ctx, &sandboxv1.ExecRequest{
		SandboxId:   sandboxID,
		Cmd:         []string{"/opt/cocola/shim/entrypoint.sh", "--mcp-check"},
		Stdin:       stdin,
		TimeoutSecs: int32(timeout / time.Second),
	})
	if err != nil {
		return nil, 0, err
	}
	var stdout bytes.Buffer
	var exitCode int32
	for {
		event, err := stream.Recv()
		if err == io.EOF {
			return stdout.Bytes(), exitCode, nil
		}
		if err != nil {
			return nil, exitCode, err
		}
		switch event.GetKind() {
		case sandboxv1.ExecEventKind_EXEC_EVENT_KIND_STDOUT:
			if stdout.Len() < mcpVerifierOutputLimit {
				remaining := mcpVerifierOutputLimit - stdout.Len()
				chunk := event.GetStdout()
				if len(chunk) > remaining {
					chunk = chunk[:remaining]
				}
				_, _ = stdout.Write(chunk)
			}
		case sandboxv1.ExecEventKind_EXEC_EVENT_KIND_EXIT:
			exitCode = event.GetExitCode()
		case sandboxv1.ExecEventKind_EXEC_EVENT_KIND_ERROR:
			return nil, exitCode, errors.New("sandbox execution failed")
		}
	}
}

func (r *grpcMCPSandboxRunner) Release(ctx context.Context, sessionID string) error {
	_, err := r.client.Release(ctx, &sandboxv1.ReleaseRequest{SessionId: sessionID})
	return err
}
