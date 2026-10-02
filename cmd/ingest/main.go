// Command ingest runs the ES ingestion worker: it consumes (user_id, segment)
// pairs from RabbitMQ and writes them to ClickHouse in batches.
//
// It is deployed separately from the gRPC API (cmd/api) so each side scales on
// its own load: add ingest replicas for write throughput (they share the queue
// as competing consumers) without running extra API servers, and vice versa.
//
// Configuration (environment variables):
//
//	RABBITMQ_URL     amqp://guest:guest@localhost:5672/
//	RABBITMQ_QUEUE   estimation.segments
//	RABBITMQ_CONSUMER_TAG  estimation-service-<UTC start time, YYYY-MM-dd HH:mm>
//	CLICKHOUSE_DSN   clickhouse://default:clickhouse@localhost:9000/default
//	BATCH_SIZE       10000   (also the RabbitMQ prefetch, max 65535)
//	FLUSH_INTERVAL   10s
//	PPROF_ENABLED    false   (true starts the pprof HTTP server)
//	PPROF_ADDR       localhost:6060
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/KhashayarKhm/go-challenge/internal/ingest"
	"github.com/KhashayarKhm/go-challenge/internal/pprofserver"
	"github.com/KhashayarKhm/go-challenge/internal/store/clickhouse"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(log); err != nil {
		log.Error("ingest stopped", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	batch, err := batchConfig()
	if err != nil {
		return err
	}
	queue := env("RABBITMQ_QUEUE", "estimation.segments")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	stopPprof, err := pprofserver.StartFromEnv("localhost:6060", log)
	if err != nil {
		return err
	}
	defer stopPprof()

	db, err := clickhouse.Open(ctx, env("CLICKHOUSE_DSN", "clickhouse://default:clickhouse@localhost:9000/default"))
	if err != nil {
		return err
	}
	defer db.Close()

	consumer, err := ingest.NewConsumer(ingest.ConsumerConfig{
		URL:      env("RABBITMQ_URL", "amqp://guest:guest@localhost:5672/"),
		Queue:    queue,
		Prefetch: batch.BatchSize,
		// Set a unique value per replica (e.g. the pod name) to tell them apart.
		ConsumerTag: os.Getenv("RABBITMQ_CONSUMER_TAG"),
	})
	if err != nil {
		return err
	}
	// Closed after Run returns, so the final flush can still ack.
	defer consumer.Close()

	log.Info("ingest started", "queue", queue, "consumer_tag", consumer.Tag, "batch_size", batch.BatchSize,
		"flush_interval", batch.FlushInterval.String())

	// Run flushes its buffer and returns nil on SIGINT/SIGTERM, or returns
	// ErrDeliveriesClosed if the broker connection drops; unacked messages are
	// then requeued by RabbitMQ and the orchestrator restarts the process.
	if err := ingest.NewBatcher(db, batch, log).Run(ctx, consumer.Deliveries); err != nil {
		return err
	}
	log.Info("ingest stopped gracefully")
	return nil
}

func batchConfig() (ingest.Config, error) {
	cfg := ingest.DefaultConfig()
	if v := os.Getenv("BATCH_SIZE"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			return cfg, fmt.Errorf("invalid BATCH_SIZE %q", v)
		}
		cfg.BatchSize = n
	}
	if cfg.BatchSize > 65535 {
		return cfg, errors.New("BATCH_SIZE must be <= 65535 (RabbitMQ prefetch limit)")
	}
	if v := os.Getenv("FLUSH_INTERVAL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d <= 0 {
			return cfg, fmt.Errorf("invalid FLUSH_INTERVAL %q", v)
		}
		cfg.FlushInterval = d
	}
	return cfg, nil
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
