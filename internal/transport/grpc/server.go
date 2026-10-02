// Package grpc exposes the estimate service over gRPC.
//
// gRPC is used for the query side because it gives a typed, versioned contract
// (api/proto) with generated clients for any language, and efficient HTTP/2
// connections for service-to-service calls.
package grpc

import (
	"context"
	"errors"
	"log/slog"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	estimationv1 "github.com/KhashayarKhm/go-challenge/api/gen/estimation/v1"
	"github.com/KhashayarKhm/go-challenge/internal/estimate"
)

// Estimator is the use case this transport depends on (estimate.Service).
type Estimator interface {
	Estimate(ctx context.Context, segment string) (uint64, error)
}

// Server implements estimationv1.EstimationServiceServer.
type Server struct {
	estimationv1.UnimplementedEstimationServiceServer
	estimator Estimator
	log       *slog.Logger
}

// NewServer creates a Server. A nil logger discards logs.
func NewServer(e Estimator, log *slog.Logger) *Server {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Server{estimator: e, log: log}
}

// Estimate maps domain errors to gRPC status codes; internal error details are
// logged, not leaked to callers.
func (s *Server) Estimate(ctx context.Context, req *estimationv1.EstimateRequest) (*estimationv1.EstimateResponse, error) {
	users, err := s.estimator.Estimate(ctx, req.GetSegment())
	switch {
	case errors.Is(err, estimate.ErrInvalidSegment):
		return nil, status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, context.Canceled):
		return nil, status.Error(codes.Canceled, err.Error())
	case errors.Is(err, context.DeadlineExceeded):
		return nil, status.Error(codes.DeadlineExceeded, err.Error())
	case err != nil:
		s.log.Error("estimate failed", "segment", req.GetSegment(), "err", err)
		return nil, status.Error(codes.Internal, "failed to estimate segment size")
	}
	return &estimationv1.EstimateResponse{Segment: req.GetSegment(), Users: users}, nil
}
