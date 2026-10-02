// Command uss-sim simulates the User Segmentation Service: it publishes random
// (user_id, segment) pairs through pkg/segmentation, exactly as USS would.
// It is a demo and end-to-end testing tool, not part of ES.
//
//	go run ./cmd/uss-sim -users 1000 -events 5000 -segments sports,news,tech
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"math/rand/v2"
	"os"
	"os/signal"
	"strings"

	"github.com/KhashayarKhm/go-challenge/pkg/segmentation"
)

func main() {
	url := flag.String("url", "amqp://guest:guest@localhost:5672/", "RabbitMQ URL")
	users := flag.Int("users", 1000, "number of distinct users to draw from")
	events := flag.Int("events", 5000, "number of pairs to publish (duplicates are expected)")
	segments := flag.String("segments", "sports,news,tech", "comma-separated segments")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	pub, err := segmentation.NewRabbitMQPublisher(segmentation.RabbitMQConfig{URL: *url})
	if err != nil {
		log.Fatal(err)
	}
	defer pub.Close()

	segs := strings.Split(*segments, ",")
	distinct := make(map[string]map[string]struct{}, len(segs))
	for _, s := range segs {
		distinct[s] = map[string]struct{}{}
	}

	for i := 0; i < *events; i++ {
		user := fmt.Sprintf("u%d", rand.IntN(*users))
		seg := segs[rand.IntN(len(segs))]
		if err := pub.Publish(ctx, user, seg); err != nil {
			log.Fatalf("publish %d: %v", i, err)
		}
		distinct[seg][user] = struct{}{}
	}

	fmt.Printf("published %d pairs; distinct users per segment (expected estimates):\n", *events)
	for _, s := range segs {
		fmt.Printf("  %-10s %d\n", s, len(distinct[s]))
	}
}
