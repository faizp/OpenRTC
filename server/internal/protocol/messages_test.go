package protocol

import (
	"encoding/json"
	"testing"

	openrtcerr "github.com/openrtc/openrtc/server/internal/errors"
)

func TestParseJoinMessage(t *testing.T) {
	message, err := ParseClientMessage([]byte(`{"t":"JOIN","id":"req-1","room":"tenant-a:room-1","meta":{"limit":50}}`), ParseOptions{
		TenantPrefix: "tenant-a:",
	})
	if err != nil {
		t.Fatalf("parse message: %v", err)
	}

	if message.Type != TypeJoin || message.JoinMeta == nil || message.JoinMeta.Limit != 50 {
		t.Fatalf("unexpected message: %+v", message)
	}
}

func TestParseRejectsUnexpectedField(t *testing.T) {
	_, err := ParseClientMessage([]byte(`{"t":"LEAVE","id":"req-1","room":"tenant-a:room-1","unexpected":true}`), ParseOptions{})
	if err == nil {
		t.Fatalf("expected error")
	}
	parseErr, ok := err.(*ParseError)
	if !ok {
		t.Fatalf("expected parse error, got %T", err)
	}
	if parseErr.Code != openrtcerr.CodeBadRequest || parseErr.Message != "Envelope includes unsupported fields" {
		t.Fatalf("unexpected error: %+v", parseErr)
	}
}

func TestParseRejectsForbiddenRoom(t *testing.T) {
	_, err := ParseClientMessage([]byte(`{"t":"JOIN","id":"req-1","room":"tenant-b:room-1"}`), ParseOptions{
		TenantPrefix: "tenant-a:",
	})
	if err == nil {
		t.Fatalf("expected error")
	}
	parseErr := err.(*ParseError)
	if parseErr.Code != openrtcerr.CodeRoomForbidden {
		t.Fatalf("unexpected error code: %s", parseErr.Code)
	}
}

func TestPaginateMembers(t *testing.T) {
	members, presence, next := PaginateMembers([]string{"c3", "c1", "c2"}, map[string]json.RawMessage{
		"c1": json.RawMessage(`{"online":true}`),
		"c2": json.RawMessage(`{"online":false}`),
	}, 2, "")
	if len(members) != 2 || members[0] != "c1" || next != "c2" {
		t.Fatalf("unexpected page: %#v %s", members, next)
	}
	if len(presence) != 2 {
		t.Fatalf("expected presence subset")
	}
}
