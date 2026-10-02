package grpc

import (
	"context"
	"errors"
	"net"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	estimationv1 "github.com/KhashayarKhm/go-challenge/api/gen/estimation/v1"
	"github.com/KhashayarKhm/go-challenge/internal/estimate"
)

type fakeEstimator struct {
	users uint64
	err   error
}

func (f fakeEstimator) Estimate(context.Context, string) (uint64, error) { return f.users, f.err }

// newClient runs the server on an in-memory listener and returns a real client,
// so the test covers registration, marshaling and status codes end to end.
func newClient(t *testing.T, e Estimator) estimationv1.EstimationServiceClient {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer()
	estimationv1.RegisterEstimationServiceServer(srv, NewServer(e, nil))
	go srv.Serve(lis)
	t.Cleanup(srv.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return estimationv1.NewEstimationServiceClient(conn)
}

func TestEstimateOK(t *testing.T) {
	c := newClient(t, fakeEstimator{users: 1234})
	resp, err := c.Estimate(context.Background(), &estimationv1.EstimateRequest{Segment: "sports"})
	if err != nil {
		t.Fatalf("Estimate: %v", err)
	}
	if resp.GetSegment() != "sports" || resp.GetUsers() != 1234 {
		t.Fatalf("response = %+v", resp)
	}
}

func TestEstimateErrorCodes(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want codes.Code
	}{
		{"invalid segment", estimate.ErrInvalidSegment, codes.InvalidArgument},
		{"store failure", errors.New("clickhouse down"), codes.Internal},
		{"deadline", context.DeadlineExceeded, codes.DeadlineExceeded},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newClient(t, fakeEstimator{err: tt.err})
			_, err := c.Estimate(context.Background(), &estimationv1.EstimateRequest{Segment: "sports"})
			if got := status.Code(err); got != tt.want {
				t.Fatalf("code = %v, want %v (err %v)", got, tt.want, err)
			}
		})
	}
}

func TestInternalErrorDetailsAreNotLeaked(t *testing.T) {
	c := newClient(t, fakeEstimator{err: errors.New("dial tcp 10.0.0.5:9000: secret detail")})
	_, err := c.Estimate(context.Background(), &estimationv1.EstimateRequest{Segment: "sports"})
	if msg := status.Convert(err).Message(); msg != "failed to estimate segment size" {
		t.Fatalf("message = %q", msg)
	}
}
