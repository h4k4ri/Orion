package middleware

import (
	"context"
	"log/slog"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"

	"github.com/horizon/orion/sdk/go/metrics"
)

type Logging struct {
	logger *slog.Logger
}

func NewLogging(logger *slog.Logger) *Logging {
	return &Logging{logger: logger}
}

func (l *Logging) UnaryServerInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		start := time.Now()
		md, _ := metadata.FromIncomingContext(ctx)
		peerInfo, _ := peer.FromContext(ctx)
		peerAddress := ""
		if peerInfo != nil && peerInfo.Addr != nil {
			peerAddress = peerInfo.Addr.String()
		}

		l.logger.Info("gRPC request",
			"method", info.FullMethod,
			"peer", peerAddress,
			"client_id", md.Get("x-client-id"),
		)

		resp, err := handler(ctx, req)

		duration := time.Since(start)
		l.logger.Info("gRPC response",
			"method", info.FullMethod,
			"duration_ms", duration.Milliseconds(),
			"error", err,
		)

		return resp, err
	}
}

func (l *Logging) StreamServerInterceptor() grpc.StreamServerInterceptor {
	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		start := time.Now()
		md, _ := metadata.FromIncomingContext(ss.Context())

		l.logger.Info("gRPC stream start",
			"method", info.FullMethod,
			"client_id", md.Get("x-client-id"),
		)

		err := handler(srv, ss)

		duration := time.Since(start)
		l.logger.Info("gRPC stream end",
			"method", info.FullMethod,
			"duration_ms", duration.Milliseconds(),
			"error", err,
		)

		return err
	}
}

type Metrics struct {
	recorder *metrics.Recorder
}

func NewMetrics(recorder *metrics.Recorder) *Metrics {
	return &Metrics{recorder: recorder}
}

func (m *Metrics) UnaryServerInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		start := time.Now()

		resp, err := handler(ctx, req)

		duration := time.Since(start)
		m.recorder.Record(info.FullMethod, duration, err == nil)

		return resp, err
	}
}

func (m *Metrics) StreamServerInterceptor() grpc.StreamServerInterceptor {
	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		start := time.Now()
		err := handler(srv, ss)

		duration := time.Since(start)
		m.recorder.Record(info.FullMethod, duration, err == nil)

		return err
	}
}
