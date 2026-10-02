package segmentation

import (
	"context"
	"errors"
	"fmt"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// ErrNotConfirmed is returned when the broker negatively acknowledges a
// message (it could not take responsibility for it). The caller should retry.
var ErrNotConfirmed = errors.New("segmentation: message was not confirmed by the broker")

// RabbitMQConfig configures RabbitMQPublisher. Empty Exchange and RoutingKey
// fall back to DefaultExchange and DefaultRoutingKey.
type RabbitMQConfig struct {
	URL        string
	Exchange   string
	RoutingKey string
}

// RabbitMQPublisher is the RabbitMQ adapter of Publisher.
//
// Reliability choices:
//   - publisher confirms: Publish returns only after the broker has taken
//     responsibility for the message, so a nil error means "will not be lost";
//   - persistent delivery mode: the message survives a broker restart;
//   - the AMQP timestamp is set at publish time, so ES buckets the pair by when
//     USS tagged the user, not by when ES happened to consume it (a queue
//     backlog or a replay would otherwise shift users to the wrong day).
//
// It is safe for concurrent use; amqp091 serializes publishes on the channel
// and confirmations are awaited outside that lock.
//
// Reconnection is intentionally not handled here: a closed connection makes
// Publish fail and the caller decides whether to recreate the publisher.
type RabbitMQPublisher struct {
	conn       *amqp.Connection
	ch         *amqp.Channel
	exchange   string
	routingKey string
	now        func() time.Time
}

var _ Publisher = (*RabbitMQPublisher)(nil)

// NewRabbitMQPublisher connects to RabbitMQ, enables confirm mode and declares
// the (idempotent) durable exchange. The queue itself is declared by ES, which
// owns its consumption side; ES must therefore have started once before USS
// publishes, otherwise RabbitMQ confirms and drops the unroutable messages.
func NewRabbitMQPublisher(cfg RabbitMQConfig) (*RabbitMQPublisher, error) {
	if cfg.Exchange == "" {
		cfg.Exchange = DefaultExchange
	}
	if cfg.RoutingKey == "" {
		cfg.RoutingKey = DefaultRoutingKey
	}

	conn, err := amqp.Dial(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("segmentation: dial rabbitmq: %w", err)
	}
	ch, err := conn.Channel()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("segmentation: open channel: %w", err)
	}
	if err := ch.Confirm(false); err != nil {
		conn.Close()
		return nil, fmt.Errorf("segmentation: enable confirms: %w", err)
	}
	if err := ch.ExchangeDeclare(cfg.Exchange, amqp.ExchangeDirect, true, false, false, false, nil); err != nil {
		conn.Close()
		return nil, fmt.Errorf("segmentation: declare exchange: %w", err)
	}

	return &RabbitMQPublisher{
		conn:       conn,
		ch:         ch,
		exchange:   cfg.Exchange,
		routingKey: cfg.RoutingKey,
		now:        time.Now,
	}, nil
}

// Publish sends the pair and blocks until the broker confirms it or ctx ends.
func (p *RabbitMQPublisher) Publish(ctx context.Context, userID, segment string) error {
	body, err := Message{UserID: userID, Segment: segment}.Encode()
	if err != nil {
		return err
	}

	confirm, err := p.ch.PublishWithDeferredConfirmWithContext(ctx, p.exchange, p.routingKey, false, false, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		Timestamp:    p.now().UTC(),
		Body:         body,
	})
	if err != nil {
		return fmt.Errorf("segmentation: publish: %w", err)
	}

	acked, err := confirm.WaitContext(ctx)
	if err != nil {
		return fmt.Errorf("segmentation: wait for confirm: %w", err)
	}
	if !acked {
		return ErrNotConfirmed
	}
	return nil
}

// Close closes the channel and the connection.
func (p *RabbitMQPublisher) Close() error {
	return errors.Join(p.ch.Close(), p.conn.Close())
}
