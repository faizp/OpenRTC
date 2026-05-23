package errors

import "testing"

func TestDescriptorFor(t *testing.T) {
	got := DescriptorFor(CodeBadRequest)
	if got.HTTPStatus != 400 || got.WSCloseCode != 4400 || got.Retryable {
		t.Fatalf("unexpected descriptor: %+v", got)
	}
	notFound := DescriptorFor(CodeRoomNotFound)
	if notFound.HTTPStatus != 404 || notFound.WSCloseCode != 4404 || notFound.Retryable {
		t.Fatalf("unexpected room not found descriptor: %+v", notFound)
	}
	conflict := DescriptorFor(CodeRoomConflict)
	if conflict.HTTPStatus != 409 || conflict.WSCloseCode != 4411 || conflict.Retryable {
		t.Fatalf("unexpected room conflict descriptor: %+v", conflict)
	}
	threadNotFound := DescriptorFor(CodeThreadNotFound)
	if threadNotFound.HTTPStatus != 404 || threadNotFound.WSCloseCode != 4414 || threadNotFound.Retryable {
		t.Fatalf("unexpected thread not found descriptor: %+v", threadNotFound)
	}
	threadConflict := DescriptorFor(CodeThreadConflict)
	if threadConflict.HTTPStatus != 409 || threadConflict.WSCloseCode != 4415 || threadConflict.Retryable {
		t.Fatalf("unexpected thread conflict descriptor: %+v", threadConflict)
	}
	inboxNotFound := DescriptorFor(CodeInboxNotificationNotFound)
	if inboxNotFound.HTTPStatus != 404 || inboxNotFound.WSCloseCode != 4416 || inboxNotFound.Retryable {
		t.Fatalf("unexpected inbox notification not found descriptor: %+v", inboxNotFound)
	}
	inboxConflict := DescriptorFor(CodeInboxNotificationConflict)
	if inboxConflict.HTTPStatus != 409 || inboxConflict.WSCloseCode != 4417 || inboxConflict.Retryable {
		t.Fatalf("unexpected inbox notification conflict descriptor: %+v", inboxConflict)
	}
	storageNotFound := DescriptorFor(CodeStorageNotFound)
	if storageNotFound.HTTPStatus != 404 || storageNotFound.WSCloseCode != 4412 || storageNotFound.Retryable {
		t.Fatalf("unexpected storage not found descriptor: %+v", storageNotFound)
	}
	patchFailed := DescriptorFor(CodePatchFailed)
	if patchFailed.HTTPStatus != 422 || patchFailed.WSCloseCode != 4413 || patchFailed.Retryable {
		t.Fatalf("unexpected patch failed descriptor: %+v", patchFailed)
	}
}

func TestWSCloseReason(t *testing.T) {
	if WSCloseReason(CodeQueueOverflow) != "4410 QUEUE_OVERFLOW" {
		t.Fatalf("unexpected close reason")
	}
}
