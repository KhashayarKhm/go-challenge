// Package segmentation is the client library the User Segmentation Service (USS)
// imports to send (user_id, segment) pairs to the Estimation Service (ES).
//
// The package owns the wire contract (Message) so that the producer (USS) and the
// consumer (ES) can never disagree on the payload format: ES decodes incoming
// messages with DecodeMessage from this same package.
package segmentation

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Default RabbitMQ topology shared by USS and ES. The exchange is durable and
// "direct" so ES can bind exactly one queue to RoutingKey.
const (
	DefaultExchange   = "segmentation"
	DefaultRoutingKey = "segment.tagged"
)

var (
	ErrEmptyUserID  = errors.New("segmentation: user_id must not be empty")
	ErrEmptySegment = errors.New("segmentation: segment must not be empty")
)

// Message is the payload published for every tagging event. It deliberately
// carries only what the challenge says USS sends: the user and the segment.
// The event time travels as the AMQP "timestamp" property, set by the adapter.
type Message struct {
	UserID  string `json:"user_id"`
	Segment string `json:"segment"`
}

// Validate rejects messages that can never be stored meaningfully. Such
// messages are dead-lettered by ES instead of being retried forever.
func (m Message) Validate() error {
	if strings.TrimSpace(m.UserID) == "" {
		return ErrEmptyUserID
	}
	if strings.TrimSpace(m.Segment) == "" {
		return ErrEmptySegment
	}
	return nil
}

// Encode validates and serializes the message as JSON. JSON is used over a
// binary format because the payload is tiny and it keeps the queue inspectable
// from the RabbitMQ management UI.
func (m Message) Encode() ([]byte, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(m)
}

// DecodeMessage parses and validates a message body produced by Encode.
func DecodeMessage(body []byte) (Message, error) {
	var m Message
	if err := json.Unmarshal(body, &m); err != nil {
		return Message{}, fmt.Errorf("segmentation: decode message: %w", err)
	}
	if err := m.Validate(); err != nil {
		return Message{}, err
	}
	return m, nil
}
