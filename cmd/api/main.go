// Command api serves `estimate(segment)` over gRPC, reading from ClickHouse.
//
// It is stateless and deployed separately from the ingestion worker
// (cmd/ingest), so query capacity scales independently of write throughput.
//
// Configuration (environment variables, optionally loaded from ./.env; see
// .env.example):
//
//	CLICKHOUSE_DSN   clickhouse://default:clickhouse@localhost:9000/default
//	GRPC_ADDR        :9090
//	PPROF_ENABLED    false   (true starts the pprof HTTP server)
//	PPROF_ADDR       localhost:6061
package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/joho/godotenv"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	estimationv1 "github.com/KhashayarKhm/go-challenge/api/gen/estimation/v1"
	"github.com/KhashayarKhm/go-challenge/internal/estimate"
	"github.com/KhashayarKhm/go-challenge/internal/pprofserver"
	"github.com/KhashayarKhm/go-challenge/internal/store/clickhouse"
	grpctransport "github.com/KhashayarKhm/go-challenge/internal/transport/grpc"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(log); err != nil {
		log.Error("api stopped", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	// Optional local config: real environment variables take precedence over
	// .env, and a missing file is fine (e.g. in containers).
	if err := godotenv.Load(); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("load .env: %w", err)
	}

	addr := env("GRPC_ADDR", ":9090")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Default port differs from cmd/ingest so both can profile on one host.
	stopPprof, err := pprofserver.StartFromEnv("localhost:6061", log)
	if err != nil {
		return err
	}
	defer stopPprof()

	db, err := clickhouse.Open(ctx, env("CLICKHOUSE_DSN", "clickhouse://default:clickhouse@localhost:9000/default"))
	if err != nil {
		return err
	}
	defer db.Close()

	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", addr, err)
	}
	srv := grpc.NewServer()
	estimationv1.RegisterEstimationServiceServer(srv, grpctransport.NewServer(estimate.NewService(db), log))
	reflection.Register(srv) // lets grpcurl discover the API

	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(lis) }()
	log.Info("api started", "grpc_addr", addr)

	select {
	case <-ctx.Done():
		srv.GracefulStop() // finish in-flight requests
		log.Info("api stopped gracefully")
		return nil
	case err := <-serveErr:
		if errors.Is(err, grpc.ErrServerStopped) {
			return nil
		}
		return err
	}
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
