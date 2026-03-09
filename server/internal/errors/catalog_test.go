package errors

import "testing"

func TestDescriptorFor(t *testing.T) {
	got := DescriptorFor(CodeBadRequest)
	if got.HTTPStatus != 400 || got.WSCloseCode != 4400 || got.Retryable {
		t.Fatalf("unexpected descriptor: %+v", got)
	}
}

func TestWSCloseReason(t *testing.T) {
	if WSCloseReason(CodeQueueOverflow) != "4410 QUEUE_OVERFLOW" {
		t.Fatalf("unexpected close reason")
	}
}
