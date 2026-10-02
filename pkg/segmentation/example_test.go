package segmentation_test

import (
	"context"
	"fmt"

	"github.com/KhashayarKhm/go-challenge/pkg/segmentation"
)

// tagUser is what a USS handler looks like: it depends on the Publisher
// interface, not on RabbitMQ.
func tagUser(ctx context.Context, p segmentation.Publisher, userID, segment string) error {
	return p.Publish(ctx, userID, segment)
}

func Example() {
	// In production: segmentation.NewRabbitMQPublisher(segmentation.RabbitMQConfig{URL: "amqp://..."})
	p := &segmentation.InMemoryPublisher{}
	defer p.Close()

	_ = tagUser(context.Background(), p, "u104010", "sports")
	fmt.Println(p.Messages())
	// Output: [{u104010 sports}]
}
