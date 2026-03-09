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
)

type MessageType string

const (
	TypeJoin        MessageType = "JOIN"
	TypeLeave       MessageType = "LEAVE"
	TypeEmit        MessageType = "EMIT"
	TypePresenceSet MessageType = "PRESENCE_SET"
)

type JoinMeta struct {
	Limit  int    `json:"limit,omitempty"`
	Cursor string `json:"cursor,omitempty"`
}

type EmitMeta struct {
	TraceID string `json:"trace_id,omitempty"`
}

type Message struct {
	Type     MessageType
	ID       string
	Room     string
	Event    string
	Payload  json.RawMessage
	JoinMeta *JoinMeta
	EmitMeta *EmitMeta
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

	switch message.Type {
	case TypeJoin, TypeLeave, TypeEmit, TypePresenceSet:
	default:
		return Message{}, &ParseError{Code: openrtcerr.CodeBadRequest, Message: fmt.Sprintf("Unsupported message type: %s", message.Type)}
	}

	if err := readRequiredString(envelope, "room", "Room is required for this message type", &message.Room); err != nil {
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
	if len(meta) > 0 {
		if !json.Valid(meta) {
			return Message{}, &ParseError{Code: openrtcerr.CodeBadRequest, Message: "Meta must be an object when present"}
		}
	}

	switch message.Type {
	case TypeJoin:
		if len(meta) > 0 {
			var joinMeta map[string]json.RawMessage
			if err := json.Unmarshal(meta, &joinMeta); err != nil {
				return Message{}, &ParseError{Code: openrtcerr.CodeBadRequest, Message: "Meta must be an object when present"}
			}
			for key := range joinMeta {
				if key != "limit" && key != "cursor" {
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
			message.JoinMeta = parsed
		}
	case TypeLeave:
	case TypeEmit:
		if err := readRequiredString(envelope, "event", "EMIT requires `event`", &message.Event); err != nil {
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
