package protocol

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"

	openrtcerr "github.com/openrtc/openrtc/server/internal/errors"
)

const (
	MaxPayloadBytesDefault  = 16 * 1024
	MaxEnvelopeBytesDefault = 20 * 1024
	MaxMessageIDBytes       = 128
	MaxRoomNameBytes        = 256
	MaxEventNameBytes       = 128
	MaxConnectionIDBytes    = 128
)

type MessageType string

const (
	TypeJoin         MessageType = "JOIN"
	TypeLeave        MessageType = "LEAVE"
	TypeEmit         MessageType = "EMIT"
	TypePresenceSet  MessageType = "PRESENCE_SET"
	TypeStorageGet   MessageType = "STORAGE_GET"
	TypeStorageSet   MessageType = "STORAGE_SET"
	TypeStoragePatch MessageType = "STORAGE_PATCH"
)

type JoinMeta struct {
	Limit         int    `json:"limit,omitempty"`
	Cursor        string `json:"cursor,omitempty"`
	AfterSequence uint64 `json:"after_seq,omitempty"`
}

type EmitMeta struct {
	TraceID string `json:"trace_id,omitempty"`
}

type StorageMeta struct {
	OpID string `json:"op_id,omitempty"`
}

type Message struct {
	Type        MessageType
	ID          string
	Room        string
	Event       string
	Payload     json.RawMessage
	JoinMeta    *JoinMeta
	EmitMeta    *EmitMeta
	StorageMeta *StorageMeta
}

type ParseOptions struct {
	MaxEnvelopeBytes int
	MaxPayloadBytes  int
	TenantPrefix     string
}

type ParseError struct {
	Code    openrtcerr.Code
	Message string
}

func (e *ParseError) Error() string {
	return e.Message
}

func ParseClientMessage(raw []byte, options ParseOptions) (Message, error) {
	maxEnvelope := options.MaxEnvelopeBytes
	if maxEnvelope == 0 {
		maxEnvelope = MaxEnvelopeBytesDefault
	}
	maxPayload := options.MaxPayloadBytes
	if maxPayload == 0 {
		maxPayload = MaxPayloadBytesDefault
	}

	if len(raw) > maxEnvelope {
		return Message{}, &ParseError{Code: openrtcerr.CodePayloadTooLarge, Message: "Envelope exceeds max size"}
	}

	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return Message{}, &ParseError{Code: openrtcerr.CodeBadRequest, Message: "Message must be valid JSON"}
	}

	allowedKeys := map[string]struct{}{
		"t": {}, "id": {}, "room": {}, "event": {}, "payload": {}, "meta": {},
	}
	for key := range envelope {
		if _, ok := allowedKeys[key]; !ok {
			return Message{}, &ParseError{Code: openrtcerr.CodeBadRequest, Message: "Envelope includes unsupported fields"}
		}
	}

	var message Message
	if err := readRequiredString(envelope, "t", "Message type `t` is required", &message.Type); err != nil {
		return Message{}, err
	}
	if err := readRequiredString(envelope, "id", "Message id `id` is required", &message.ID); err != nil {
		return Message{}, err
	}
	if err := ValidateMessageID(message.ID); err != nil {
		return Message{}, err
	}

	switch message.Type {
	case TypeJoin, TypeLeave, TypeEmit, TypePresenceSet, TypeStorageGet, TypeStorageSet, TypeStoragePatch:
	default:
		return Message{}, &ParseError{Code: openrtcerr.CodeBadRequest, Message: fmt.Sprintf("Unsupported message type: %s", message.Type)}
	}

	if err := readRequiredString(envelope, "room", "Room is required for this message type", &message.Room); err != nil {
		return Message{}, err
	}
	if err := ValidateRoomName(message.Room); err != nil {
		return Message{}, err
	}
	if options.TenantPrefix != "" && !bytes.HasPrefix([]byte(message.Room), []byte(options.TenantPrefix)) {
		return Message{}, &ParseError{Code: openrtcerr.CodeRoomForbidden, Message: "Room is outside the allowed tenant prefix"}
	}

	if payload, ok := envelope["payload"]; ok && len(payload) > 0 {
		if len(payload) > maxPayload {
			return Message{}, &ParseError{Code: openrtcerr.CodePayloadTooLarge, Message: "Payload exceeds max size"}
		}
		message.Payload = payload
	}

	meta := envelope["meta"]
	switch message.Type {
	case TypeJoin:
		if len(meta) > 0 {
			var joinMeta map[string]json.RawMessage
			if err := json.Unmarshal(meta, &joinMeta); err != nil {
				return Message{}, &ParseError{Code: openrtcerr.CodeBadRequest, Message: "Meta must be an object when present"}
			}
			for key := range joinMeta {
				if key != "limit" && key != "cursor" && key != "after_seq" {
					return Message{}, &ParseError{Code: openrtcerr.CodeBadRequest, Message: "JOIN meta includes unsupported fields"}
				}
			}
			parsed := &JoinMeta{}
			if limitRaw, ok := joinMeta["limit"]; ok {
				if err := json.Unmarshal(limitRaw, &parsed.Limit); err != nil || parsed.Limit < 1 || parsed.Limit > 200 {
					return Message{}, &ParseError{Code: openrtcerr.CodeBadRequest, Message: "JOIN meta.limit must be an integer between 1 and 200"}
				}
			}
			if cursorRaw, ok := joinMeta["cursor"]; ok {
				if err := json.Unmarshal(cursorRaw, &parsed.Cursor); err != nil || parsed.Cursor == "" {
					return Message{}, &ParseError{Code: openrtcerr.CodeBadRequest, Message: "JOIN meta.cursor must be a non-empty string"}
				}
			}
			if afterSequenceRaw, ok := joinMeta["after_seq"]; ok {
				if err := json.Unmarshal(afterSequenceRaw, &parsed.AfterSequence); err != nil {
					return Message{}, &ParseError{Code: openrtcerr.CodeBadRequest, Message: "JOIN meta.after_seq must be an integer greater than or equal to 0"}
				}
			}
			message.JoinMeta = parsed
		}
	case TypeLeave:
	case TypeEmit:
		if err := readRequiredString(envelope, "event", "EMIT requires `event`", &message.Event); err != nil {
			return Message{}, err
		}
		if err := ValidateEventName(message.Event); err != nil {
			return Message{}, err
		}
		if len(message.Payload) == 0 {
			return Message{}, &ParseError{Code: openrtcerr.CodeBadRequest, Message: "EMIT requires `payload`"}
		}
		if len(meta) > 0 {
			var emitMeta map[string]json.RawMessage
			if err := json.Unmarshal(meta, &emitMeta); err != nil {
				return Message{}, &ParseError{Code: openrtcerr.CodeBadRequest, Message: "Meta must be an object when present"}
			}
			for key := range emitMeta {
				if key != "trace_id" {
					return Message{}, &ParseError{Code: openrtcerr.CodeBadRequest, Message: "EMIT meta includes unsupported fields"}
				}
			}
			parsed := &EmitMeta{}
			if traceRaw, ok := emitMeta["trace_id"]; ok {
				if err := json.Unmarshal(traceRaw, &parsed.TraceID); err != nil || parsed.TraceID == "" {
					return Message{}, &ParseError{Code: openrtcerr.CodeBadRequest, Message: "EMIT meta.trace_id must be a non-empty string"}
				}
			}
			message.EmitMeta = parsed
		}
	case TypePresenceSet:
		if len(message.Payload) == 0 || !json.Valid(message.Payload) || message.Payload[0] != '{' {
			return Message{}, &ParseError{Code: openrtcerr.CodeBadRequest, Message: "PRESENCE_SET requires object payload"}
		}
	case TypeStorageGet:
		if len(message.Payload) > 0 {
			return Message{}, &ParseError{Code: openrtcerr.CodeBadRequest, Message: "STORAGE_GET does not accept payload"}
		}
		if len(meta) > 0 {
			return Message{}, &ParseError{Code: openrtcerr.CodeBadRequest, Message: "STORAGE_GET does not accept meta"}
		}
	case TypeStorageSet, TypeStoragePatch:
		if len(message.Payload) == 0 || !json.Valid(message.Payload) {
			return Message{}, &ParseError{Code: openrtcerr.CodeBadRequest, Message: fmt.Sprintf("%s requires JSON payload", message.Type)}
		}
		trimmedPayload := bytes.TrimSpace(message.Payload)
		if message.Type == TypeStorageSet && (len(trimmedPayload) == 0 || trimmedPayload[0] != '{') {
			return Message{}, &ParseError{Code: openrtcerr.CodeBadRequest, Message: "STORAGE_SET requires object payload"}
		}
		if message.Type == TypeStoragePatch && (len(trimmedPayload) == 0 || trimmedPayload[0] != '[') {
			return Message{}, &ParseError{Code: openrtcerr.CodeBadRequest, Message: "STORAGE_PATCH requires JSON Patch array payload"}
		}
		if len(meta) > 0 {
			var storageMeta map[string]json.RawMessage
			if err := json.Unmarshal(meta, &storageMeta); err != nil {
				return Message{}, &ParseError{Code: openrtcerr.CodeBadRequest, Message: "Meta must be an object when present"}
			}
			for key := range storageMeta {
				if key != "op_id" {
					return Message{}, &ParseError{Code: openrtcerr.CodeBadRequest, Message: fmt.Sprintf("%s meta includes unsupported fields", message.Type)}
				}
			}
			parsed := &StorageMeta{}
			if opIDRaw, ok := storageMeta["op_id"]; ok {
				if err := json.Unmarshal(opIDRaw, &parsed.OpID); err != nil || parsed.OpID == "" {
					return Message{}, &ParseError{Code: openrtcerr.CodeBadRequest, Message: fmt.Sprintf("%s meta.op_id must be a non-empty string", message.Type)}
				}
				if err := ValidateMessageID(parsed.OpID); err != nil {
					return Message{}, err
				}
			}
			message.StorageMeta = parsed
		}
	}

	return message, nil
}

func readRequiredString[T ~string](envelope map[string]json.RawMessage, key string, failure string, dest *T) error {
	raw := envelope[key]
	if len(raw) == 0 {
		return &ParseError{Code: openrtcerr.CodeBadRequest, Message: failure}
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil || value == "" {
		return &ParseError{Code: openrtcerr.CodeBadRequest, Message: failure}
	}
	*dest = T(value)
	return nil
}

func ValidateMessageID(id string) error {
	if id == "" {
		return &ParseError{Code: openrtcerr.CodeBadRequest, Message: "Message id `id` is required"}
	}
	if len(id) > MaxMessageIDBytes || !isSafeIdentifier(id) {
		return &ParseError{Code: openrtcerr.CodeBadRequest, Message: "Message id must be 1-128 safe ASCII characters"}
	}
	return nil
}

func ValidateRoomName(room string) error {
	if room == "" {
		return &ParseError{Code: openrtcerr.CodeBadRequest, Message: "Room is required"}
	}
	if len(room) > MaxRoomNameBytes || !isSafeIdentifier(room) {
		return &ParseError{Code: openrtcerr.CodeBadRequest, Message: "Room must be 1-256 safe ASCII characters"}
	}
	return nil
}

func ValidateRoomPrefix(prefix string) error {
	if len(prefix) > MaxRoomNameBytes || !isSafeIdentifier(prefix) {
		return &ParseError{Code: openrtcerr.CodeBadRequest, Message: "Room prefix must be at most 256 safe ASCII characters"}
	}
	return nil
}

func ValidateEventName(event string) error {
	if event == "" {
		return &ParseError{Code: openrtcerr.CodeBadRequest, Message: "Event is required"}
	}
	if len(event) > MaxEventNameBytes || !isSafeIdentifier(event) {
		return &ParseError{Code: openrtcerr.CodeBadRequest, Message: "Event must be 1-128 safe ASCII characters"}
	}
	return nil
}

func ValidateConnectionID(connID string) error {
	if connID == "" {
		return &ParseError{Code: openrtcerr.CodeBadRequest, Message: "Connection id is required"}
	}
	if len(connID) > MaxConnectionIDBytes || !isSafeIdentifier(connID) {
		return &ParseError{Code: openrtcerr.CodeBadRequest, Message: "Connection id must be 1-128 safe ASCII characters"}
	}
	return nil
}

func isSafeIdentifier(value string) bool {
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-' || r == '_' || r == '.' || r == ':' || r == '@':
		default:
			return false
		}
	}
	return true
}

func PaginateMembers(members []string, presence map[string]json.RawMessage, limit int, cursor string) ([]string, map[string]json.RawMessage, string) {
	sortedMembers := append([]string(nil), members...)
	sort.Strings(sortedMembers)

	start := 0
	if cursor != "" {
		for index, member := range sortedMembers {
			if member == cursor {
				start = index + 1
				break
			}
		}
	}

	if limit <= 0 || limit > len(sortedMembers) {
		limit = len(sortedMembers)
	}
	end := start + limit
	if end > len(sortedMembers) {
		end = len(sortedMembers)
	}

	page := sortedMembers[start:end]
	pagePresence := make(map[string]json.RawMessage, len(page))
	for _, member := range page {
		if state, ok := presence[member]; ok {
			pagePresence[member] = state
		}
	}

	nextCursor := ""
	if end < len(sortedMembers) {
		nextCursor = sortedMembers[end-1]
	}

	return page, pagePresence, nextCursor
}
