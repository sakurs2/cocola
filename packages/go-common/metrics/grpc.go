package metrics

import (
	"context"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
)

// UnaryServerInterceptor records every unary gRPC call into the RED vectors
// under transport="grpc". The "method" label is info.FullMethod (bounded set),
// "code" is the gRPC status code string (e.g. "OK", "NotFound").
func (r *Registry) UnaryServerInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		r.inflight.WithLabelValues("grpc").Inc()
		defer r.inflight.WithLabelValues("grpc").Dec()

		start := time.Now()
		resp, err := handler(ctx, req)
		r.observeRequest("grpc", info.FullMethod, status.Code(err).String(), time.Since(start).Seconds())
		return resp, err
	}
}

// StreamServerInterceptor records every streaming gRPC call into the RED vectors
// under transport="grpc". Duration spans the whole stream lifetime, which is the
// useful signal for cocola's server-streaming RPCs (Exec, Query).
func (r *Registry) StreamServerInterceptor() grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		r.inflight.WithLabelValues("grpc").Inc()
		defer r.inflight.WithLabelValues("grpc").Dec()

		start := time.Now()
		err := handler(srv, ss)
		r.observeRequest("grpc", info.FullMethod, status.Code(err).String(), time.Since(start).Seconds())
		return err
	}
}
