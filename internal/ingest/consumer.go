package ingest

import (
	"errors"
	"fmt"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/KhashayarKhm/go-challenge/pkg/segmentation"
)

// ConsumerConfig describes the RabbitMQ side of ingestion.
type ConsumerConfig struct {
	URL      string
	Queue    string
	Prefetch int // must equal Config.BatchSize, see Config.BatchSize
}

// Consumer owns the AMQP connection that feeds a Batcher.
type Consumer struct {
	conn       *amqp.Connection
	ch         *amqp.Channel
	Deliveries <-chan amqp.Delivery
}

// NewConsumer connects, declares the topology and starts consuming.
//
// Topology (all declarations are idempotent):
//
//	exchange segmentation (direct) --segment.tagged--> <Queue> (quorum)
//	<Queue> --rejected/invalid--> <Queue>.dlx (fanout) --> <Queue>.dlq
//
// A quorum queue is replicated across RabbitMQ nodes (Raft), so accepted
// messages survive the loss of a broker node. Messages are consumed with
// manual acks; Batcher acks them only after they are stored.
func NewConsumer(cfg ConsumerConfig) (*Consumer, error) {
	conn, err := amqp.Dial(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("ingest: dial rabbitmq: %w", err)
	}
	c := &Consumer{conn: conn}
	if err := c.setup(cfg); err != nil {
		conn.Close()
		return nil, err
	}
	return c, nil
}

func (c *Consumer) setup(cfg ConsumerConfig) error {
	ch, err := c.conn.Channel()
	if err != nil {
		return fmt.Errorf("ingest: open channel: %w", err)
	}
	c.ch = ch

	dlx, dlq := cfg.Queue+".dlx", cfg.Queue+".dlq"
	steps := []struct {
		what string
		do   func() error
	}{
		{"declare exchange", func() error {
			return ch.ExchangeDeclare(segmentation.DefaultExchange, amqp.ExchangeDirect, true, false, false, false, nil)
		}},
		{"declare dead-letter exchange", func() error {
			return ch.ExchangeDeclare(dlx, amqp.ExchangeFanout, true, false, false, false, nil)
		}},
		{"declare dead-letter queue", func() error {
			_, err := ch.QueueDeclare(dlq, true, false, false, false, amqp.Table{amqp.QueueTypeArg: amqp.QueueTypeQuorum})
			return err
		}},
		{"bind dead-letter queue", func() error { return ch.QueueBind(dlq, "", dlx, false, nil) }},
		{"declare queue", func() error {
			_, err := ch.QueueDeclare(cfg.Queue, true, false, false, false, amqp.Table{
				amqp.QueueTypeArg:        amqp.QueueTypeQuorum,
				"x-dead-letter-exchange": dlx,
			})
			return err
		}},
		{"bind queue", func() error {
			return ch.QueueBind(cfg.Queue, segmentation.DefaultRoutingKey, segmentation.DefaultExchange, false, nil)
		}},
		{"set prefetch", func() error { return ch.Qos(cfg.Prefetch, 0, false) }},
	}
	for _, s := range steps {
		if err := s.do(); err != nil {
			return fmt.Errorf("ingest: %s: %w", s.what, err)
		}
	}

	deliveries, err := ch.Consume(cfg.Queue, "estimation-service", false, false, false, false, nil)
	if err != nil {
		return fmt.Errorf("ingest: consume: %w", err)
	}
	c.Deliveries = deliveries
	return nil
}

// Close closes the channel (unacked messages return to the queue) and the connection.
func (c *Consumer) Close() error {
	var chErr error
	if c.ch != nil {
		chErr = c.ch.Close()
	}
	return errors.Join(chErr, c.conn.Close())
}
