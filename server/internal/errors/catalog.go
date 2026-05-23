package errors

import "fmt"

type Code string

const (
	CodeAuthInvalid               Code = "AUTH_INVALID"
	CodeAuthExpired               Code = "AUTH_EXPIRED"
	CodeRoomForbidden             Code = "ROOM_FORBIDDEN"
	CodeRoomNotFound              Code = "ROOM_NOT_FOUND"
	CodeRoomConflict              Code = "ROOM_CONFLICT"
	CodeThreadNotFound            Code = "THREAD_NOT_FOUND"
	CodeThreadConflict            Code = "THREAD_CONFLICT"
	CodeInboxNotificationNotFound Code = "INBOX_NOTIFICATION_NOT_FOUND"
	CodeInboxNotificationConflict Code = "INBOX_NOTIFICATION_CONFLICT"
	CodeStorageNotFound           Code = "STORAGE_NOT_FOUND"
	CodePatchFailed               Code = "PATCH_FAILED"
	CodeRateLimited               Code = "RATE_LIMITED"
	CodePayloadTooLarge           Code = "PAYLOAD_TOO_LARGE"
	CodeQueueOverflow             Code = "QUEUE_OVERFLOW"
	CodeBadRequest                Code = "BAD_REQUEST"
	CodeInternal                  Code = "INTERNAL"
)

type Descriptor struct {
	Code        Code
	HTTPStatus  int
	WSCloseCode int
	Retryable   bool
}

var catalog = map[Code]Descriptor{
	CodeAuthInvalid:               {Code: CodeAuthInvalid, HTTPStatus: 401, WSCloseCode: 4001, Retryable: false},
	CodeAuthExpired:               {Code: CodeAuthExpired, HTTPStatus: 401, WSCloseCode: 4002, Retryable: true},
	CodeRoomForbidden:             {Code: CodeRoomForbidden, HTTPStatus: 403, WSCloseCode: 4403, Retryable: false},
	CodeRoomNotFound:              {Code: CodeRoomNotFound, HTTPStatus: 404, WSCloseCode: 4404, Retryable: false},
	CodeRoomConflict:              {Code: CodeRoomConflict, HTTPStatus: 409, WSCloseCode: 4411, Retryable: false},
	CodeThreadNotFound:            {Code: CodeThreadNotFound, HTTPStatus: 404, WSCloseCode: 4414, Retryable: false},
	CodeThreadConflict:            {Code: CodeThreadConflict, HTTPStatus: 409, WSCloseCode: 4415, Retryable: false},
	CodeInboxNotificationNotFound: {Code: CodeInboxNotificationNotFound, HTTPStatus: 404, WSCloseCode: 4416, Retryable: false},
	CodeInboxNotificationConflict: {Code: CodeInboxNotificationConflict, HTTPStatus: 409, WSCloseCode: 4417, Retryable: false},
	CodeStorageNotFound:           {Code: CodeStorageNotFound, HTTPStatus: 404, WSCloseCode: 4412, Retryable: false},
	CodePatchFailed:               {Code: CodePatchFailed, HTTPStatus: 422, WSCloseCode: 4413, Retryable: false},
	CodeRateLimited:               {Code: CodeRateLimited, HTTPStatus: 429, WSCloseCode: 4408, Retryable: true},
	CodePayloadTooLarge:           {Code: CodePayloadTooLarge, HTTPStatus: 413, WSCloseCode: 4409, Retryable: false},
	CodeQueueOverflow:             {Code: CodeQueueOverflow, HTTPStatus: 503, WSCloseCode: 4410, Retryable: true},
	CodeBadRequest:                {Code: CodeBadRequest, HTTPStatus: 400, WSCloseCode: 4400, Retryable: false},
	CodeInternal:                  {Code: CodeInternal, HTTPStatus: 500, WSCloseCode: 4500, Retryable: true},
}

type APIError struct {
	Code      Code   `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id,omitempty"`
}

func DescriptorFor(code Code) Descriptor {
	return catalog[code]
}

func WSCloseReason(code Code) string {
	descriptor := catalog[code]
	return fmt.Sprintf("%d %s", descriptor.WSCloseCode, descriptor.Code)
}
