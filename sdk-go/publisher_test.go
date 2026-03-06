package sdkgo

import (
	"context"
	"testing"
)

type noopPublisher struct{}

func (noopPublisher) Publish(_ context.Context, _ PublishRequest) error {
	return nil
}

func TestPublisherContract(t *testing.T) {
	var publisher Publisher = noopPublisher{}
	if err := publisher.Publish(context.Background(), PublishRequest{Room: "tenant:room", Event: "evt", Payload: map[string]string{"ok": "true"}}); err != nil {
		t.Fatalf("publish returned error: %v", err)
	}
}
