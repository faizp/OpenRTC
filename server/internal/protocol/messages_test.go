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

func TestParseValidMessageVariants(t *testing.T) {
	join, err := ParseClientMessage([]byte(`{"t":"JOIN","id":"req-1","room":"tenant-a:room-1","meta":{"limit":25,"cursor":"conn-1"}}`), ParseOptions{})
	if err != nil {
		t.Fatalf("parse join with cursor: %v", err)
	}
	if join.JoinMeta == nil || join.JoinMeta.Limit != 25 || join.JoinMeta.Cursor != "conn-1" {
		t.Fatalf("unexpected join meta: %+v", join.JoinMeta)
	}

	leave, err := ParseClientMessage([]byte(`{"t":"LEAVE","id":"req-2","room":"tenant-a:room-1"}`), ParseOptions{})
	if err != nil {
		t.Fatalf("parse leave: %v", err)
	}
	if leave.Type != TypeLeave || leave.Room != "tenant-a:room-1" {
		t.Fatalf("unexpected leave message: %+v", leave)
	}

	emit, err := ParseClientMessage([]byte(`{"t":"EMIT","id":"req-3","room":"tenant-a:room-1","event":"comment.created","payload":{"ok":true},"meta":{"trace_id":"trace-1"}}`), ParseOptions{})
	if err != nil {
		t.Fatalf("parse emit: %v", err)
	}
	if emit.EmitMeta == nil || emit.EmitMeta.TraceID != "trace-1" || string(emit.Payload) != `{"ok":true}` {
		t.Fatalf("unexpected emit message: %+v", emit)
	}

	presence, err := ParseClientMessage([]byte(`{"t":"PRESENCE_SET","id":"req-4","room":"tenant-a:room-1","payload":{"cursor":1}}`), ParseOptions{})
	if err != nil {
		t.Fatalf("parse presence: %v", err)
	}
	if presence.Type != TypePresenceSet || string(presence.Payload) != `{"cursor":1}` {
		t.Fatalf("unexpected presence message: %+v", presence)
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

func TestParseRejectsUnsafeIdentifiers(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{"unsafe id", `{"t":"JOIN","id":"req 1","room":"tenant-a:room-1"}`},
		{"unsafe room", `{"t":"JOIN","id":"req-1","room":"tenant-a:room/1"}`},
		{"unsafe event", `{"t":"EMIT","id":"req-1","room":"tenant-a:room-1","event":"chat message","payload":{"ok":true}}`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseClientMessage([]byte(tc.raw), ParseOptions{})
			if err == nil {
				t.Fatalf("expected error")
			}
			parseErr := err.(*ParseError)
			if parseErr.Code != openrtcerr.CodeBadRequest {
				t.Fatalf("unexpected error code: %s", parseErr.Code)
			}
		})
	}
}

func TestParseRejectsInvalidMessageShapes(t *testing.T) {
	longPayload := make([]byte, 0, 64)
	longPayload = append(longPayload, `{"t":"EMIT","id":"req-1","room":"tenant-a:room-1","event":"event","payload":"`...)
	longPayload = append(longPayload, []byte("0123456789")...)
	longPayload = append(longPayload, `"}`...)

	tests := []struct {
		name string
		raw  []byte
		opts ParseOptions
		want openrtcerr.Code
	}{
		{
			name: "envelope too large",
			raw:  []byte(`{"t":"JOIN","id":"req-1","room":"tenant-a:room-1"}`),
			opts: ParseOptions{MaxEnvelopeBytes: 4},
			want: openrtcerr.CodePayloadTooLarge,
		},
		{
			name: "invalid json",
			raw:  []byte(`{`),
			want: openrtcerr.CodeBadRequest,
		},
		{
			name: "missing type",
			raw:  []byte(`{"id":"req-1","room":"tenant-a:room-1"}`),
			want: openrtcerr.CodeBadRequest,
		},
		{
			name: "missing id",
			raw:  []byte(`{"t":"JOIN","room":"tenant-a:room-1"}`),
			want: openrtcerr.CodeBadRequest,
		},
		{
			name: "non string id",
			raw:  []byte(`{"t":"JOIN","id":1,"room":"tenant-a:room-1"}`),
			want: openrtcerr.CodeBadRequest,
		},
		{
			name: "unsupported type",
			raw:  []byte(`{"t":"SYNC","id":"req-1","room":"tenant-a:room-1"}`),
			want: openrtcerr.CodeBadRequest,
		},
		{
			name: "join meta non object",
			raw:  []byte(`{"t":"JOIN","id":"req-1","room":"tenant-a:room-1","meta":[]}`),
			want: openrtcerr.CodeBadRequest,
		},
		{
			name: "missing room",
			raw:  []byte(`{"t":"JOIN","id":"req-1"}`),
			want: openrtcerr.CodeBadRequest,
		},
		{
			name: "payload too large",
			raw:  longPayload,
			opts: ParseOptions{MaxPayloadBytes: 4},
			want: openrtcerr.CodePayloadTooLarge,
		},
		{
			name: "join meta unsupported field",
			raw:  []byte(`{"t":"JOIN","id":"req-1","room":"tenant-a:room-1","meta":{"unknown":true}}`),
			want: openrtcerr.CodeBadRequest,
		},
		{
			name: "join meta bad limit",
			raw:  []byte(`{"t":"JOIN","id":"req-1","room":"tenant-a:room-1","meta":{"limit":201}}`),
			want: openrtcerr.CodeBadRequest,
		},
		{
			name: "join meta empty cursor",
			raw:  []byte(`{"t":"JOIN","id":"req-1","room":"tenant-a:room-1","meta":{"cursor":""}}`),
			want: openrtcerr.CodeBadRequest,
		},
		{
			name: "emit missing event",
			raw:  []byte(`{"t":"EMIT","id":"req-1","room":"tenant-a:room-1","payload":{"ok":true}}`),
			want: openrtcerr.CodeBadRequest,
		},
		{
			name: "emit missing payload",
			raw:  []byte(`{"t":"EMIT","id":"req-1","room":"tenant-a:room-1","event":"event"}`),
			want: openrtcerr.CodeBadRequest,
		},
		{
			name: "emit unsupported meta field",
			raw:  []byte(`{"t":"EMIT","id":"req-1","room":"tenant-a:room-1","event":"event","payload":{},"meta":{"other":"x"}}`),
			want: openrtcerr.CodeBadRequest,
		},
		{
			name: "emit meta non object",
			raw:  []byte(`{"t":"EMIT","id":"req-1","room":"tenant-a:room-1","event":"event","payload":{},"meta":[]}`),
			want: openrtcerr.CodeBadRequest,
		},
		{
			name: "emit bad trace id",
			raw:  []byte(`{"t":"EMIT","id":"req-1","room":"tenant-a:room-1","event":"event","payload":{},"meta":{"trace_id":""}}`),
			want: openrtcerr.CodeBadRequest,
		},
		{
			name: "presence requires object",
			raw:  []byte(`{"t":"PRESENCE_SET","id":"req-1","room":"tenant-a:room-1","payload":[]}`),
			want: openrtcerr.CodeBadRequest,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseClientMessage(tc.raw, tc.opts)
			if err == nil {
				t.Fatalf("expected parse error")
			}
			parseErr := err.(*ParseError)
			if parseErr.Code != tc.want {
				t.Fatalf("expected %s, got %+v", tc.want, parseErr)
			}
		})
	}
}

func TestIdentifierValidators(t *testing.T) {
	if err := ValidateMessageID("req-1"); err != nil {
		t.Fatalf("expected safe message id: %v", err)
	}
	if err := ValidateMessageID(""); err == nil {
		t.Fatalf("expected empty message id rejection")
	}
	if err := ValidateMessageID(string(make([]byte, MaxMessageIDBytes+1))); err == nil {
		t.Fatalf("expected oversized message id rejection")
	}

	if err := ValidateRoomName("tenant-a:room-1"); err != nil {
		t.Fatalf("expected safe room name: %v", err)
	}
	if err := ValidateRoomName(""); err == nil {
		t.Fatalf("expected empty room rejection")
	}
	if err := ValidateRoomName(string(make([]byte, MaxRoomNameBytes+1))); err == nil {
		t.Fatalf("expected oversized room rejection")
	}

	if err := ValidateEventName("doc.update"); err != nil {
		t.Fatalf("expected safe event name: %v", err)
	}
	if err := ValidateEventName(""); err == nil {
		t.Fatalf("expected empty event rejection")
	}
	if err := ValidateEventName(string(make([]byte, MaxEventNameBytes+1))); err == nil {
		t.Fatalf("expected oversized event rejection")
	}
}

func TestValidateRoomPrefix(t *testing.T) {
	if err := ValidateRoomPrefix("tenant-a:doc-"); err != nil {
		t.Fatalf("expected safe prefix: %v", err)
	}
	if err := ValidateRoomPrefix("tenant-a:bad prefix"); err == nil {
		t.Fatalf("expected unsafe prefix rejection")
	}
}

func TestValidateConnectionID(t *testing.T) {
	if err := ValidateConnectionID("conn-1"); err != nil {
		t.Fatalf("expected safe connection id: %v", err)
	}
	if err := ValidateConnectionID(""); err == nil {
		t.Fatalf("expected empty connection id rejection")
	}
	if err := ValidateConnectionID("conn/1"); err == nil {
		t.Fatalf("expected unsafe connection id rejection")
	}
	if err := ValidateConnectionID(string(make([]byte, MaxConnectionIDBytes+1))); err == nil {
		t.Fatalf("expected oversized connection id rejection")
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

	members, presence, next = PaginateMembers([]string{"c3", "c1", "c2"}, map[string]json.RawMessage{
		"c3": json.RawMessage(`{"online":true}`),
	}, 0, "c2")
	if len(members) != 1 || members[0] != "c3" || next != "" {
		t.Fatalf("unexpected cursor page: %#v %s", members, next)
	}
	if len(presence) != 1 || string(presence["c3"]) != `{"online":true}` {
		t.Fatalf("unexpected cursor presence: %#v", presence)
	}
}
