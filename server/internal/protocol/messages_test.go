package protocol

import (
	"testing"

	openrtcerr "github.com/openrtc/openrtc/server/internal/errors"
)

func TestParseJoinMessage(t *testing.T) {
	message, err := ParseClientMessage([]byte(`{"t":"JOIN","id":"req-1","room":"tenant-a:room-1","meta":{"limit":50,"after_seq":42}}`), ParseOptions{
		TenantPrefix: "tenant-a:",
	})
	if err != nil {
		t.Fatalf("parse message: %v", err)
	}

	if message.Type != TypeJoin || message.JoinMeta == nil || message.JoinMeta.Limit != 50 || message.JoinMeta.AfterSequence != 42 {
		t.Fatalf("unexpected message: %+v", message)
	}
}

func TestParseErrorMessage(t *testing.T) {
	err := &ParseError{Message: "bad request"}
	if err.Error() != "bad request" {
		t.Fatalf("unexpected parse error string: %q", err.Error())
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

	eventAck, err := ParseClientMessage([]byte(`{"t":"EVENT_ACK","id":"req-ack","room":"tenant-a:room-1","meta":{"seq":42}}`), ParseOptions{})
	if err != nil {
		t.Fatalf("parse event ack: %v", err)
	}
	if eventAck.Type != TypeEventAck || eventAck.EventAckMeta == nil || eventAck.EventAckMeta.Sequence != 42 {
		t.Fatalf("unexpected event ack message: %+v", eventAck)
	}

	presence, err := ParseClientMessage([]byte(`{"t":"PRESENCE_SET","id":"req-4","room":"tenant-a:room-1","payload":{"cursor":1}}`), ParseOptions{})
	if err != nil {
		t.Fatalf("parse presence: %v", err)
	}
	if presence.Type != TypePresenceSet || string(presence.Payload) != `{"cursor":1}` {
		t.Fatalf("unexpected presence message: %+v", presence)
	}

	storageGet, err := ParseClientMessage([]byte(`{"t":"STORAGE_GET","id":"req-5","room":"tenant-a:room-1"}`), ParseOptions{})
	if err != nil {
		t.Fatalf("parse storage get: %v", err)
	}
	if storageGet.Type != TypeStorageGet || storageGet.Room != "tenant-a:room-1" {
		t.Fatalf("unexpected storage get message: %+v", storageGet)
	}

	storageSet, err := ParseClientMessage([]byte(`{"t":"STORAGE_SET","id":"req-6","room":"tenant-a:room-1","payload":{"liveblocksType":"LiveObject","data":{}},"meta":{"op_id":"op-1","expected_seq":0}}`), ParseOptions{})
	if err != nil {
		t.Fatalf("parse storage set: %v", err)
	}
	if storageSet.Type != TypeStorageSet || storageSet.StorageMeta == nil || storageSet.StorageMeta.OpID != "op-1" || !storageSet.StorageMeta.ExpectedSequenceSet || storageSet.StorageMeta.ExpectedSequence != 0 {
		t.Fatalf("unexpected storage set message: %+v", storageSet)
	}

	storagePatch, err := ParseClientMessage([]byte(`{"t":"STORAGE_PATCH","id":"req-7","room":"tenant-a:room-1","payload":[{"op":"add","path":"/title","value":"Draft"}],"meta":{"op_id":"op-2","expected_seq":3}}`), ParseOptions{})
	if err != nil {
		t.Fatalf("parse storage patch: %v", err)
	}
	if storagePatch.Type != TypeStoragePatch || storagePatch.StorageMeta == nil || storagePatch.StorageMeta.OpID != "op-2" || !storagePatch.StorageMeta.ExpectedSequenceSet || storagePatch.StorageMeta.ExpectedSequence != 3 {
		t.Fatalf("unexpected storage patch message: %+v", storagePatch)
	}

	uppercaseAndUnderscore, err := ParseClientMessage([]byte(`{"t":"JOIN","id":"REQ_1","room":"TENANT_A:ROOM_1"}`), ParseOptions{})
	if err != nil {
		t.Fatalf("parse uppercase and underscore identifiers: %v", err)
	}
	if uppercaseAndUnderscore.ID != "REQ_1" || uppercaseAndUnderscore.Room != "TENANT_A:ROOM_1" {
		t.Fatalf("unexpected uppercase identifier message: %+v", uppercaseAndUnderscore)
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
			name: "join meta bad after sequence",
			raw:  []byte(`{"t":"JOIN","id":"req-1","room":"tenant-a:room-1","meta":{"after_seq":-1}}`),
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
			name: "event ack rejects payload",
			raw:  []byte(`{"t":"EVENT_ACK","id":"req-1","room":"tenant-a:room-1","payload":{},"meta":{"seq":1}}`),
			want: openrtcerr.CodeBadRequest,
		},
		{
			name: "event ack missing meta",
			raw:  []byte(`{"t":"EVENT_ACK","id":"req-1","room":"tenant-a:room-1"}`),
			want: openrtcerr.CodeBadRequest,
		},
		{
			name: "event ack unsupported meta",
			raw:  []byte(`{"t":"EVENT_ACK","id":"req-1","room":"tenant-a:room-1","meta":{"seq":1,"extra":true}}`),
			want: openrtcerr.CodeBadRequest,
		},
		{
			name: "event ack zero sequence",
			raw:  []byte(`{"t":"EVENT_ACK","id":"req-1","room":"tenant-a:room-1","meta":{"seq":0}}`),
			want: openrtcerr.CodeBadRequest,
		},
		{
			name: "presence requires object",
			raw:  []byte(`{"t":"PRESENCE_SET","id":"req-1","room":"tenant-a:room-1","payload":[]}`),
			want: openrtcerr.CodeBadRequest,
		},
		{
			name: "storage get rejects meta",
			raw:  []byte(`{"t":"STORAGE_GET","id":"req-1","room":"tenant-a:room-1","meta":{"op_id":"op-1"}}`),
			want: openrtcerr.CodeBadRequest,
		},
		{
			name: "storage get rejects payload",
			raw:  []byte(`{"t":"STORAGE_GET","id":"req-1","room":"tenant-a:room-1","payload":{}}`),
			want: openrtcerr.CodeBadRequest,
		},
		{
			name: "storage set requires object",
			raw:  []byte(`{"t":"STORAGE_SET","id":"req-1","room":"tenant-a:room-1","payload":[]}`),
			want: openrtcerr.CodeBadRequest,
		},
		{
			name: "storage patch requires array",
			raw:  []byte(`{"t":"STORAGE_PATCH","id":"req-1","room":"tenant-a:room-1","payload":{}}`),
			want: openrtcerr.CodeBadRequest,
		},
		{
			name: "storage meta unsupported field",
			raw:  []byte(`{"t":"STORAGE_PATCH","id":"req-1","room":"tenant-a:room-1","payload":[],"meta":{"trace_id":"x"}}`),
			want: openrtcerr.CodeBadRequest,
		},
		{
			name: "storage meta bad op id",
			raw:  []byte(`{"t":"STORAGE_PATCH","id":"req-1","room":"tenant-a:room-1","payload":[],"meta":{"op_id":"bad id"}}`),
			want: openrtcerr.CodeBadRequest,
		},
		{
			name: "storage meta bad expected sequence",
			raw:  []byte(`{"t":"STORAGE_PATCH","id":"req-1","room":"tenant-a:room-1","payload":[],"meta":{"expected_seq":-1}}`),
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
