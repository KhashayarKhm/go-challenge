package segmentation

import (
	"context"
	"errors"
	"testing"
)

func TestMessageValidate(t *testing.T) {
	tests := []struct {
		name string
		msg  Message
		want error
	}{
		{"valid", Message{UserID: "u104010", Segment: "sports"}, nil},
		{"empty user", Message{UserID: "", Segment: "sports"}, ErrEmptyUserID},
		{"blank user", Message{UserID: "  ", Segment: "sports"}, ErrEmptyUserID},
		{"empty segment", Message{UserID: "u1", Segment: ""}, ErrEmptySegment},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.msg.Validate(); !errors.Is(got, tt.want) {
				t.Fatalf("Validate() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	in := Message{UserID: "u104010", Segment: "sports"}
	body, err := in.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if string(body) != `{"user_id":"u104010","segment":"sports"}` {
		t.Fatalf("unexpected wire format: %s", body)
	}
	out, err := DecodeMessage(body)
	if err != nil {
		t.Fatalf("DecodeMessage: %v", err)
	}
	if out != in {
		t.Fatalf("round trip = %+v, want %+v", out, in)
	}
}

func TestEncodeRejectsInvalid(t *testing.T) {
	if _, err := (Message{Segment: "sports"}).Encode(); !errors.Is(err, ErrEmptyUserID) {
		t.Fatalf("Encode() error = %v, want %v", err, ErrEmptyUserID)
	}
}

func TestDecodeMessageErrors(t *testing.T) {
	if _, err := DecodeMessage([]byte("not json")); err == nil {
		t.Fatal("expected error for malformed JSON")
	}
	if _, err := DecodeMessage([]byte(`{"user_id":"u1"}`)); !errors.Is(err, ErrEmptySegment) {
		t.Fatalf("expected ErrEmptySegment, got %v", err)
	}
}

func TestInMemoryPublisher(t *testing.T) {
	ctx := context.Background()
	p := &InMemoryPublisher{}

	if err := p.Publish(ctx, "u1", "sports"); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if err := p.Publish(ctx, "", "sports"); !errors.Is(err, ErrEmptyUserID) {
		t.Fatalf("expected validation error, got %v", err)
	}

	p.Err = errors.New("broker down")
	if err := p.Publish(ctx, "u2", "news"); !errors.Is(err, p.Err) {
		t.Fatalf("expected injected error, got %v", err)
	}

	got := p.Messages()
	if len(got) != 1 || got[0] != (Message{UserID: "u1", Segment: "sports"}) {
		t.Fatalf("Messages() = %+v", got)
	}
}

func TestInMemoryPublisherCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := (&InMemoryPublisher{}).Publish(ctx, "u1", "sports"); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}
