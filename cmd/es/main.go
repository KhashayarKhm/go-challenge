// Command es runs the Estimation Service: it ingests (user_id, segment) pairs
// from RabbitMQ into ClickHouse and serves estimates over gRPC.
//
// Configuration (environment variables):
//
//	RABBITMQ_URL     amqp://guest:guest@localhost:5672/
//	RABBITMQ_QUEUE   estimation.segments
//	CLICKHOUSE_DSN   clickhouse://default:clickhouse@localhost:9000/default
//	GRPC_ADDR        :9090
//	BATCH_SIZE       10000   (also the RabbitMQ prefetch)
//	FLUSH_INTERVAL   10s
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	estimationv1 "github.com/KhashayarKhm/go-challenge/api/gen/estimation/v1"
	"github.com/KhashayarKhm/go-challenge/internal/estimate"
	"github.com/KhashayarKhm/go-challenge/internal/ingest"
	"github.com/KhashayarKhm/go-challenge/internal/store/clickhouse"
	grpctransport "github.com/KhashayarKhm/go-challenge/internal/transport/grpc"
)

type config struct {
	rabbitURL     string
	queue         string
	clickhouseDSN string
	grpcAddr      string
	batch         ingest.Config
}

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(log); err != nil {
		log.Error("estimation service stopped", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := clickhouse.Open(ctx, cfg.clickhouseDSN)
	if err != nil {
		return err
	}
	defer db.Close()

	consumer, err := ingest.NewConsumer(ingest.ConsumerConfig{
		URL:      cfg.rabbitURL,
		Queue:    cfg.queue,
		Prefetch: cfg.batch.BatchSize,
	})
	if err != nil {
		return err
	}
	defer consumer.Close()

	lis, err := net.Listen("tcp", cfg.grpcAddr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", cfg.grpcAddr, err)
	}
	srv := grpc.NewServer()
	estimationv1.RegisterEstimationServiceServer(srv, grpctransport.NewServer(estimate.NewService(db), log))
	reflection.Register(srv) // lets grpcurl discover the API

	batcher := ingest.NewBatcher(db, cfg.batch, log)
	ingestDone := make(chan error, 1)
	go func() { ingestDone <- batcher.Run(ctx, consumer.Deliveries) }()

	serveDone := make(chan error, 1)
	go func() { serveDone <- srv.Serve(lis) }()

	log.Info("estimation service started", "grpc_addr", cfg.grpcAddr, "queue", cfg.queue,
		"batch_size", cfg.batch.BatchSize, "flush_interval", cfg.batch.FlushInterval.String())

	// Stop everything as soon as one part fails or a signal arrives. The
	// batcher flushes its buffer on cancellation before the consumer closes.
	var ingestErr, serveErr error
	ingestStopped := false
	select {
	case <-ctx.Done():
	case ingestErr = <-ingestDone:
		ingestStopped = true
	case serveErr = <-serveDone:
	}
	stop()
	srv.GracefulStop()
	if !ingestStopped {
		ingestErr = <-ingestDone
	}
	log.Info("estimation service stopped")
	return errors.Join(ingestErr, serveErr)
}

func loadConfig() (config, error) {
	batch := ingest.DefaultConfig()
	if v := os.Getenv("BATCH_SIZE"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			return config{}, fmt.Errorf("invalid BATCH_SIZE %q", v)
		}
		batch.BatchSize = n
	}
	if v := os.Getenv("FLUSH_INTERVAL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d <= 0 {
			return config{}, fmt.Errorf("invalid FLUSH_INTERVAL %q", v)
		}
		batch.FlushInterval = d
	}
	if batch.BatchSize > 65535 {
		return config{}, errors.New("BATCH_SIZE must be <= 65535 (RabbitMQ prefetch limit)")
	}

	return config{
		rabbitURL:     env("RABBITMQ_URL", "amqp://guest:guest@localhost:5672/"),
		queue:         env("RABBITMQ_QUEUE", "estimation.segments"),
		clickhouseDSN: env("CLICKHOUSE_DSN", "clickhouse://default:clickhouse@localhost:9000/default"),
		grpcAddr:      env("GRPC_ADDR", ":9090"),
		batch:         batch,
	}, nil
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
