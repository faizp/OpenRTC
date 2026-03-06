package sdkgo

import "context"

type PublishRequest struct {
	Room    string
	Event   string
	Payload any
}

type Publisher interface {
	Publish(ctx context.Context, req PublishRequest) error
}
