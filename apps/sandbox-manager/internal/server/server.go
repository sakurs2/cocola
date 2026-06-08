// Package server adapts the SandboxService gRPC contract onto the
// provider.SandboxProvider interface. It contains zero backend-specific logic:
// every concrete provider (Docker today, K8s+gVisor later) plugs in behind the
// same interface, so this layer never changes when the backend changes.
package server

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/cocola-project/cocola/apps/sandbox-manager/internal/provider"
	sandboxv1 "github.com/cocola-project/cocola/packages/proto/gen/go/cocola/sandbox/v1"
)

// Server implements sandboxv1.SandboxServiceServer.
type Server struct {
	sandboxv1.UnimplementedSandboxServiceServer
	p provider.SandboxProvider
}

// New wires a provider into the gRPC server.
func New(p provider.SandboxProvider) *Server { return &Server{p: p} }

// Create provisions a sandbox.
func (s *Server) Create(ctx context.Context, req *sandboxv1.CreateRequest) (*sandboxv1.CreateResponse, error) {
	spec := req.GetSpec()
	if spec == nil {
		return nil, status.Error(codes.InvalidArgument, "spec is required")
	}
	res := provider.Resources{}
	if r := spec.GetResources(); r != nil {
		res = provider.Resources{
			CPUCores:  r.GetCpuCores(),
			MemoryMiB: r.GetMemoryMib(),
			DiskMiB:   r.GetDiskMib(),
		}
	}
	sb, err := s.p.Create(ctx, provider.SandboxSpec{
		UserID:     spec.GetUserId(),
		SessionID:  spec.GetSessionId(),
		Image:      spec.GetImage(),
		Env:        spec.GetEnv(),
		Resources:  res,
		Networking: provider.Networking{EgressAllowlist: spec.GetEgressAllowlist()},
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "create: %v", err)
	}
	return &sandboxv1.CreateResponse{Sandbox: toProtoSandbox(sb)}, nil
}

// Exec streams command output back to the caller.
func (s *Server) Exec(req *sandboxv1.ExecRequest, stream sandboxv1.SandboxService_ExecServer) error {
	ctx := stream.Context()
	events, err := s.p.Exec(ctx, req.GetSandboxId(), provider.ExecRequest{
		Cmd:     req.GetCmd(),
		Cwd:     req.GetCwd(),
		Env:     req.GetEnv(),
		Stdin:   req.GetStdin(),
		Timeout: int(req.GetTimeoutSecs()),
	})
	if err != nil {
		return status.Errorf(codes.Internal, "exec: %v", err)
	}
	for ev := range events {
		out := &sandboxv1.ExecEvent{}
		switch ev.Kind {
		case provider.ExecEventStdout:
			out.Kind = sandboxv1.ExecEventKind_EXEC_EVENT_KIND_STDOUT
			out.Stdout = ev.Stdout
		case provider.ExecEventStderr:
			out.Kind = sandboxv1.ExecEventKind_EXEC_EVENT_KIND_STDERR
			out.Stderr = ev.Stderr
		case provider.ExecEventExit:
			out.Kind = sandboxv1.ExecEventKind_EXEC_EVENT_KIND_EXIT
			out.ExitCode = ev.Exit
		case provider.ExecEventError:
			out.Kind = sandboxv1.ExecEventKind_EXEC_EVENT_KIND_ERROR
			if ev.Err != nil {
				out.Error = ev.Err.Error()
			}
		}
		if err := stream.Send(out); err != nil {
			return err
		}
	}
	return nil
}

// WriteFile writes a file into the sandbox.
func (s *Server) WriteFile(ctx context.Context, req *sandboxv1.WriteFileRequest) (*sandboxv1.WriteFileResponse, error) {
	if err := s.p.WriteFile(ctx, req.GetSandboxId(), req.GetPath(), req.GetData()); err != nil {
		return nil, status.Errorf(codes.Internal, "write_file: %v", err)
	}
	return &sandboxv1.WriteFileResponse{}, nil
}

// ReadFile reads a file from the sandbox.
func (s *Server) ReadFile(ctx context.Context, req *sandboxv1.ReadFileRequest) (*sandboxv1.ReadFileResponse, error) {
	data, err := s.p.ReadFile(ctx, req.GetSandboxId(), req.GetPath())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "read_file: %v", err)
	}
	return &sandboxv1.ReadFileResponse{Data: data}, nil
}

// Pause freezes the sandbox.
func (s *Server) Pause(ctx context.Context, req *sandboxv1.PauseRequest) (*sandboxv1.PauseResponse, error) {
	if err := s.p.Pause(ctx, req.GetSandboxId()); err != nil {
		return nil, status.Errorf(codes.Internal, "pause: %v", err)
	}
	return &sandboxv1.PauseResponse{}, nil
}

// Resume thaws the sandbox.
func (s *Server) Resume(ctx context.Context, req *sandboxv1.ResumeRequest) (*sandboxv1.ResumeResponse, error) {
	if err := s.p.Resume(ctx, req.GetSandboxId()); err != nil {
		return nil, status.Errorf(codes.Internal, "resume: %v", err)
	}
	return &sandboxv1.ResumeResponse{}, nil
}

// Destroy tears down the sandbox.
func (s *Server) Destroy(ctx context.Context, req *sandboxv1.DestroyRequest) (*sandboxv1.DestroyResponse, error) {
	if err := s.p.Destroy(ctx, req.GetSandboxId()); err != nil {
		return nil, status.Errorf(codes.Internal, "destroy: %v", err)
	}
	return &sandboxv1.DestroyResponse{}, nil
}

// Health reports sandbox liveness.
func (s *Server) Health(ctx context.Context, req *sandboxv1.HealthRequest) (*sandboxv1.HealthResponse, error) {
	hs, err := s.p.Health(ctx, req.GetSandboxId())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "health: %v", err)
	}
	return &sandboxv1.HealthResponse{Healthy: hs.Healthy, Detail: hs.Detail}, nil
}

func toProtoSandbox(sb *provider.Sandbox) *sandboxv1.Sandbox {
	return &sandboxv1.Sandbox{
		Id:        sb.ID,
		UserId:    sb.UserID,
		SessionId: sb.SessionID,
		Endpoint:  sb.Endpoint,
	}
}
