package admin

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/openrtc/openrtc/server/internal/auth"
	"github.com/openrtc/openrtc/server/internal/cluster"
	"github.com/openrtc/openrtc/server/internal/config"
	openrtcerr "github.com/openrtc/openrtc/server/internal/errors"
	"github.com/openrtc/openrtc/server/internal/observability"
	"github.com/openrtc/openrtc/server/internal/protocol"
	"github.com/openrtc/openrtc/server/internal/stats"
)

func TestRoomPathAndMetadataValidation(t *testing.T) {
	room, err := roomFromPath("/v1/rooms/tenant-a%3Aroom-1")
	if err != nil {
		t.Fatalf("valid room path rejected: %v", err)
	}
	if room != "tenant-a:room-1" {
		t.Fatalf("unexpected room: %s", room)
	}
	room, subresource, err := roomPathParts("/v1/rooms/tenant-a%3Aroom-1/storage/json-patch")
	if err != nil {
		t.Fatalf("storage path rejected: %v", err)
	}
	if room != "tenant-a:room-1" || subresource != "storage/json-patch" {
		t.Fatalf("unexpected storage path parts: room=%s subresource=%s", room, subresource)
	}
	if _, err := roomFromPath("/v1/rooms/tenant-a%3Aroom-1/storage"); err == nil {
		t.Fatalf("roomFromPath should reject storage subresources")
	}
	threadID, child, err := threadPathParts("threads/thread-1/comments")
	if err != nil {
		t.Fatalf("thread path rejected: %v", err)
	}
	if threadID != "thread-1" || child != "comments" {
		t.Fatalf("unexpected thread path parts: thread=%s child=%s", threadID, child)
	}
	if _, _, err := threadPathParts("threads/bad%20thread/comments"); err == nil {
		t.Fatalf("expected unsafe thread id to fail")
	}
	if _, _, err := threadPathParts("threads/%zz/comments"); err == nil {
		t.Fatalf("expected malformed thread escape to fail")
	}
	if _, _, err := threadPathParts("threads"); err == nil {
		t.Fatalf("expected incomplete thread path to fail")
	}
	commentID, err := commentPathPart("comments/comment-1")
	if err != nil {
		t.Fatalf("comment path rejected: %v", err)
	}
	if commentID != "comment-1" {
		t.Fatalf("unexpected comment id: %s", commentID)
	}
	if _, err := commentPathPart("comments/bad%20comment"); err == nil {
		t.Fatalf("expected unsafe comment id to fail")
	}
	if _, err := commentPathPart("comments/%zz"); err == nil {
		t.Fatalf("expected malformed comment escape to fail")
	}
	if _, err := commentPathPart("comments/comment-1/reactions"); err == nil {
		t.Fatalf("expected nested comment path to fail")
	}
	userID, userChild, err := roomUserPathParts("users/user-1/subscription-settings")
	if err != nil {
		t.Fatalf("room user path rejected: %v", err)
	}
	if userID != "user-1" || userChild != "subscription-settings" {
		t.Fatalf("unexpected room user path parts: user=%s child=%s", userID, userChild)
	}
	userID, userSubresource, itemID, err := userPathParts("/v1/users/user-1/inbox-notifications/in_1")
	if err != nil {
		t.Fatalf("user path rejected: %v", err)
	}
	if userID != "user-1" || userSubresource != "inbox-notifications" || itemID != "in_1" {
		t.Fatalf("unexpected user path parts: user=%s subresource=%s item=%s", userID, userSubresource, itemID)
	}
	notificationID, action, err := inboxNotificationActionParts("/v1/inbox-notifications/in_1/read")
	if err != nil {
		t.Fatalf("notification action path rejected: %v", err)
	}
	if notificationID != "in_1" || action != "read" {
		t.Fatalf("unexpected notification action parts: id=%s action=%s", notificationID, action)
	}
	if _, _, err := roomUserPathParts("users/bad%20user/subscription-settings"); err == nil {
		t.Fatalf("expected unsafe room user id to fail")
	}
	if _, _, err := roomUserPathParts("users"); err == nil {
		t.Fatalf("expected incomplete room user path to fail")
	}
	if _, _, err := roomUserPathParts("users/%zz/subscription-settings"); err == nil {
		t.Fatalf("expected malformed room user escape to fail")
	}
	if _, _, _, err := userPathParts("/v1/users/user-1"); err == nil {
		t.Fatalf("expected missing user subresource to fail")
	}
	if _, _, _, err := userPathParts("/v1/not-users/user-1/inbox-notifications"); err == nil {
		t.Fatalf("expected missing users prefix to fail")
	}
	if _, _, _, err := userPathParts("/v1/users/%zz/inbox-notifications"); err == nil {
		t.Fatalf("expected malformed user escape to fail")
	}
	if _, _, _, err := userPathParts("/v1/users/user-1/inbox-notifications/bad%20id"); err == nil {
		t.Fatalf("expected unsafe user item id to fail")
	}
	if _, _, _, err := userPathParts("/v1/users/user-1/inbox-notifications/%zz"); err == nil {
		t.Fatalf("expected malformed item escape to fail")
	}
	if _, _, err := inboxNotificationActionParts("/v1/inbox-notifications/bad%20id/read"); err == nil {
		t.Fatalf("expected unsafe notification id to fail")
	}
	if _, _, err := inboxNotificationActionParts("/v1/inbox-notifications/%zz/read"); err == nil {
		t.Fatalf("expected malformed notification escape to fail")
	}
	if _, _, err := inboxNotificationActionParts("/v1/inbox-notifications"); err == nil {
		t.Fatalf("expected incomplete notification action path to fail")
	}

	invalidPaths := []string{
		"/v1/rooms/",
		"/v1/rooms/tenant-a:room-1/extra",
		"/v1/rooms/%zz",
		"/v1/rooms/tenant-a%2Froom-1",
	}
	for _, path := range invalidPaths {
		if _, err := roomFromPath(path); err == nil {
			t.Fatalf("expected invalid room path %q to fail", path)
		}
	}

	if err := validateMetadata(nil, true, 16); err != nil {
		t.Fatalf("optional metadata should pass: %v", err)
	}
	if err := validateMetadata(nil, false, 16); err == nil || err.Code != openrtcerr.CodeBadRequest {
		t.Fatalf("required metadata should fail with BAD_REQUEST, got %v", err)
	}
	if err := validateMetadata(json.RawMessage(`[]`), false, 16); err == nil || err.Code != openrtcerr.CodeBadRequest {
		t.Fatalf("array metadata should fail with BAD_REQUEST, got %v", err)
	}
	if err := validateMetadata(json.RawMessage(`{"long":"0123456789"}`), false, 4); err == nil || err.Code != openrtcerr.CodePayloadTooLarge {
		t.Fatalf("oversized metadata should fail with PAYLOAD_TOO_LARGE, got %v", err)
	}
	if string(normalizedMetadata(nil)) != "{}" {
		t.Fatalf("expected nil metadata to normalize to object")
	}
	if err := validateRoomRequest("tenant-a:room-1", json.RawMessage(`{"ok":true}`), false, 16); err != nil {
		t.Fatalf("valid room request should pass: %v", err)
	}
	if err := validateRoomRequest("tenant-a:bad room", json.RawMessage(`{"ok":true}`), false, 16); err == nil || err.Code != openrtcerr.CodeBadRequest {
		t.Fatalf("invalid room request should fail with BAD_REQUEST, got %v", err)
	}
	if id := newRecordID("th"); !strings.HasPrefix(id, "th_") || len(id) != 27 {
		t.Fatalf("unexpected generated record id: %s", id)
	}
}

func TestNewRecordIDPanicsWhenRandomReadFails(t *testing.T) {
	oldRandomRead := randomRead
	randomRead = func([]byte) (int, error) {
		return 0, errors.New("random failed")
	}
	defer func() {
		randomRead = oldRandomRead
		if recovered := recover(); recovered == nil {
			t.Fatalf("expected newRecordID to panic")
		}
	}()

	_ = newRecordID("th")
}

func TestAuthorizedRoomListPrefixAndPaginationParsing(t *testing.T) {
	cfg := config.RuntimeConfig{}
	cfg.Tenant.EnforcePrefix = true
	cfg.Tenant.Separator = ":"
	service := &Service{cfg: cfg}
	claims := &auth.Claims{Tenant: "tenant-a", Scope: "rooms:tenant-a:*"}

	prefix, err := service.authorizedRoomListPrefix(claims, "")
	if err != nil {
		t.Fatalf("default tenant prefix rejected: %v", err)
	}
	if prefix != "tenant-a:" {
		t.Fatalf("unexpected default prefix: %s", prefix)
	}
	if _, err := service.authorizedRoomListPrefix(claims, "tenant-a:doc-"); err != nil {
		t.Fatalf("tenant doc prefix rejected: %v", err)
	}
	if _, err := service.authorizedRoomListPrefix(&auth.Claims{Scope: "rooms:tenant-a:*"}, ""); err == nil || err.Code != openrtcerr.CodeRoomForbidden {
		t.Fatalf("missing tenant should be forbidden, got %v", err)
	}
	if _, err := service.authorizedRoomListPrefix(claims, "tenant-a:bad prefix"); err == nil || err.Code != openrtcerr.CodeBadRequest {
		t.Fatalf("unsafe prefix should be bad request, got %v", err)
	}
	if _, err := service.authorizedRoomListPrefix(claims, "tenant-b:"); err == nil || err.Code != openrtcerr.CodeRoomForbidden {
		t.Fatalf("cross-tenant prefix should be forbidden, got %v", err)
	}
	longPrefix := strings.Repeat("a", protocol.MaxRoomNameBytes)
	if _, err := service.authorizedRoomListPrefix(&auth.Claims{Scope: "rooms:*"}, longPrefix); err == nil || err.Code != openrtcerr.CodeBadRequest {
		t.Fatalf("oversized prefix should be bad request, got %v", err)
	}

	if limit, err := parseListLimit(""); err != nil || limit != 50 {
		t.Fatalf("unexpected default limit: %d %v", limit, err)
	}
	if limit, err := parseListLimit("200"); err != nil || limit != 200 {
		t.Fatalf("unexpected explicit limit: %d %v", limit, err)
	}
	if _, err := parseListLimit("0"); err == nil {
		t.Fatalf("expected zero limit to fail")
	}
	if _, err := parseListLimit("201"); err == nil {
		t.Fatalf("expected limit above max to fail")
	}
	if _, err := parseListLimit("not-a-number"); err == nil {
		t.Fatalf("expected non-numeric limit to fail")
	}
	if cursor, err := parseCursor(""); err != nil || cursor != 0 {
		t.Fatalf("unexpected default cursor: %d %v", cursor, err)
	}
	if cursor, err := parseCursor("42"); err != nil || cursor != 42 {
		t.Fatalf("unexpected explicit cursor: %d %v", cursor, err)
	}
	if _, err := parseCursor("-1"); err == nil {
		t.Fatalf("expected negative cursor to fail")
	}
	if _, err := parseCursor("not-a-number"); err == nil {
		t.Fatalf("expected non-numeric cursor to fail")
	}

	query, parseErr := parseRoomListQuery(`id:room metadata.status:active metadata.priority:2 metadata.public:true metadata.owner:* metadata["kind"]:"white board"`)
	if parseErr != nil {
		t.Fatalf("expected room query to parse: %v", parseErr)
	}
	matchingRoom := cluster.RoomRecord{
		ID:       "tenant-a:room-1",
		Metadata: json.RawMessage(`{"status":"active","priority":2,"public":true,"owner":"user-1","kind":"white board"}`),
	}
	if !query.Matches(matchingRoom) {
		t.Fatalf("expected query to match room")
	}
	if query.Matches(cluster.RoomRecord{ID: "tenant-a:room-2", Metadata: json.RawMessage(`{"status":"active","priority":2,"public":false,"owner":"user-1","kind":"white board"}`)}) {
		t.Fatalf("expected bool mismatch to fail query")
	}
	if _, parseErr := parseRoomListQuery("metadata.status"); parseErr == nil || parseErr.Code != openrtcerr.CodeBadRequest {
		t.Fatalf("expected malformed query to fail, got %v", parseErr)
	}
	if _, parseErr := parseRoomListQuery("metadata.bad/key:value"); parseErr == nil || parseErr.Code != openrtcerr.CodeBadRequest {
		t.Fatalf("expected unsafe metadata path to fail, got %v", parseErr)
	}
}

func TestStorageDocumentParsing(t *testing.T) {
	t.Run("valid object is compacted", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodPut, "/v1/rooms/tenant-a%3Aroom-1/storage", strings.NewReader(" { \n \"ok\" : true } "))
		document, err := readStorageDocument(httptest.NewRecorder(), request, 64)
		if err != nil {
			t.Fatalf("read storage document: %v", err)
		}
		if string(document) != `{"ok":true}` {
			t.Fatalf("unexpected compacted document: %s", document)
		}
	})

	t.Run("valid typed storage is compacted", func(t *testing.T) {
		body := `{
			"liveblocksType":"LiveObject",
			"data":{
				"items":{"liveblocksType":"LiveList","data":["a"]},
				"props":{"liveblocksType":"LiveMap","data":{"visible":true}}
			}
		}`
		request := httptest.NewRequest(http.MethodPut, "/v1/rooms/tenant-a%3Aroom-1/storage", strings.NewReader(body))
		document, err := readStorageDocument(httptest.NewRecorder(), request, 256)
		if err != nil {
			t.Fatalf("read typed storage document: %v", err)
		}
		if string(document) != `{"liveblocksType":"LiveObject","data":{"items":{"liveblocksType":"LiveList","data":["a"]},"props":{"liveblocksType":"LiveMap","data":{"visible":true}}}}` {
			t.Fatalf("unexpected compacted typed document: %s", document)
		}
	})

	t.Run("array root is rejected", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodPut, "/v1/rooms/tenant-a%3Aroom-1/storage", strings.NewReader(`[]`))
		_, err := readStorageDocument(httptest.NewRecorder(), request, 64)
		if err == nil || err.Code != openrtcerr.CodeBadRequest {
			t.Fatalf("expected BAD_REQUEST for array root, got %v", err)
		}
	})

	t.Run("invalid typed storage is rejected", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodPut, "/v1/rooms/tenant-a%3Aroom-1/storage", strings.NewReader(`{"liveblocksType":"LiveList","data":[]}`))
		_, err := readStorageDocument(httptest.NewRecorder(), request, 128)
		if err == nil || err.Code != openrtcerr.CodeBadRequest {
			t.Fatalf("expected BAD_REQUEST for invalid typed storage, got %v", err)
		}
	})

	t.Run("invalid object JSON is rejected", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodPut, "/v1/rooms/tenant-a%3Aroom-1/storage", strings.NewReader(`{"ok":`))
		_, err := readStorageDocument(httptest.NewRecorder(), request, 64)
		if err == nil || err.Code != openrtcerr.CodeBadRequest {
			t.Fatalf("expected BAD_REQUEST for invalid JSON, got %v", err)
		}
	})

	t.Run("oversized body is rejected", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodPut, "/v1/rooms/tenant-a%3Aroom-1/storage", strings.NewReader(`{"ok":true}`))
		_, err := readStorageDocument(httptest.NewRecorder(), request, 4)
		if err == nil || err.Code != openrtcerr.CodePayloadTooLarge {
			t.Fatalf("expected PAYLOAD_TOO_LARGE for oversized document, got %v", err)
		}
	})
}

func TestNotificationSettingsParsing(t *testing.T) {
	valid := httptest.NewRequest(http.MethodPost, "/v1/users/user-1/notification-settings", strings.NewReader(`{"email":{"thread":true}}`))
	settings, err := readNotificationSettings(httptest.NewRecorder(), valid, 64)
	if err != nil {
		t.Fatalf("expected notification settings to parse: %v", err)
	}
	if string(settings) != `{"email":{"thread":true}}` {
		t.Fatalf("unexpected settings: %s", settings)
	}
	invalidJSON := httptest.NewRequest(http.MethodPost, "/v1/users/user-1/notification-settings", strings.NewReader(`{`))
	if _, err := readNotificationSettings(httptest.NewRecorder(), invalidJSON, 64); err == nil || err.Code != openrtcerr.CodeBadRequest {
		t.Fatalf("expected invalid JSON to fail, got %v", err)
	}
	array := httptest.NewRequest(http.MethodPost, "/v1/users/user-1/notification-settings", strings.NewReader(`[]`))
	if _, err := readNotificationSettings(httptest.NewRecorder(), array, 64); err == nil || err.Code != openrtcerr.CodeBadRequest {
		t.Fatalf("expected array settings to fail, got %v", err)
	}
	tooLarge := httptest.NewRequest(http.MethodPost, "/v1/users/user-1/notification-settings", strings.NewReader(`{"long":"0123456789"}`))
	if _, err := readNotificationSettings(httptest.NewRecorder(), tooLarge, 4); err == nil || err.Code != openrtcerr.CodePayloadTooLarge {
		t.Fatalf("expected oversized settings to fail, got %v", err)
	}
}

func TestStoragePatchParsing(t *testing.T) {
	validRequest := httptest.NewRequest(http.MethodPatch, "/v1/rooms/tenant-a%3Aroom-1/storage/json-patch", strings.NewReader(`[{"op":"add","path":"/title","value":"Draft"}]`))
	operations, err := decodeStoragePatch(httptest.NewRecorder(), validRequest, 256)
	if err != nil {
		t.Fatalf("decode valid patch: %v", err)
	}
	if len(operations) != 1 || operations[0].Op != "add" || operations[0].Path != "/title" {
		t.Fatalf("unexpected patch operations: %+v", operations)
	}

	tests := []struct {
		name string
		body string
		want openrtcerr.Code
	}{
		{
			name: "malformed json",
			body: `[`,
			want: openrtcerr.CodeBadRequest,
		},
		{
			name: "empty operations",
			body: `[]`,
			want: openrtcerr.CodeBadRequest,
		},
		{
			name: "unknown field",
			body: `[{"op":"add","path":"/ok","value":true,"extra":true}]`,
			want: openrtcerr.CodeBadRequest,
		},
		{
			name: "missing op",
			body: `[{"path":"/ok","value":true}]`,
			want: openrtcerr.CodeBadRequest,
		},
		{
			name: "unsupported op",
			body: `[{"op":"increment","path":"/ok","value":1}]`,
			want: openrtcerr.CodePatchFailed,
		},
		{
			name: "remove root",
			body: `[{"op":"remove","path":""}]`,
			want: openrtcerr.CodePatchFailed,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPatch, "/v1/rooms/tenant-a%3Aroom-1/storage/json-patch", strings.NewReader(tc.body))
			_, err := decodeStoragePatch(httptest.NewRecorder(), request, 256)
			if err == nil || err.Code != tc.want {
				t.Fatalf("expected %s, got %v", tc.want, err)
			}
		})
	}

	var tooMany bytes.Buffer
	tooMany.WriteByte('[')
	for i := 0; i < maxStoragePatchOperations+1; i++ {
		if i > 0 {
			tooMany.WriteByte(',')
		}
		tooMany.WriteString(`{"op":"test","path":"","value":{}}`)
	}
	tooMany.WriteByte(']')
	request := httptest.NewRequest(http.MethodPatch, "/v1/rooms/tenant-a%3Aroom-1/storage/json-patch", &tooMany)
	if _, err := decodeStoragePatch(httptest.NewRecorder(), request, tooMany.Len()+1); err == nil || err.Code != openrtcerr.CodeBadRequest {
		t.Fatalf("expected BAD_REQUEST for too many operations, got %v", err)
	}

	request = httptest.NewRequest(http.MethodPatch, "/v1/rooms/tenant-a%3Aroom-1/storage/json-patch", strings.NewReader(`[{"op":"test","path":"","value":{}}]`))
	if _, err := decodeStoragePatch(httptest.NewRecorder(), request, 4); err == nil || err.Code != openrtcerr.CodeBadRequest {
		t.Fatalf("expected BAD_REQUEST for oversized patch body, got %v", err)
	}
}

func TestStoragePatchOperationJSONShape(t *testing.T) {
	raw, err := json.Marshal(cluster.JSONPatchOperation{
		Op:    "replace",
		Path:  "/title",
		From:  "/draftTitle",
		Value: json.RawMessage(`"Published"`),
	})
	if err != nil {
		t.Fatalf("marshal patch operation: %v", err)
	}
	if string(raw) != `{"op":"replace","path":"/title","from":"/draftTitle","value":"Published"}` {
		t.Fatalf("unexpected operation JSON shape: %s", raw)
	}
}

func TestRoomAccessValidation(t *testing.T) {
	if err := validateRoomAccesses([]string{cluster.PermissionRoomRead}, nil, nil); err != nil {
		t.Fatalf("expected valid default access to pass: %v", err)
	}
	if err := validateRoomAccesses(nil, nil, map[string][]string{"team-1": {cluster.PermissionCommentsWrite}}); err != nil {
		t.Fatalf("expected valid group comments access to pass: %v", err)
	}
	if err := validateRoomAccesses(
		[]string{cluster.PermissionStorageRead, cluster.PermissionCommentsRead, cluster.PermissionFeedsRead},
		map[string][]string{"user-1": {cluster.PermissionStorageWrite, cluster.PermissionFeedsWrite}},
		map[string][]string{"team-1": {cluster.PermissionCommentsWrite}},
	); err != nil {
		t.Fatalf("expected normalized read/write access permissions to pass: %v", err)
	}
	if err := validateRoomAccesses([]string{"room:delete"}, nil, nil); err == nil || err.Code != openrtcerr.CodeBadRequest {
		t.Fatalf("expected unsupported access to fail with BAD_REQUEST, got %v", err)
	}
	if err := validateRoomAccesses(nil, map[string][]string{"": {cluster.PermissionRoomRead}}, nil); err == nil || err.Code != openrtcerr.CodeBadRequest {
		t.Fatalf("expected empty user access id to fail with BAD_REQUEST, got %v", err)
	}
	if err := validateRoomAccesses(nil, map[string][]string{strings.Repeat("u", protocol.MaxRoomNameBytes+1): {cluster.PermissionRoomRead}}, nil); err == nil || err.Code != openrtcerr.CodeBadRequest {
		t.Fatalf("expected oversized user access id to fail with BAD_REQUEST, got %v", err)
	}
	if err := validateRoomAccesses(nil, nil, map[string][]string{"team-1": {"room:delete"}}); err == nil || err.Code != openrtcerr.CodeBadRequest {
		t.Fatalf("expected invalid group access permission to fail with BAD_REQUEST, got %v", err)
	}

	tooMany := make(map[string][]string, 1001)
	for i := 0; i < 1001; i++ {
		tooMany[fmt.Sprintf("user-%d", i)] = []string{cluster.PermissionRoomRead}
	}
	if err := validateRoomAccesses(nil, tooMany, nil); err == nil || err.Code != openrtcerr.CodeBadRequest {
		t.Fatalf("expected oversized user access map to fail with BAD_REQUEST, got %v", err)
	}
}

func TestThreadValidation(t *testing.T) {
	validComment := CommentCreateRequest{
		ID:     "comment-1",
		UserID: "user-1",
		Body:   json.RawMessage(`{"content":[]}`),
	}
	if err := validateThreadRequest("thread-1", json.RawMessage(`{"anchor":"shape-1"}`), validComment, 128); err != nil {
		t.Fatalf("expected valid thread request: %v", err)
	}
	if err := validateThreadRequest("bad thread", nil, validComment, 128); err == nil || err.Code != openrtcerr.CodeBadRequest {
		t.Fatalf("expected invalid thread id to fail, got %v", err)
	}
	if err := validateThreadRequest("thread-1", json.RawMessage(`[]`), validComment, 128); err == nil || err.Code != openrtcerr.CodeBadRequest {
		t.Fatalf("expected invalid thread metadata to fail, got %v", err)
	}

	tests := []struct {
		name    string
		comment CommentCreateRequest
		want    openrtcerr.Code
	}{
		{
			name:    "bad comment id",
			comment: CommentCreateRequest{ID: "bad comment", UserID: "user-1", Body: json.RawMessage(`{}`)},
			want:    openrtcerr.CodeBadRequest,
		},
		{
			name:    "bad user id",
			comment: CommentCreateRequest{ID: "comment-1", UserID: "bad user", Body: json.RawMessage(`{}`)},
			want:    openrtcerr.CodeBadRequest,
		},
		{
			name:    "missing body",
			comment: CommentCreateRequest{ID: "comment-1", UserID: "user-1"},
			want:    openrtcerr.CodeBadRequest,
		},
		{
			name:    "array body",
			comment: CommentCreateRequest{ID: "comment-1", UserID: "user-1", Body: json.RawMessage(`[]`)},
			want:    openrtcerr.CodeBadRequest,
		},
		{
			name:    "oversized body",
			comment: CommentCreateRequest{ID: "comment-1", UserID: "user-1", Body: json.RawMessage(`{"body":"0123456789"}`)},
			want:    openrtcerr.CodePayloadTooLarge,
		},
		{
			name:    "invalid metadata",
			comment: CommentCreateRequest{ID: "comment-1", UserID: "user-1", Body: json.RawMessage(`{}`), Metadata: json.RawMessage(`[]`)},
			want:    openrtcerr.CodeBadRequest,
		},
		{
			name:    "invalid mention",
			comment: CommentCreateRequest{ID: "comment-1", UserID: "user-1", Body: json.RawMessage(`{}`), Mentions: []string{"bad user"}},
			want:    openrtcerr.CodeBadRequest,
		},
		{
			name:    "invalid reaction emoji",
			comment: CommentCreateRequest{ID: "comment-1", UserID: "user-1", Body: json.RawMessage(`{}`), Reactions: []cluster.CommentReaction{{Emoji: "", UserID: "user-1"}}},
			want:    openrtcerr.CodeBadRequest,
		},
		{
			name:    "invalid reaction user",
			comment: CommentCreateRequest{ID: "comment-1", UserID: "user-1", Body: json.RawMessage(`{}`), Reactions: []cluster.CommentReaction{{Emoji: "+1", UserID: "bad user"}}},
			want:    openrtcerr.CodeBadRequest,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateCommentRequest(tc.comment, 8); err == nil || err.Code != tc.want {
				t.Fatalf("expected %s, got %v", tc.want, err)
			}
		})
	}

	validUpdateBody := json.RawMessage(`{"content":[]}`)
	validUpdateMetadata := json.RawMessage(`{"status":"open"}`)
	validUpdateMentions := []string{"user-1"}
	validUpdateReactions := []cluster.CommentReaction{{Emoji: "+1", UserID: "user-1"}}
	if err := validateCommentUpdateRequest(CommentUpdateRequest{
		Body:      &validUpdateBody,
		Metadata:  &validUpdateMetadata,
		Mentions:  &validUpdateMentions,
		Reactions: &validUpdateReactions,
	}, 128); err != nil {
		t.Fatalf("expected valid comment update: %v", err)
	}
	for _, tc := range []struct {
		name    string
		request CommentUpdateRequest
		want    openrtcerr.Code
	}{
		{name: "empty update", request: CommentUpdateRequest{}, want: openrtcerr.CodeBadRequest},
		{name: "array body", request: CommentUpdateRequest{Body: rawMessagePtr(json.RawMessage(`[]`))}, want: openrtcerr.CodeBadRequest},
		{name: "invalid metadata", request: CommentUpdateRequest{Metadata: rawMessagePtr(json.RawMessage(`[]`))}, want: openrtcerr.CodeBadRequest},
		{name: "invalid mention", request: CommentUpdateRequest{Mentions: stringSlicePtr([]string{"bad user"})}, want: openrtcerr.CodeBadRequest},
		{name: "invalid reaction", request: CommentUpdateRequest{Reactions: reactionSlicePtr([]cluster.CommentReaction{{Emoji: "+1", UserID: "bad user"}})}, want: openrtcerr.CodeBadRequest},
	} {
		t.Run("update "+tc.name, func(t *testing.T) {
			if err := validateCommentUpdateRequest(tc.request, 8); err == nil || err.Code != tc.want {
				t.Fatalf("expected %s, got %v", tc.want, err)
			}
		})
	}
}

func rawMessagePtr(value json.RawMessage) *json.RawMessage {
	return &value
}

func stringSlicePtr(value []string) *[]string {
	return &value
}

func reactionSlicePtr(value []cluster.CommentReaction) *[]cluster.CommentReaction {
	return &value
}

func TestNotificationValidation(t *testing.T) {
	request := InboxNotificationTriggerRequest{
		ID:           "in_1",
		UserID:       "user-1",
		Kind:         "$custom",
		SubjectID:    "subject-1",
		ThreadID:     "thread-1",
		RoomID:       "tenant-a:room-1",
		ActivityData: json.RawMessage(`{"count":1,"flag":true,"label":"ok"}`),
	}
	if err := validateInboxNotificationRequest(request, 128); err != nil {
		t.Fatalf("expected valid notification: %v", err)
	}
	request.Kind = "$CUSTOM_KIND"
	if err := validateInboxNotificationRequest(request, 128); err != nil {
		t.Fatalf("expected uppercase custom notification kind: %v", err)
	}
	request.Kind = "$bad-kind"
	if err := validateInboxNotificationRequest(request, 128); err == nil || err.Code != openrtcerr.CodeBadRequest {
		t.Fatalf("expected invalid custom notification kind, got %v", err)
	}
	request.Kind = "$"
	if err := validateInboxNotificationRequest(request, 128); err == nil || err.Code != openrtcerr.CodeBadRequest {
		t.Fatalf("expected empty custom notification kind, got %v", err)
	}
	request.Kind = "bad kind"
	if err := validateInboxNotificationRequest(request, 128); err == nil || err.Code != openrtcerr.CodeBadRequest {
		t.Fatalf("expected unsafe notification kind, got %v", err)
	}
	request.Kind = "thread"
	request.ActivityData = json.RawMessage(`{"nested":{}}`)
	if err := validateInboxNotificationRequest(request, 128); err == nil || err.Code != openrtcerr.CodeBadRequest {
		t.Fatalf("expected nested activityData to fail, got %v", err)
	}
	request.ActivityData = json.RawMessage(`[]`)
	if err := validateInboxNotificationRequest(request, 128); err == nil || err.Code != openrtcerr.CodeBadRequest {
		t.Fatalf("expected array activityData to fail, got %v", err)
	}
	request.ActivityData = nil
	request.RoomID = "bad room"
	if err := validateInboxNotificationRequest(request, 128); err == nil || err.Code != openrtcerr.CodeBadRequest {
		t.Fatalf("expected invalid notification room to fail, got %v", err)
	}
	request.RoomID = "tenant-a:room-1"
	request.ThreadID = "bad thread"
	if err := validateInboxNotificationRequest(request, 128); err == nil || err.Code != openrtcerr.CodeBadRequest {
		t.Fatalf("expected invalid notification thread to fail, got %v", err)
	}
	request.ThreadID = "thread-1"
	request.SubjectID = "bad subject"
	if err := validateInboxNotificationRequest(request, 128); err == nil || err.Code != openrtcerr.CodeBadRequest {
		t.Fatalf("expected invalid notification subject to fail, got %v", err)
	}
	request.SubjectID = "subject-1"
	request.UserID = "bad user"
	if err := validateInboxNotificationRequest(request, 128); err == nil || err.Code != openrtcerr.CodeBadRequest {
		t.Fatalf("expected invalid notification user to fail, got %v", err)
	}
	request.UserID = "user-1"
	request.ID = "bad id"
	if err := validateInboxNotificationRequest(request, 128); err == nil || err.Code != openrtcerr.CodeBadRequest {
		t.Fatalf("expected invalid notification id to fail, got %v", err)
	}
	if err := validateRoomSubscriptionSettingsRequest(RoomSubscriptionSettingsRequest{Threads: "all", TextMentions: "mine"}); err != nil {
		t.Fatalf("expected valid subscription settings: %v", err)
	}
	if err := validateRoomSubscriptionSettingsRequest(RoomSubscriptionSettingsRequest{Threads: "mentions"}); err == nil || err.Code != openrtcerr.CodeBadRequest {
		t.Fatalf("expected invalid thread subscription setting, got %v", err)
	}
	if err := validateRoomSubscriptionSettingsRequest(RoomSubscriptionSettingsRequest{TextMentions: "all"}); err == nil || err.Code != openrtcerr.CodeBadRequest {
		t.Fatalf("expected invalid text mention setting, got %v", err)
	}
	if err := validateNotificationKind(strings.Repeat("a", protocol.MaxEventNameBytes+1)); err == nil || err.Code != openrtcerr.CodeBadRequest {
		t.Fatalf("expected oversized notification kind, got %v", err)
	}
	if err := validateActivityData(nil, false, 128); err == nil || err.Code != openrtcerr.CodeBadRequest {
		t.Fatalf("expected required activityData to fail, got %v", err)
	}
	if err := validateActivityData(json.RawMessage(`{"long":"0123456789"}`), true, 4); err == nil || err.Code != openrtcerr.CodePayloadTooLarge {
		t.Fatalf("expected oversized activityData to fail, got %v", err)
	}
	if err := validateActivityData(json.RawMessage(`{"bad":`), true, 128); err == nil || err.Code != openrtcerr.CodeBadRequest {
		t.Fatalf("expected invalid activityData JSON to fail, got %v", err)
	}
	if err := validateActivityData(json.RawMessage(`[]`), true, 128); err == nil || err.Code != openrtcerr.CodeBadRequest {
		t.Fatalf("expected array activityData to fail, got %v", err)
	}
	if err := validateActivityData(json.RawMessage(`null`), true, 128); err == nil || err.Code != openrtcerr.CodeBadRequest {
		t.Fatalf("expected null activityData to fail, got %v", err)
	}
	if !unreadOnlyQuery("unread:true", "") || !unreadOnlyQuery("", "true") || unreadOnlyQuery("unread:false", "") {
		t.Fatalf("unexpected unread query parsing")
	}
	if value := firstNonEmpty("", "next"); value != "next" {
		t.Fatalf("unexpected first non-empty value: %s", value)
	}
	if _, err := parseNotificationListLimit(""); err != nil {
		t.Fatalf("default notification list limit should pass: %v", err)
	}
	if _, err := parseNotificationListLimit("0"); err == nil {
		t.Fatalf("expected invalid notification list limit")
	}
}

func TestAdminAuthenticateErrors(t *testing.T) {
	service := &Service{}
	if _, err := service.authenticate(httptest.NewRequest(http.MethodGet, "/v1/stats", nil)); err == nil {
		t.Fatalf("expected missing verifier to fail authentication")
	}

	verifier, _, cleanup := newAdminTestVerifier(t, map[string]any{"scope": "rooms:*"})
	defer cleanup()
	service.verifier = verifier
	if _, err := service.authenticate(httptest.NewRequest(http.MethodGet, "/v1/stats", nil)); err == nil {
		t.Fatalf("expected missing bearer token to fail authentication")
	}
}

func TestNewAdminServiceConfigBranches(t *testing.T) {
	cfg := config.RuntimeConfig{}
	service, err := NewService(cfg, nil)
	if err != nil {
		t.Fatalf("new service without admin auth/redis: %v", err)
	}
	if service.verifier != nil || service.store != nil {
		t.Fatalf("expected no verifier/store without admin auth/redis")
	}
	if err := service.Close(); err != nil {
		t.Fatalf("close service without store: %v", err)
	}

	cfg.AdminAuth = &struct {
		Issuer   string
		Audience string
		JWKSURL  string
	}{
		Issuer:   "https://issuer.example.com",
		Audience: "openrtc-admin",
		JWKSURL:  "https://issuer.example.com/jwks.json",
	}
	cfg.Redis = &struct {
		URL           string
		ChannelPrefix string
	}{
		URL:           "redis://localhost:6379/0",
		ChannelPrefix: "room:",
	}
	service, err = NewService(cfg, nil)
	if err != nil {
		t.Fatalf("new service with admin auth/redis: %v", err)
	}
	if service.verifier == nil || service.store == nil {
		t.Fatalf("expected verifier and store with admin auth/redis")
	}
	if err := service.Close(); err != nil {
		t.Fatalf("close redis-backed service: %v", err)
	}

	cfg.Redis.URL = "redis://%"
	if _, err := NewService(cfg, nil); err == nil {
		t.Fatalf("expected invalid redis URL to fail admin service construction")
	}
}

func TestAdminHealthStatsPublishAndPresenceHandlers(t *testing.T) {
	verifier, token, cleanup := newAdminTestVerifier(t, map[string]any{
		"tenant": "tenant-a",
		"scope":  "publish:tenant-a:* presence:tenant-a:*",
	})
	defer cleanup()

	store := &fakeAdminStore{
		stats: stats.Snapshot{
			ActiveConnections: 2,
			ActiveRooms:       1,
		},
	}
	service := newTestAdminService(verifier, store)
	handler := service.Handler()

	healthResp := httptest.NewRecorder()
	handler.ServeHTTP(healthResp, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if healthResp.Code != http.StatusOK || strings.TrimSpace(healthResp.Body.String()) != "ok" {
		t.Fatalf("unexpected health response: %d %q", healthResp.Code, healthResp.Body.String())
	}

	readyResp := httptest.NewRecorder()
	handler.ServeHTTP(readyResp, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if readyResp.Code != http.StatusOK || strings.TrimSpace(readyResp.Body.String()) != "ready" {
		t.Fatalf("unexpected ready response: %d %q", readyResp.Code, readyResp.Body.String())
	}

	statsReq := httptest.NewRequest(http.MethodGet, "/v1/stats", nil)
	statsReq.Header.Set("Authorization", "Bearer "+token)
	statsResp := httptest.NewRecorder()
	handler.ServeHTTP(statsResp, statsReq)
	if statsResp.Code != http.StatusOK {
		t.Fatalf("expected stats 200, got %d", statsResp.Code)
	}
	var snapshot stats.Snapshot
	if err := json.NewDecoder(statsResp.Body).Decode(&snapshot); err != nil {
		t.Fatalf("decode stats: %v", err)
	}
	if snapshot.ActiveConnections != 2 || snapshot.ActiveRooms != 1 {
		t.Fatalf("unexpected stats snapshot: %+v", snapshot)
	}

	publishReq := httptest.NewRequest(http.MethodPost, "/v1/publish", strings.NewReader(`{"room":"tenant-a:room-1","event":"doc.update","payload":{"ok":true},"exclude_sender_conn_id":"conn-1","trace_id":"trace-1"}`))
	publishReq.Header.Set("Authorization", "Bearer "+token)
	publishResp := httptest.NewRecorder()
	handler.ServeHTTP(publishResp, publishReq)
	if publishResp.Code != http.StatusAccepted {
		t.Fatalf("expected publish 202, got %d", publishResp.Code)
	}
	if len(store.publishedEvents) != 1 || store.publishedEvents[0].OriginNode != "admin:node-a" || store.publishedEvents[0].TraceID != "trace-1" {
		t.Fatalf("unexpected published event: %+v", store.publishedEvents)
	}
	if len(store.syncedStats) != 1 || store.syncedStats[0].AdminPublishesTotal != 1 {
		t.Fatalf("expected publish stats sync, got %+v", store.syncedStats)
	}
	if service.metrics.AdminPublishesTotal.Load() != 1 {
		t.Fatalf("expected admin publish metric to increment")
	}

	presenceReq := httptest.NewRequest(http.MethodPost, "/v1/presence", strings.NewReader(`{"room":"tenant-a:room-1","conn_id":"agent-1","state":{"status":"thinking"},"ttl_seconds":2}`))
	presenceReq.Header.Set("Authorization", "Bearer "+token)
	presenceResp := httptest.NewRecorder()
	handler.ServeHTTP(presenceResp, presenceReq)
	if presenceResp.Code != http.StatusAccepted {
		t.Fatalf("expected presence 202, got %d", presenceResp.Code)
	}
	if store.ephemeralPresence.ConnID != "agent-1" || store.ephemeralPresence.Room != "tenant-a:room-1" || store.ephemeralPresence.TTL != 2*time.Second {
		t.Fatalf("unexpected ephemeral presence: %+v", store.ephemeralPresence)
	}
	if len(store.presenceEvents) != 1 || store.presenceEvents[0].OriginNode != "admin:node-a" {
		t.Fatalf("unexpected presence event: %+v", store.presenceEvents)
	}
}

func TestAdminHandlerErrorBranches(t *testing.T) {
	verifier, token, cleanup := newAdminTestVerifier(t, map[string]any{
		"tenant": "tenant-a",
		"scope":  "publish:tenant-a:* presence:tenant-a:*",
	})
	defer cleanup()

	store := &fakeAdminStore{}
	service := newTestAdminService(verifier, store)
	handler := service.Handler()

	statsPostResp := httptest.NewRecorder()
	handler.ServeHTTP(statsPostResp, httptest.NewRequest(http.MethodPost, "/v1/stats", nil))
	if statsPostResp.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected stats POST 405, got %d", statsPostResp.Code)
	}

	statsNoAuthResp := httptest.NewRecorder()
	handler.ServeHTTP(statsNoAuthResp, httptest.NewRequest(http.MethodGet, "/v1/stats", nil))
	if statsNoAuthResp.Code != http.StatusUnauthorized {
		t.Fatalf("expected stats without auth 401, got %d", statsNoAuthResp.Code)
	}

	store.aggregateStatsErr = errors.New("stats failed")
	statsReq := httptest.NewRequest(http.MethodGet, "/v1/stats", nil)
	statsReq.Header.Set("Authorization", "Bearer "+token)
	statsErrResp := httptest.NewRecorder()
	handler.ServeHTTP(statsErrResp, statsReq)
	if statsErrResp.Code != http.StatusInternalServerError {
		t.Fatalf("expected stats aggregate failure 500, got %d", statsErrResp.Code)
	}
	store.aggregateStatsErr = nil

	store.healthyErr = errors.New("redis down")
	readyResp := httptest.NewRecorder()
	handler.ServeHTTP(readyResp, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if readyResp.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected unhealthy readyz 503, got %d", readyResp.Code)
	}
	store.healthyErr = nil

	store.publishErr = errors.New("publish failed")
	publishReq := httptest.NewRequest(http.MethodPost, "/v1/publish", strings.NewReader(`{"room":"tenant-a:room-1","event":"doc.update","payload":{"ok":true}}`))
	publishReq.Header.Set("Authorization", "Bearer "+token)
	publishErrResp := httptest.NewRecorder()
	handler.ServeHTTP(publishErrResp, publishReq)
	if publishErrResp.Code != http.StatusInternalServerError {
		t.Fatalf("expected publish failure 500, got %d", publishErrResp.Code)
	}
	store.publishErr = nil

	presenceTTLReq := httptest.NewRequest(http.MethodPost, "/v1/presence", strings.NewReader(`{"room":"tenant-a:room-1","conn_id":"agent-1","state":{},"ttl_seconds":3601}`))
	presenceTTLReq.Header.Set("Authorization", "Bearer "+token)
	presenceTTLResp := httptest.NewRecorder()
	handler.ServeHTTP(presenceTTLResp, presenceTTLReq)
	if presenceTTLResp.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid ttl 400, got %d", presenceTTLResp.Code)
	}

	store.setEphemeralErr = errors.New("presence failed")
	presenceReq := httptest.NewRequest(http.MethodPost, "/v1/presence", strings.NewReader(`{"room":"tenant-a:room-1","conn_id":"agent-1","state":{}}`))
	presenceReq.Header.Set("Authorization", "Bearer "+token)
	presenceErrResp := httptest.NewRecorder()
	handler.ServeHTTP(presenceErrResp, presenceReq)
	if presenceErrResp.Code != http.StatusInternalServerError {
		t.Fatalf("expected set presence failure 500, got %d", presenceErrResp.Code)
	}
	store.setEphemeralErr = nil

	store.publishPresenceErr = errors.New("publish presence failed")
	presencePublishReq := httptest.NewRequest(http.MethodPost, "/v1/presence", strings.NewReader(`{"room":"tenant-a:room-1","conn_id":"agent-1","state":{}}`))
	presencePublishReq.Header.Set("Authorization", "Bearer "+token)
	presencePublishResp := httptest.NewRecorder()
	handler.ServeHTTP(presencePublishResp, presencePublishReq)
	if presencePublishResp.Code != http.StatusInternalServerError {
		t.Fatalf("expected publish presence failure 500, got %d", presencePublishResp.Code)
	}
}

func TestAdminPublishValidationBranches(t *testing.T) {
	verifier, token, cleanup := newAdminTestVerifier(t, map[string]any{
		"tenant": "tenant-a",
		"scope":  "publish:tenant-a:*",
	})
	defer cleanup()

	handler := newTestAdminService(verifier, &fakeAdminStore{}).Handler()

	tests := []struct {
		name   string
		method string
		token  string
		body   string
		want   int
	}{
		{
			name:   "method not allowed",
			method: http.MethodGet,
			token:  token,
			want:   http.StatusMethodNotAllowed,
		},
		{
			name:   "missing auth",
			method: http.MethodPost,
			body:   `{"room":"tenant-a:room-1","event":"doc.update","payload":{}}`,
			want:   http.StatusUnauthorized,
		},
		{
			name:   "malformed json",
			method: http.MethodPost,
			token:  token,
			body:   `{`,
			want:   http.StatusBadRequest,
		},
		{
			name:   "missing fields",
			method: http.MethodPost,
			token:  token,
			body:   `{"room":"tenant-a:room-1","payload":{}}`,
			want:   http.StatusBadRequest,
		},
		{
			name:   "invalid room",
			method: http.MethodPost,
			token:  token,
			body:   `{"room":"tenant-a:bad room","event":"doc.update","payload":{}}`,
			want:   http.StatusBadRequest,
		},
		{
			name:   "invalid event",
			method: http.MethodPost,
			token:  token,
			body:   `{"room":"tenant-a:room-1","event":"bad event","payload":{}}`,
			want:   http.StatusBadRequest,
		},
		{
			name:   "invalid exclude sender",
			method: http.MethodPost,
			token:  token,
			body:   `{"room":"tenant-a:room-1","event":"doc.update","payload":{},"exclude_sender_conn_id":"bad conn"}`,
			want:   http.StatusBadRequest,
		},
		{
			name:   "oversized payload",
			method: http.MethodPost,
			token:  token,
			body:   `{"room":"tenant-a:room-1","event":"doc.update","payload":{"body":"` + strings.Repeat("x", 260) + `"}}`,
			want:   http.StatusRequestEntityTooLarge,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp := performAdminRequest(handler, tc.token, tc.method, "/v1/publish", tc.body)
			if resp.Code != tc.want {
				t.Fatalf("expected %d, got %d body=%q", tc.want, resp.Code, resp.Body.String())
			}
		})
	}

	noStoreHandler := newTestAdminService(verifier, nil).Handler()
	resp := performAdminRequest(noStoreHandler, token, http.MethodPost, "/v1/publish", `{"room":"tenant-a:room-1","event":"doc.update","payload":{}}`)
	if resp.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected no-store publish 503, got %d", resp.Code)
	}

	forbiddenVerifier, forbiddenToken, forbiddenCleanup := newAdminTestVerifier(t, map[string]any{
		"tenant": "tenant-a",
		"scope":  "publish:tenant-b:*",
	})
	defer forbiddenCleanup()
	forbiddenHandler := newTestAdminService(forbiddenVerifier, &fakeAdminStore{}).Handler()
	resp = performAdminRequest(forbiddenHandler, forbiddenToken, http.MethodPost, "/v1/publish", `{"room":"tenant-a:room-1","event":"doc.update","payload":{}}`)
	if resp.Code != http.StatusForbidden {
		t.Fatalf("expected forbidden publish 403, got %d", resp.Code)
	}
}

func TestAdminPresenceValidationBranches(t *testing.T) {
	verifier, token, cleanup := newAdminTestVerifier(t, map[string]any{
		"tenant": "tenant-a",
		"scope":  "presence:tenant-a:*",
	})
	defer cleanup()

	handler := newTestAdminService(verifier, &fakeAdminStore{}).Handler()

	tests := []struct {
		name   string
		method string
		token  string
		body   string
		want   int
	}{
		{
			name:   "method not allowed",
			method: http.MethodGet,
			token:  token,
			want:   http.StatusMethodNotAllowed,
		},
		{
			name:   "missing auth",
			method: http.MethodPost,
			body:   `{"room":"tenant-a:room-1","conn_id":"agent-1","state":{}}`,
			want:   http.StatusUnauthorized,
		},
		{
			name:   "malformed json",
			method: http.MethodPost,
			token:  token,
			body:   `{`,
			want:   http.StatusBadRequest,
		},
		{
			name:   "missing fields",
			method: http.MethodPost,
			token:  token,
			body:   `{"room":"tenant-a:room-1","state":{}}`,
			want:   http.StatusBadRequest,
		},
		{
			name:   "non object state",
			method: http.MethodPost,
			token:  token,
			body:   `{"room":"tenant-a:room-1","conn_id":"agent-1","state":[]}`,
			want:   http.StatusBadRequest,
		},
		{
			name:   "invalid room",
			method: http.MethodPost,
			token:  token,
			body:   `{"room":"tenant-a:bad room","conn_id":"agent-1","state":{}}`,
			want:   http.StatusBadRequest,
		},
		{
			name:   "invalid connection id",
			method: http.MethodPost,
			token:  token,
			body:   `{"room":"tenant-a:room-1","conn_id":"bad conn","state":{}}`,
			want:   http.StatusBadRequest,
		},
		{
			name:   "oversized state",
			method: http.MethodPost,
			token:  token,
			body:   `{"room":"tenant-a:room-1","conn_id":"agent-1","state":{"body":"` + strings.Repeat("x", 260) + `"}}`,
			want:   http.StatusRequestEntityTooLarge,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp := performAdminRequest(handler, tc.token, tc.method, "/v1/presence", tc.body)
			if resp.Code != tc.want {
				t.Fatalf("expected %d, got %d body=%q", tc.want, resp.Code, resp.Body.String())
			}
		})
	}

	noStoreHandler := newTestAdminService(verifier, nil).Handler()
	resp := performAdminRequest(noStoreHandler, token, http.MethodPost, "/v1/presence", `{"room":"tenant-a:room-1","conn_id":"agent-1","state":{}}`)
	if resp.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected no-store presence 503, got %d", resp.Code)
	}

	forbiddenVerifier, forbiddenToken, forbiddenCleanup := newAdminTestVerifier(t, map[string]any{
		"tenant": "tenant-a",
		"scope":  "presence:tenant-b:*",
	})
	defer forbiddenCleanup()
	forbiddenHandler := newTestAdminService(forbiddenVerifier, &fakeAdminStore{}).Handler()
	resp = performAdminRequest(forbiddenHandler, forbiddenToken, http.MethodPost, "/v1/presence", `{"room":"tenant-a:room-1","conn_id":"agent-1","state":{}}`)
	if resp.Code != http.StatusForbidden {
		t.Fatalf("expected forbidden presence 403, got %d", resp.Code)
	}
}

func TestAdminCreateRoomAndActiveUsersHandlers(t *testing.T) {
	verifier, token, cleanup := newAdminTestVerifier(t, map[string]any{
		"tenant": "tenant-a",
		"scope":  "rooms:tenant-a:* presence:tenant-a:*",
	})
	defer cleanup()

	now := time.Unix(200, 0).UTC()
	store := &fakeAdminStore{
		createRoomRecord: cluster.RoomRecord{
			ID:              "tenant-a:room-1",
			Metadata:        json.RawMessage(`{"title":"Draft"}`),
			DefaultAccesses: []string{cluster.PermissionRoomRead},
			CreatedAt:       now,
			UpdatedAt:       now,
		},
		activeUsers: []cluster.ActiveUser{
			{
				Type:         "user",
				ConnectionID: "conn-1",
				ID:           "user-1",
				Tenant:       "tenant-a",
				NodeID:       "node-a",
				ConnectedAt:  now,
				Presence:     json.RawMessage(`{"cursor":{"x":1}}`),
			},
		},
	}
	handler := newTestAdminService(verifier, store).Handler()

	createResp := performAdminRequest(handler, token, http.MethodPost, "/v1/rooms", `{"id":"tenant-a:room-1","metadata":{"title":"Draft"},"defaultAccesses":["room:read"]}`)
	if createResp.Code != http.StatusCreated {
		t.Fatalf("expected create room 201, got %d", createResp.Code)
	}
	if store.createdRoom.ID != "tenant-a:room-1" || string(store.createdRoom.Metadata) != `{"title":"Draft"}` {
		t.Fatalf("unexpected created room capture: %+v", store.createdRoom)
	}
	var created cluster.RoomRecord
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode create room response: %v", err)
	}
	if created.ID != "tenant-a:room-1" || string(created.Metadata) != `{"title":"Draft"}` {
		t.Fatalf("unexpected create room response: %+v", created)
	}

	activeResp := performAdminRequest(handler, token, http.MethodGet, "/v1/rooms/tenant-a%3Aroom-1/active_users", "")
	if activeResp.Code != http.StatusOK {
		t.Fatalf("expected active users 200, got %d", activeResp.Code)
	}
	var active activeUsersResponse
	if err := json.NewDecoder(activeResp.Body).Decode(&active); err != nil {
		t.Fatalf("decode active users response: %v", err)
	}
	if len(active.Data) != 1 || active.Data[0].ConnectionID != "conn-1" {
		t.Fatalf("unexpected active users response: %+v", active)
	}
}

func TestAdminCreateRoomAndActiveUsersErrorBranches(t *testing.T) {
	verifier, token, cleanup := newAdminTestVerifier(t, map[string]any{
		"tenant": "tenant-a",
		"scope":  "rooms:tenant-a:* presence:tenant-a:*",
	})
	defer cleanup()

	store := &fakeAdminStore{}
	handler := newTestAdminService(verifier, store).Handler()

	resp := performAdminRequest(handler, token, http.MethodPost, "/v1/rooms", `{"id":"tenant-a:room-1","metadata":{"title":"Draft"},"defaultAccesses":["room:delete"]}`)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid create access 400, got %d", resp.Code)
	}
	resp = performAdminRequest(handler, token, http.MethodPost, "/v1/rooms", `{"id":"tenant-a:bad room","metadata":{"title":"Draft"}}`)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid create room id 400, got %d", resp.Code)
	}

	store.createRoomErr = cluster.ErrRoomAlreadyExists
	resp = performAdminRequest(handler, token, http.MethodPost, "/v1/rooms", `{"id":"tenant-a:room-1","metadata":{"title":"Draft"}}`)
	if resp.Code != http.StatusConflict {
		t.Fatalf("expected create room conflict 409, got %d", resp.Code)
	}
	store.createRoomErr = errors.New("create failed")
	resp = performAdminRequest(handler, token, http.MethodPost, "/v1/rooms", `{"id":"tenant-a:room-1","metadata":{"title":"Draft"}}`)
	if resp.Code != http.StatusInternalServerError {
		t.Fatalf("expected create room failure 500, got %d", resp.Code)
	}
	store.createRoomErr = nil

	store.activeUsersErr = errors.New("active users failed")
	resp = performAdminRequest(handler, token, http.MethodGet, "/v1/rooms/tenant-a%3Aroom-1/active_users", "")
	if resp.Code != http.StatusInternalServerError {
		t.Fatalf("expected active users failure 500, got %d", resp.Code)
	}
	store.activeUsersErr = nil

	resp = performAdminRequest(handler, token, http.MethodPost, "/v1/rooms/tenant-a%3Aroom-1/active_users", "")
	if resp.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected active users POST 405, got %d", resp.Code)
	}
	noStoreHandler := newTestAdminService(verifier, nil).Handler()
	resp = performAdminRequest(noStoreHandler, token, http.MethodGet, "/v1/rooms/tenant-a%3Aroom-1/active_users", "")
	if resp.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected active users without store 503, got %d", resp.Code)
	}

	forbiddenVerifier, forbiddenToken, forbiddenCleanup := newAdminTestVerifier(t, map[string]any{
		"tenant": "tenant-a",
		"scope":  "presence:tenant-b:*",
	})
	defer forbiddenCleanup()
	forbiddenHandler := newTestAdminService(forbiddenVerifier, &fakeAdminStore{}).Handler()
	resp = performAdminRequest(forbiddenHandler, forbiddenToken, http.MethodGet, "/v1/rooms/tenant-a%3Aroom-1/active_users", "")
	if resp.Code != http.StatusForbidden {
		t.Fatalf("expected active users forbidden 403, got %d", resp.Code)
	}
}

func TestAdminThreadHandlers(t *testing.T) {
	verifier, token, cleanup := newAdminTestVerifier(t, map[string]any{
		"tenant": "tenant-a",
		"scope":  "comments:tenant-a:*",
	})
	defer cleanup()

	now := time.Unix(300, 0).UTC()
	thread := cluster.ThreadRecord{
		Type:      "thread",
		ID:        "thread-1",
		RoomID:    "tenant-a:room-1",
		Metadata:  json.RawMessage(`{"anchor":"shape-1"}`),
		CreatedAt: now,
		UpdatedAt: now,
		Comments: []cluster.CommentRecord{
			{
				Type:      "comment",
				ID:        "comment-1",
				ThreadID:  "thread-1",
				RoomID:    "tenant-a:room-1",
				UserID:    "user-1",
				Body:      json.RawMessage(`{"content":[{"type":"paragraph","text":"first"}]}`),
				Metadata:  json.RawMessage(`{}`),
				CreatedAt: now,
			},
		},
	}
	store := &fakeAdminStore{
		createThreadRecord: thread,
		listThreads:        []cluster.ThreadRecord{thread},
		addCommentThread: cluster.ThreadRecord{
			Type:      "thread",
			ID:        "thread-1",
			RoomID:    "tenant-a:room-1",
			Metadata:  json.RawMessage(`{"anchor":"shape-1"}`),
			CreatedAt: now,
			UpdatedAt: now.Add(time.Second),
			Comments: append(thread.Comments, cluster.CommentRecord{
				Type:      "comment",
				ID:        "comment-2",
				ThreadID:  "thread-1",
				RoomID:    "tenant-a:room-1",
				UserID:    "user-2",
				Body:      json.RawMessage(`{"content":[{"type":"paragraph","text":"second"}]}`),
				Metadata:  json.RawMessage(`{}`),
				CreatedAt: now.Add(time.Second),
			}),
		},
		updateCommentThread: cluster.ThreadRecord{
			Type:      "thread",
			ID:        "thread-1",
			RoomID:    "tenant-a:room-1",
			Metadata:  json.RawMessage(`{"anchor":"shape-1"}`),
			CreatedAt: now,
			UpdatedAt: now.Add(2 * time.Second),
			Comments: []cluster.CommentRecord{
				{
					Type:      "comment",
					ID:        "comment-1",
					ThreadID:  "thread-1",
					RoomID:    "tenant-a:room-1",
					UserID:    "user-1",
					Body:      json.RawMessage(`{"content":[{"type":"paragraph","text":"edited"}]}`),
					Metadata:  json.RawMessage(`{"status":"resolved"}`),
					Mentions:  []string{"user-2"},
					Reactions: []cluster.CommentReaction{{Emoji: "+1", UserID: "user-2"}},
					CreatedAt: now,
				},
			},
		},
	}
	handler := newTestAdminService(verifier, store).Handler()

	createBody := `{"id":"thread-1","metadata":{"anchor":"shape-1"},"comment":{"id":"comment-1","userId":"user-1","body":{"content":[{"type":"paragraph","text":"first"}]},"mentions":["user-2"],"reactions":[{"emoji":"+1","userId":"user-2"}]}}`
	createResp := performAdminRequest(handler, token, http.MethodPost, "/v1/rooms/tenant-a%3Aroom-1/threads", createBody)
	if createResp.Code != http.StatusCreated {
		t.Fatalf("expected create thread 201, got %d body=%q", createResp.Code, createResp.Body.String())
	}
	if store.createdThread.ID != "thread-1" || len(store.createdThread.Comments) != 1 || store.createdThread.Comments[0].UserID != "user-1" {
		t.Fatalf("unexpected created thread capture: %+v", store.createdThread)
	}
	if len(store.createdThread.Comments[0].Mentions) != 1 || store.createdThread.Comments[0].Mentions[0] != "user-2" || len(store.createdThread.Comments[0].Reactions) != 1 {
		t.Fatalf("expected mention/reaction capture: %+v", store.createdThread.Comments[0])
	}
	assertCommentEvent(t, store.publishedEvents, 0, commentEventThreadCreated, commentEventTypeThreadCreated, "thread-1", "comment-1")

	listResp := performAdminRequest(handler, token, http.MethodGet, "/v1/rooms/tenant-a%3Aroom-1/threads", "")
	if listResp.Code != http.StatusOK {
		t.Fatalf("expected list threads 200, got %d", listResp.Code)
	}
	var list threadListResponse
	if err := json.NewDecoder(listResp.Body).Decode(&list); err != nil {
		t.Fatalf("decode thread list: %v", err)
	}
	if len(list.Data) != 1 || list.Data[0].ID != "thread-1" || len(list.Data[0].Comments) != 1 {
		t.Fatalf("unexpected thread list: %+v", list)
	}

	commentResp := performAdminRequest(handler, token, http.MethodPost, "/v1/rooms/tenant-a%3Aroom-1/threads/thread-1/comments", `{"id":"comment-2","userId":"user-2","body":{"content":[{"type":"paragraph","text":"second"}]}}`)
	if commentResp.Code != http.StatusCreated {
		t.Fatalf("expected add comment 201, got %d body=%q", commentResp.Code, commentResp.Body.String())
	}
	if store.addedComment.ID != "comment-2" || store.addedComment.ThreadID != "thread-1" || store.addedComment.RoomID != "tenant-a:room-1" {
		t.Fatalf("unexpected added comment capture: %+v", store.addedComment)
	}
	var updated cluster.ThreadRecord
	if err := json.NewDecoder(commentResp.Body).Decode(&updated); err != nil {
		t.Fatalf("decode updated thread: %v", err)
	}
	if len(updated.Comments) != 2 {
		t.Fatalf("unexpected updated thread: %+v", updated)
	}
	assertCommentEvent(t, store.publishedEvents, 1, commentEventCommentCreated, commentEventTypeCommentCreated, "thread-1", "comment-2")

	updateResp := performAdminRequest(handler, token, http.MethodPatch, "/v1/rooms/tenant-a%3Aroom-1/threads/thread-1/comments/comment-1", `{"body":{"content":[{"type":"paragraph","text":"edited"}]},"metadata":{"status":"resolved"},"mentions":["user-2"],"reactions":[{"emoji":"+1","userId":"user-2"}]}`)
	if updateResp.Code != http.StatusOK {
		t.Fatalf("expected update comment 200, got %d body=%q", updateResp.Code, updateResp.Body.String())
	}
	if store.updatedComment.Room != "tenant-a:room-1" || store.updatedComment.ThreadID != "thread-1" || store.updatedComment.CommentID != "comment-1" {
		t.Fatalf("unexpected updated comment target: %+v", store.updatedComment)
	}
	if !store.updatedComment.Update.BodySet || string(store.updatedComment.Update.Body) != `{"content":[{"type":"paragraph","text":"edited"}]}` {
		t.Fatalf("unexpected updated comment body: %+v", store.updatedComment.Update)
	}
	if !store.updatedComment.Update.MetadataSet || string(store.updatedComment.Update.Metadata) != `{"status":"resolved"}` {
		t.Fatalf("unexpected updated comment metadata: %+v", store.updatedComment.Update)
	}
	if !store.updatedComment.Update.MentionsSet || len(store.updatedComment.Update.Mentions) != 1 || store.updatedComment.Update.Mentions[0] != "user-2" {
		t.Fatalf("unexpected updated comment mentions: %+v", store.updatedComment.Update)
	}
	if !store.updatedComment.Update.ReactionsSet || len(store.updatedComment.Update.Reactions) != 1 || store.updatedComment.Update.Reactions[0].UserID != "user-2" {
		t.Fatalf("unexpected updated comment reactions: %+v", store.updatedComment.Update)
	}
	var patched cluster.ThreadRecord
	if err := json.NewDecoder(updateResp.Body).Decode(&patched); err != nil {
		t.Fatalf("decode patched thread: %v", err)
	}
	if len(patched.Comments) != 1 || len(patched.Comments[0].Reactions) != 1 || patched.Comments[0].Mentions[0] != "user-2" {
		t.Fatalf("unexpected patched thread response: %+v", patched)
	}
	assertCommentEvent(t, store.publishedEvents, 2, commentEventCommentUpdated, commentEventTypeCommentUpdated, "thread-1", "comment-1")

	generatedStore := &fakeAdminStore{
		addCommentThread: cluster.ThreadRecord{ID: "thread-1", RoomID: "tenant-a:room-1"},
	}
	generatedHandler := newTestAdminService(verifier, generatedStore).Handler()
	generatedThreadResp := performAdminRequest(generatedHandler, token, http.MethodPost, "/v1/rooms/tenant-a%3Aroom-1/threads", `{"comment":{"userId":"user-1","body":{}}}`)
	if generatedThreadResp.Code != http.StatusCreated {
		t.Fatalf("expected generated thread create 201, got %d body=%q", generatedThreadResp.Code, generatedThreadResp.Body.String())
	}
	if !strings.HasPrefix(generatedStore.createdThread.ID, "th_") || !strings.HasPrefix(generatedStore.createdThread.Comments[0].ID, "cm_") {
		t.Fatalf("expected generated thread/comment ids, got %+v", generatedStore.createdThread)
	}
	generatedCommentResp := performAdminRequest(generatedHandler, token, http.MethodPost, "/v1/rooms/tenant-a%3Aroom-1/threads/thread-1/comments", `{"userId":"user-1","body":{}}`)
	if generatedCommentResp.Code != http.StatusCreated {
		t.Fatalf("expected generated comment 201, got %d body=%q", generatedCommentResp.Code, generatedCommentResp.Body.String())
	}
	if !strings.HasPrefix(generatedStore.addedComment.ID, "cm_") {
		t.Fatalf("expected generated comment id, got %+v", generatedStore.addedComment)
	}
}

func assertCommentEvent(t *testing.T, events []cluster.PublishedEvent, index int, eventName string, eventType string, threadID string, commentID string) {
	t.Helper()
	if len(events) <= index {
		t.Fatalf("expected published comment event %d, got %d events", index, len(events))
	}
	event := events[index]
	if event.Room != "tenant-a:room-1" || event.Event != eventName || event.OriginNode != "admin:node-a" {
		t.Fatalf("unexpected published comment event: %+v", event)
	}
	var payload commentEventPayload
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		t.Fatalf("decode comment event payload: %v", err)
	}
	if payload.Type != eventType || payload.RoomID != "tenant-a:room-1" || payload.ThreadID != threadID || payload.Thread.ID != threadID {
		t.Fatalf("unexpected comment event payload: %+v", payload)
	}
	if commentID == "" {
		if payload.Comment != nil || payload.CommentID != "" {
			t.Fatalf("unexpected comment payload for thread event: %+v", payload)
		}
		return
	}
	if payload.CommentID != commentID || payload.Comment == nil || payload.Comment.ID != commentID {
		t.Fatalf("unexpected comment payload: %+v", payload)
	}
}

func assertNotificationEvent(t *testing.T, events []cluster.PublishedEvent, index int, eventName string, eventType string, userID string, notificationID string) {
	t.Helper()
	if len(events) <= index {
		t.Fatalf("expected published notification event %d, got %d events", index, len(events))
	}
	event := events[index]
	if event.Room != notificationEventRoom(userID) || event.Event != eventName || event.OriginNode != "admin:node-a" {
		t.Fatalf("unexpected published notification event: %+v", event)
	}
	var payload notificationEventPayload
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		t.Fatalf("decode notification event payload: %v", err)
	}
	if payload.Type != eventType || payload.UserID != userID {
		t.Fatalf("unexpected notification event payload: %+v", payload)
	}
	if notificationID == "" {
		if payload.NotificationID != "" || payload.Notification != nil {
			t.Fatalf("unexpected notification payload for delete-all event: %+v", payload)
		}
		return
	}
	if payload.NotificationID != notificationID || payload.Notification == nil || payload.Notification.ID != notificationID {
		t.Fatalf("unexpected notification payload: %+v", payload)
	}
}

func TestAdminThreadErrorBranches(t *testing.T) {
	verifier, token, cleanup := newAdminTestVerifier(t, map[string]any{
		"tenant": "tenant-a",
		"scope":  "comments:tenant-a:*",
	})
	defer cleanup()

	store := &fakeAdminStore{}
	handler := newTestAdminService(verifier, store).Handler()

	noStoreHandler := newTestAdminService(verifier, nil).Handler()
	resp := performAdminRequest(noStoreHandler, token, http.MethodGet, "/v1/rooms/tenant-a%3Aroom-1/threads", "")
	if resp.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected no-store threads 503, got %d", resp.Code)
	}
	resp = performAdminRequest(noStoreHandler, token, http.MethodPost, "/v1/rooms/tenant-a%3Aroom-1/threads", `{"comment":{"userId":"user-1","body":{}}}`)
	if resp.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected no-store create thread 503, got %d", resp.Code)
	}
	resp = performAdminRequest(noStoreHandler, token, http.MethodPost, "/v1/rooms/tenant-a%3Aroom-1/threads/thread-1/comments", `{"userId":"user-1","body":{}}`)
	if resp.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected no-store add comment 503, got %d", resp.Code)
	}
	resp = performAdminRequest(noStoreHandler, token, http.MethodPatch, "/v1/rooms/tenant-a%3Aroom-1/threads/thread-1/comments/comment-1", `{"metadata":{}}`)
	if resp.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected no-store update comment 503, got %d", resp.Code)
	}

	for _, tc := range []struct {
		method string
		path   string
		body   string
	}{
		{method: http.MethodGet, path: "/v1/rooms/tenant-a%3Aroom-1/threads"},
		{method: http.MethodPost, path: "/v1/rooms/tenant-a%3Aroom-1/threads", body: `{"comment":{"userId":"user-1","body":{}}}`},
		{method: http.MethodPost, path: "/v1/rooms/tenant-a%3Aroom-1/threads/thread-1/comments", body: `{"userId":"user-1","body":{}}`},
		{method: http.MethodPatch, path: "/v1/rooms/tenant-a%3Aroom-1/threads/thread-1/comments/comment-1", body: `{"metadata":{}}`},
	} {
		request := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		noAuthResp := httptest.NewRecorder()
		handler.ServeHTTP(noAuthResp, request)
		if noAuthResp.Code != http.StatusUnauthorized {
			t.Fatalf("expected no-auth %s %s 401, got %d", tc.method, tc.path, noAuthResp.Code)
		}
	}

	resp = performAdminRequest(handler, token, http.MethodPut, "/v1/rooms/tenant-a%3Aroom-1/threads", "")
	if resp.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected threads PUT 405, got %d", resp.Code)
	}
	resp = performAdminRequest(handler, token, http.MethodGet, "/v1/rooms/tenant-a%3Aroom-1/threads/thread-1/comments", "")
	if resp.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected add-comment GET 405, got %d", resp.Code)
	}
	resp = performAdminRequest(handler, token, http.MethodGet, "/v1/rooms/tenant-a%3Aroom-1/threads/thread-1/comments/comment-1", "")
	if resp.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected update-comment GET 405, got %d", resp.Code)
	}
	resp = performAdminRequest(handler, token, http.MethodPost, "/v1/rooms/tenant-a%3Aroom-1/threads/bad%20thread/comments", `{"id":"comment-1","userId":"user-1","body":{}}`)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected unsafe thread id 400, got %d", resp.Code)
	}
	resp = performAdminRequest(handler, token, http.MethodPatch, "/v1/rooms/tenant-a%3Aroom-1/threads/thread-1/comments/bad%20comment", `{"metadata":{}}`)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected unsafe comment id 400, got %d", resp.Code)
	}
	resp = performAdminRequest(handler, token, http.MethodPost, "/v1/rooms/tenant-a%3Aroom-1/threads/thread-1/reactions", `{}`)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected unsupported thread subresource 400, got %d", resp.Code)
	}

	resp = performAdminRequest(handler, token, http.MethodPost, "/v1/rooms/tenant-a%3Aroom-1/threads", `{"id":"bad thread","comment":{"id":"comment-1","userId":"user-1","body":{}}}`)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid thread id 400, got %d", resp.Code)
	}
	resp = performAdminRequest(handler, token, http.MethodPost, "/v1/rooms/tenant-a%3Aroom-1/threads", `{`)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected malformed thread create body 400, got %d", resp.Code)
	}
	resp = performAdminRequest(handler, token, http.MethodPost, "/v1/rooms/tenant-a%3Aroom-1/threads", `{"id":"thread-1","comment":{"id":"comment-1","userId":"user-1","body":[]}}`)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid comment body 400, got %d", resp.Code)
	}
	store.createThreadErr = cluster.ErrThreadAlreadyExists
	resp = performAdminRequest(handler, token, http.MethodPost, "/v1/rooms/tenant-a%3Aroom-1/threads", `{"id":"thread-1","comment":{"id":"comment-1","userId":"user-1","body":{}}}`)
	if resp.Code != http.StatusConflict {
		t.Fatalf("expected thread conflict 409, got %d", resp.Code)
	}
	store.createThreadErr = errors.New("create thread failed")
	resp = performAdminRequest(handler, token, http.MethodPost, "/v1/rooms/tenant-a%3Aroom-1/threads", `{"id":"thread-1","comment":{"id":"comment-1","userId":"user-1","body":{}}}`)
	if resp.Code != http.StatusInternalServerError {
		t.Fatalf("expected thread create failure 500, got %d", resp.Code)
	}
	store.createThreadErr = nil

	store.listThreadsErr = errors.New("list threads failed")
	resp = performAdminRequest(handler, token, http.MethodGet, "/v1/rooms/tenant-a%3Aroom-1/threads", "")
	if resp.Code != http.StatusInternalServerError {
		t.Fatalf("expected thread list failure 500, got %d", resp.Code)
	}
	store.listThreadsErr = nil

	resp = performAdminRequest(handler, token, http.MethodPost, "/v1/rooms/tenant-a%3Aroom-1/threads/thread-1/comments", `{"id":"comment-1","userId":"user-1","body":{}}`)
	if resp.Code != http.StatusNotFound {
		t.Fatalf("expected missing thread 404, got %d", resp.Code)
	}
	resp = performAdminRequest(handler, token, http.MethodPost, "/v1/rooms/tenant-a%3Aroom-1/threads/thread-1/comments", `{`)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected malformed comment body 400, got %d", resp.Code)
	}
	resp = performAdminRequest(handler, token, http.MethodPost, "/v1/rooms/tenant-a%3Aroom-1/threads/thread-1/comments", `{"id":"comment-1","userId":"user-1","body":[]}`)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid add-comment body 400, got %d", resp.Code)
	}
	store.addCommentErr = errors.New("add comment failed")
	resp = performAdminRequest(handler, token, http.MethodPost, "/v1/rooms/tenant-a%3Aroom-1/threads/thread-1/comments", `{"id":"comment-1","userId":"user-1","body":{}}`)
	if resp.Code != http.StatusInternalServerError {
		t.Fatalf("expected add comment failure 500, got %d", resp.Code)
	}
	store.addCommentErr = nil

	resp = performAdminRequest(handler, token, http.MethodPatch, "/v1/rooms/tenant-a%3Aroom-1/threads/thread-1/comments/comment-1", `{`)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected malformed update-comment body 400, got %d", resp.Code)
	}
	resp = performAdminRequest(handler, token, http.MethodPatch, "/v1/rooms/tenant-a%3Aroom-1/threads/thread-1/comments/comment-1", `{}`)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected empty update-comment body 400, got %d", resp.Code)
	}
	resp = performAdminRequest(handler, token, http.MethodPatch, "/v1/rooms/tenant-a%3Aroom-1/threads/thread-1/comments/comment-1", `{"body":[]}`)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid update-comment body 400, got %d", resp.Code)
	}
	resp = performAdminRequest(handler, token, http.MethodPatch, "/v1/rooms/tenant-a%3Aroom-1/threads/thread-1/comments/comment-1", `{"mentions":["bad user"]}`)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid update-comment mentions 400, got %d", resp.Code)
	}
	resp = performAdminRequest(handler, token, http.MethodPatch, "/v1/rooms/tenant-a%3Aroom-1/threads/thread-1/comments/comment-1", `{"metadata":{}}`)
	if resp.Code != http.StatusNotFound {
		t.Fatalf("expected missing comment 404, got %d", resp.Code)
	}
	store.updateCommentErr = errors.New("update comment failed")
	resp = performAdminRequest(handler, token, http.MethodPatch, "/v1/rooms/tenant-a%3Aroom-1/threads/thread-1/comments/comment-1", `{"metadata":{}}`)
	if resp.Code != http.StatusInternalServerError {
		t.Fatalf("expected update comment failure 500, got %d", resp.Code)
	}

	forbiddenVerifier, forbiddenToken, forbiddenCleanup := newAdminTestVerifier(t, map[string]any{
		"tenant": "tenant-a",
		"scope":  "comments:tenant-b:*",
	})
	defer forbiddenCleanup()
	forbiddenHandler := newTestAdminService(forbiddenVerifier, &fakeAdminStore{}).Handler()
	resp = performAdminRequest(forbiddenHandler, forbiddenToken, http.MethodGet, "/v1/rooms/tenant-a%3Aroom-1/threads", "")
	if resp.Code != http.StatusForbidden {
		t.Fatalf("expected thread list forbidden 403, got %d", resp.Code)
	}
	resp = performAdminRequest(forbiddenHandler, forbiddenToken, http.MethodPost, "/v1/rooms/tenant-a%3Aroom-1/threads", `{"id":"thread-1","comment":{"id":"comment-1","userId":"user-1","body":{}}}`)
	if resp.Code != http.StatusForbidden {
		t.Fatalf("expected create thread forbidden 403, got %d", resp.Code)
	}
	resp = performAdminRequest(forbiddenHandler, forbiddenToken, http.MethodPost, "/v1/rooms/tenant-a%3Aroom-1/threads/thread-1/comments", `{"id":"comment-1","userId":"user-1","body":{}}`)
	if resp.Code != http.StatusForbidden {
		t.Fatalf("expected add comment forbidden 403, got %d", resp.Code)
	}
	resp = performAdminRequest(forbiddenHandler, forbiddenToken, http.MethodPatch, "/v1/rooms/tenant-a%3Aroom-1/threads/thread-1/comments/comment-1", `{"metadata":{}}`)
	if resp.Code != http.StatusForbidden {
		t.Fatalf("expected update comment forbidden 403, got %d", resp.Code)
	}
}

func TestAdminNotificationHandlers(t *testing.T) {
	verifier, token, cleanup := newAdminTestVerifier(t, map[string]any{
		"tenant": "tenant-a",
		"scope":  "notifications:user-*",
	})
	defer cleanup()

	readAt := time.Now().UTC()
	notification := cluster.InboxNotificationRecord{
		ID:           "in_1",
		UserID:       "user-1",
		Kind:         "$custom",
		SubjectID:    "subject-1",
		RoomID:       "tenant-a:room-1",
		ActivityData: json.RawMessage(`{"count":1}`),
		NotifiedAt:   time.Now().UTC(),
	}
	readNotification := notification
	readNotification.ReadAt = &readAt
	store := &fakeAdminStore{
		listInboxNotifications: cluster.InboxNotificationList{
			Data:       []cluster.InboxNotificationRecord{notification},
			NextCursor: 1,
		},
		getInboxNotificationRecord:  notification,
		markInboxNotificationRecord: readNotification,
		notificationSettings:        json.RawMessage(`{"email":{"thread":true}}`),
		roomSubscriptionSettings: cluster.RoomSubscriptionSettings{
			RoomID:       "tenant-a:room-1",
			UserID:       "user-1",
			Threads:      "all",
			TextMentions: "mine",
			UpdatedAt:    time.Now().UTC(),
		},
		listRoomSubscriptionSettings: cluster.RoomSubscriptionSettingsList{
			Data: []cluster.RoomSubscriptionSettings{{
				RoomID:       "tenant-a:room-1",
				UserID:       "user-1",
				Threads:      "all",
				TextMentions: "mine",
				UpdatedAt:    time.Now().UTC(),
			}},
		},
	}
	handler := newTestAdminService(verifier, store).Handler()

	triggerResp := performAdminRequest(handler, token, http.MethodPost, "/v1/inbox-notifications/trigger", `{"id":"in_1","userId":"user-1","kind":"$custom","subjectId":"subject-1","roomId":"tenant-a:room-1","activityData":{"count":1}}`)
	if triggerResp.Code != http.StatusCreated {
		t.Fatalf("expected trigger 201, got %d body=%q", triggerResp.Code, triggerResp.Body.String())
	}
	if store.createdInboxNotification.ID != "in_1" || store.createdInboxNotification.UserID != "user-1" {
		t.Fatalf("unexpected created notification: %+v", store.createdInboxNotification)
	}
	assertNotificationEvent(t, store.publishedEvents, 0, notificationEventInboxCreated, notificationEventTypeInboxCreated, "user-1", "in_1")

	listResp := performAdminRequest(handler, token, http.MethodGet, "/v1/users/user-1/inbox-notifications?limit=1&query=unread:true", "")
	if listResp.Code != http.StatusOK {
		t.Fatalf("expected list 200, got %d", listResp.Code)
	}
	var list inboxNotificationListResponse
	if err := json.Unmarshal(listResp.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode notification list: %v", err)
	}
	if len(list.Data) != 1 || list.Data[0].ID != "in_1" || list.NextCursor != "1" {
		t.Fatalf("unexpected notification list: %+v", list)
	}

	getResp := performAdminRequest(handler, token, http.MethodGet, "/v1/users/user-1/inbox-notifications/in_1", "")
	if getResp.Code != http.StatusOK {
		t.Fatalf("expected get 200, got %d", getResp.Code)
	}
	readResp := performAdminRequest(handler, token, http.MethodPost, "/v1/inbox-notifications/in_1/read", "")
	if readResp.Code != http.StatusOK {
		t.Fatalf("expected read 200, got %d body=%q", readResp.Code, readResp.Body.String())
	}
	assertNotificationEvent(t, store.publishedEvents, 1, notificationEventInboxRead, notificationEventTypeInboxRead, "user-1", "in_1")
	deleteResp := performAdminRequest(handler, token, http.MethodDelete, "/v1/users/user-1/inbox-notifications/in_1", "")
	if deleteResp.Code != http.StatusNoContent {
		t.Fatalf("expected delete 204, got %d", deleteResp.Code)
	}
	assertNotificationEvent(t, store.publishedEvents, 2, notificationEventInboxDeleted, notificationEventTypeInboxDeleted, "user-1", "in_1")
	deleteAllResp := performAdminRequest(handler, token, http.MethodDelete, "/v1/users/user-1/inbox-notifications", "")
	if deleteAllResp.Code != http.StatusNoContent {
		t.Fatalf("expected delete all 204, got %d", deleteAllResp.Code)
	}
	assertNotificationEvent(t, store.publishedEvents, 3, notificationEventInboxDeletedAll, notificationEventTypeInboxDeletedAll, "user-1", "")

	settingsResp := performAdminRequest(handler, token, http.MethodGet, "/v1/users/user-1/notification-settings", "")
	if settingsResp.Code != http.StatusOK || strings.TrimSpace(settingsResp.Body.String()) != `{"email":{"thread":true}}` {
		t.Fatalf("unexpected settings response: %d %q", settingsResp.Code, settingsResp.Body.String())
	}
	updateSettingsResp := performAdminRequest(handler, token, http.MethodPost, "/v1/users/user-1/notification-settings", `{"email":{"thread":false}}`)
	if updateSettingsResp.Code != http.StatusOK {
		t.Fatalf("expected update settings 200, got %d", updateSettingsResp.Code)
	}
	if string(store.setNotificationSettingsInput) != `{"email":{"thread":false}}` {
		t.Fatalf("unexpected settings input: %s", store.setNotificationSettingsInput)
	}
	deleteSettingsResp := performAdminRequest(handler, token, http.MethodDelete, "/v1/users/user-1/notification-settings", "")
	if deleteSettingsResp.Code != http.StatusNoContent {
		t.Fatalf("expected delete settings 204, got %d", deleteSettingsResp.Code)
	}

	roomSettingsResp := performAdminRequest(handler, token, http.MethodGet, "/v1/rooms/tenant-a%3Aroom-1/users/user-1/subscription-settings", "")
	if roomSettingsResp.Code != http.StatusOK {
		t.Fatalf("expected get room settings 200, got %d", roomSettingsResp.Code)
	}
	updateRoomSettingsResp := performAdminRequest(handler, token, http.MethodPost, "/v1/rooms/tenant-a%3Aroom-1/users/user-1/subscription-settings", `{"threads":"none","textMentions":"none"}`)
	if updateRoomSettingsResp.Code != http.StatusOK {
		t.Fatalf("expected update room settings 200, got %d", updateRoomSettingsResp.Code)
	}
	if store.setRoomSubscriptionSettingsInput.Threads != "none" || store.setRoomSubscriptionSettingsInput.TextMentions != "none" {
		t.Fatalf("unexpected room settings input: %+v", store.setRoomSubscriptionSettingsInput)
	}
	listRoomSettingsResp := performAdminRequest(handler, token, http.MethodGet, "/v1/users/user-1/room-subscription-settings", "")
	if listRoomSettingsResp.Code != http.StatusOK {
		t.Fatalf("expected list room settings 200, got %d", listRoomSettingsResp.Code)
	}
	var roomSettingsList roomSubscriptionSettingsListResponse
	if err := json.Unmarshal(listRoomSettingsResp.Body.Bytes(), &roomSettingsList); err != nil {
		t.Fatalf("decode room settings list: %v", err)
	}
	if roomSettingsList.NextCursor != "" {
		t.Fatalf("unexpected empty room settings cursor: %+v", roomSettingsList)
	}
	store.listRoomSubscriptionSettings.NextCursor = 7
	listRoomSettingsResp = performAdminRequest(handler, token, http.MethodGet, "/v1/users/user-1/room-subscription-settings", "")
	if listRoomSettingsResp.Code != http.StatusOK {
		t.Fatalf("expected list room settings with cursor 200, got %d", listRoomSettingsResp.Code)
	}
	roomSettingsList = roomSubscriptionSettingsListResponse{}
	if err := json.Unmarshal(listRoomSettingsResp.Body.Bytes(), &roomSettingsList); err != nil {
		t.Fatalf("decode room settings cursor list: %v", err)
	}
	if roomSettingsList.NextCursor != "7" {
		t.Fatalf("unexpected room settings cursor: %+v", roomSettingsList)
	}
	deleteRoomSettingsResp := performAdminRequest(handler, token, http.MethodDelete, "/v1/rooms/tenant-a%3Aroom-1/users/user-1/subscription-settings", "")
	if deleteRoomSettingsResp.Code != http.StatusNoContent {
		t.Fatalf("expected delete room settings 204, got %d", deleteRoomSettingsResp.Code)
	}
}

func TestAdminWebhookDelivery(t *testing.T) {
	verifier, token, cleanup := newAdminTestVerifier(t, map[string]any{
		"tenant": "tenant-a",
		"scope":  "rooms:tenant-a:*",
	})
	defer cleanup()

	var deliveries []capturedWebhook
	webhookServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		deliveries = append(deliveries, capturedWebhook{
			Header: r.Header.Clone(),
			Body:   append([]byte(nil), body...),
		})
		w.WriteHeader(http.StatusNoContent)
	}))
	defer webhookServer.Close()

	client := webhookServer.Client()
	client.Timeout = time.Second
	store := &fakeAdminStore{
		createRoomRecord: cluster.RoomRecord{
			ID:        "tenant-a:room-1",
			Metadata:  json.RawMessage(`{"title":"Draft"}`),
			CreatedAt: time.Unix(400, 0).UTC(),
			UpdatedAt: time.Unix(400, 0).UTC(),
		},
	}
	service := newTestAdminService(verifier, store)
	service.cfg.Webhooks = &config.WebhooksConfig{
		URLs:      []string{webhookServer.URL},
		Secret:    "whsec_test",
		TimeoutMS: 1000,
	}
	service.webhookClient = client
	handler := service.Handler()

	createResp := performAdminRequest(handler, token, http.MethodPost, "/v1/rooms", `{"id":"tenant-a:room-1","metadata":{"title":"Draft"}}`)
	if createResp.Code != http.StatusCreated {
		t.Fatalf("expected room create 201, got %d body=%q", createResp.Code, createResp.Body.String())
	}

	thread := cluster.ThreadRecord{
		Type:   "thread",
		ID:     "thread-1",
		RoomID: "tenant-a:room-1",
		Comments: []cluster.CommentRecord{{
			Type:     "comment",
			ID:       "comment-1",
			ThreadID: "thread-1",
			RoomID:   "tenant-a:room-1",
			UserID:   "user-1",
			Body:     json.RawMessage(`{"content":[{"type":"paragraph","text":"first"}]}`),
		}},
	}
	if err := service.publishCommentEvent(context.Background(), commentEventThreadCreated, thread, &thread.Comments[0]); err != nil {
		t.Fatalf("publish comment event: %v", err)
	}

	notification := cluster.InboxNotificationRecord{
		ID:         "in_1",
		UserID:     "user-1",
		Kind:       "$custom",
		RoomID:     "tenant-a:room-1",
		NotifiedAt: time.Unix(401, 0).UTC(),
	}
	if err := service.publishNotificationEvent(context.Background(), notificationEventInboxCreated, notification.UserID, &notification); err != nil {
		t.Fatalf("publish notification event: %v", err)
	}

	if len(deliveries) != 3 {
		t.Fatalf("expected 3 webhook deliveries, got %d", len(deliveries))
	}

	roomEnvelope := assertWebhookDelivery(t, deliveries[0], roomEventCreated, "whsec_test")
	var roomPayload roomEventPayload
	if err := json.Unmarshal(roomEnvelope.Data, &roomPayload); err != nil {
		t.Fatalf("decode room webhook payload: %v", err)
	}
	if roomPayload.Type != roomEventTypeCreated || roomPayload.RoomID != "tenant-a:room-1" || roomPayload.Room == nil || roomPayload.Room.ID != "tenant-a:room-1" {
		t.Fatalf("unexpected room webhook payload: %+v", roomPayload)
	}

	commentEnvelope := assertWebhookDelivery(t, deliveries[1], commentEventThreadCreated, "whsec_test")
	var commentPayload commentEventPayload
	if err := json.Unmarshal(commentEnvelope.Data, &commentPayload); err != nil {
		t.Fatalf("decode comment webhook payload: %v", err)
	}
	if commentPayload.Type != commentEventTypeThreadCreated || commentPayload.ThreadID != "thread-1" || commentPayload.CommentID != "comment-1" {
		t.Fatalf("unexpected comment webhook payload: %+v", commentPayload)
	}

	notificationEnvelope := assertWebhookDelivery(t, deliveries[2], notificationEventInboxCreated, "whsec_test")
	var notificationPayload notificationEventPayload
	if err := json.Unmarshal(notificationEnvelope.Data, &notificationPayload); err != nil {
		t.Fatalf("decode notification webhook payload: %v", err)
	}
	if notificationPayload.Type != notificationEventTypeInboxCreated || notificationPayload.UserID != "user-1" || notificationPayload.NotificationID != "in_1" {
		t.Fatalf("unexpected notification webhook payload: %+v", notificationPayload)
	}
}

func TestAdminWebhookFailuresAreBestEffort(t *testing.T) {
	verifier, token, cleanup := newAdminTestVerifier(t, map[string]any{
		"tenant": "tenant-a",
		"scope":  "rooms:tenant-a:*",
	})
	defer cleanup()

	webhookServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer webhookServer.Close()

	client := webhookServer.Client()
	client.Timeout = time.Second
	store := &fakeAdminStore{
		createRoomRecord: cluster.RoomRecord{ID: "tenant-a:room-1"},
	}
	service := newTestAdminService(verifier, store)
	service.cfg.Webhooks = &config.WebhooksConfig{
		URLs:      []string{webhookServer.URL},
		Secret:    "whsec_test",
		TimeoutMS: 1000,
	}
	service.webhookClient = client

	resp := performAdminRequest(service.Handler(), token, http.MethodPost, "/v1/rooms", `{"id":"tenant-a:room-1","metadata":{}}`)
	if resp.Code != http.StatusCreated {
		t.Fatalf("expected webhook failure to keep room create successful, got %d body=%q", resp.Code, resp.Body.String())
	}
}

type capturedWebhook struct {
	Header http.Header
	Body   []byte
}

type decodedWebhook struct {
	ID        string          `json:"id"`
	Event     string          `json:"event"`
	CreatedAt string          `json:"createdAt"`
	Data      json.RawMessage `json:"data"`
}

func assertWebhookDelivery(t *testing.T, delivery capturedWebhook, eventName string, secret string) decodedWebhook {
	t.Helper()
	if delivery.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("unexpected webhook content type: %q", delivery.Header.Get("Content-Type"))
	}
	if delivery.Header.Get("User-Agent") != webhookUserAgent {
		t.Fatalf("unexpected webhook user agent: %q", delivery.Header.Get("User-Agent"))
	}
	if delivery.Header.Get("OpenRTC-Webhook-Event") != eventName {
		t.Fatalf("unexpected webhook event header: %q", delivery.Header.Get("OpenRTC-Webhook-Event"))
	}
	timestamp := delivery.Header.Get("OpenRTC-Webhook-Timestamp")
	if timestamp == "" {
		t.Fatalf("missing webhook timestamp")
	}
	wantSignature := "v1=" + signWebhookPayload(secret, timestamp, delivery.Body)
	if delivery.Header.Get("OpenRTC-Webhook-Signature") != wantSignature {
		t.Fatalf("unexpected webhook signature")
	}

	var envelope decodedWebhook
	if err := json.Unmarshal(delivery.Body, &envelope); err != nil {
		t.Fatalf("decode webhook envelope: %v", err)
	}
	if envelope.ID == "" || delivery.Header.Get("OpenRTC-Webhook-Id") != envelope.ID {
		t.Fatalf("unexpected webhook id header=%q envelope=%q", delivery.Header.Get("OpenRTC-Webhook-Id"), envelope.ID)
	}
	if envelope.Event != eventName {
		t.Fatalf("unexpected webhook envelope event: %q", envelope.Event)
	}
	if _, err := time.Parse(time.RFC3339Nano, envelope.CreatedAt); err != nil {
		t.Fatalf("invalid webhook createdAt: %v", err)
	}
	return envelope
}

func TestAdminNotificationErrorBranches(t *testing.T) {
	verifier, token, cleanup := newAdminTestVerifier(t, map[string]any{
		"tenant": "tenant-a",
		"scope":  "notifications:user-1",
	})
	defer cleanup()

	noStoreHandler := newTestAdminService(verifier, nil).Handler()
	resp := performAdminRequest(noStoreHandler, token, http.MethodPost, "/v1/inbox-notifications/trigger", `{"userId":"user-1","kind":"thread"}`)
	if resp.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected no-store trigger 503, got %d", resp.Code)
	}

	store := &fakeAdminStore{getInboxNotificationRecord: cluster.InboxNotificationRecord{ID: "in_1", UserID: "user-1", Kind: "thread"}}
	handler := newTestAdminService(verifier, store).Handler()
	resp = performAdminRequest(handler, token, http.MethodPost, "/v1/inbox-notifications/trigger", `{`)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected malformed trigger JSON 400, got %d", resp.Code)
	}
	resp = performAdminRequest(handler, token, http.MethodGet, "/v1/users/bad%20user/inbox-notifications", "")
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected unsafe user id 400, got %d", resp.Code)
	}
	resp = performAdminRequest(handler, token, http.MethodGet, "/v1/users/user-1/inbox-notifications?limit=99", "")
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid limit 400, got %d", resp.Code)
	}
	resp = performAdminRequest(handler, token, http.MethodGet, "/v1/users/user-1/inbox-notifications?cursor=bad", "")
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid notification cursor 400, got %d", resp.Code)
	}
	resp = performAdminRequest(handler, token, http.MethodPost, "/v1/inbox-notifications/trigger", `{"userId":"user-1","kind":"$bad-kind"}`)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid notification kind 400, got %d", resp.Code)
	}
	store.createInboxNotificationErr = cluster.ErrInboxAlreadyExists
	resp = performAdminRequest(handler, token, http.MethodPost, "/v1/inbox-notifications/trigger", `{"id":"in_1","userId":"user-1","kind":"thread"}`)
	if resp.Code != http.StatusConflict {
		t.Fatalf("expected notification conflict 409, got %d", resp.Code)
	}
	store.createInboxNotificationErr = nil
	store.getInboxNotificationErr = cluster.ErrInboxNotFound
	resp = performAdminRequest(handler, token, http.MethodGet, "/v1/users/user-1/inbox-notifications/in_1", "")
	if resp.Code != http.StatusNotFound {
		t.Fatalf("expected notification not found 404, got %d", resp.Code)
	}
	store.getInboxNotificationErr = errors.New("get failed")
	resp = performAdminRequest(handler, token, http.MethodGet, "/v1/users/user-1/inbox-notifications/in_1", "")
	if resp.Code != http.StatusInternalServerError {
		t.Fatalf("expected notification get failure 500, got %d", resp.Code)
	}
	store.getInboxNotificationErr = cluster.ErrInboxNotFound
	resp = performAdminRequest(handler, token, http.MethodPost, "/v1/inbox-notifications/in_1/read", "")
	if resp.Code != http.StatusNotFound {
		t.Fatalf("expected read missing notification 404, got %d", resp.Code)
	}
	store.getInboxNotificationErr = nil
	resp = performAdminRequest(handler, token, http.MethodGet, "/v1/inbox-notifications/in_1/archive", "")
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected unsupported notification action 400, got %d", resp.Code)
	}
	store.getInboxNotificationErr = errors.New("get failed")
	resp = performAdminRequest(handler, token, http.MethodPost, "/v1/inbox-notifications/in_1/read", "")
	if resp.Code != http.StatusInternalServerError {
		t.Fatalf("expected get notification read failure 500, got %d", resp.Code)
	}
	store.getInboxNotificationErr = nil
	store.markInboxNotificationErr = cluster.ErrInboxNotFound
	resp = performAdminRequest(handler, token, http.MethodPost, "/v1/inbox-notifications/in_1/read", "")
	if resp.Code != http.StatusNotFound {
		t.Fatalf("expected mark missing notification 404, got %d", resp.Code)
	}
	store.markInboxNotificationErr = nil
	actionReq := httptest.NewRequest(http.MethodPost, "/v1/inbox-notifications/in_1/read", nil)
	actionReq.URL.Path = "/v1/inbox-notifications/%zz/read"
	actionReq.Header.Set("Authorization", "Bearer "+token)
	actionResp := httptest.NewRecorder()
	newTestAdminService(verifier, store).handleInboxNotificationAction(actionResp, actionReq)
	if actionResp.Code != http.StatusBadRequest {
		t.Fatalf("expected malformed notification action path 400, got %d", actionResp.Code)
	}
	resp = performAdminRequest(handler, token, http.MethodPost, "/v1/users/user-1/notification-settings", `[]`)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid notification settings 400, got %d", resp.Code)
	}
	resp = performAdminRequest(handler, token, http.MethodPost, "/v1/rooms/tenant-a%3Aroom-1/users/user-1/subscription-settings", `{`)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected malformed room subscription settings 400, got %d", resp.Code)
	}
	resp = performAdminRequest(handler, token, http.MethodPost, "/v1/rooms/tenant-a%3Aroom-1/users/user-1/subscription-settings", `{"threads":"bad"}`)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid room subscription settings 400, got %d", resp.Code)
	}
	resp = performAdminRequest(handler, token, http.MethodGet, "/v1/rooms/tenant-a%3Aroom-1/users/user-1/unknown", "")
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected unsupported room user subresource 400, got %d", resp.Code)
	}
	store.listInboxNotificationsErr = errors.New("list failed")
	resp = performAdminRequest(handler, token, http.MethodGet, "/v1/users/user-1/inbox-notifications", "")
	if resp.Code != http.StatusInternalServerError {
		t.Fatalf("expected list notification failure 500, got %d", resp.Code)
	}
	store.listInboxNotificationsErr = nil
	store.deleteAllInboxNotificationsErr = errors.New("delete all failed")
	resp = performAdminRequest(handler, token, http.MethodDelete, "/v1/users/user-1/inbox-notifications", "")
	if resp.Code != http.StatusInternalServerError {
		t.Fatalf("expected delete all notification failure 500, got %d", resp.Code)
	}
	store.deleteAllInboxNotificationsErr = nil
	store.deleteInboxNotificationErr = cluster.ErrInboxNotFound
	resp = performAdminRequest(handler, token, http.MethodDelete, "/v1/users/user-1/inbox-notifications/in_1", "")
	if resp.Code != http.StatusNotFound {
		t.Fatalf("expected delete missing notification 404, got %d", resp.Code)
	}
	store.deleteInboxNotificationErr = errors.New("delete failed")
	resp = performAdminRequest(handler, token, http.MethodDelete, "/v1/users/user-1/inbox-notifications/in_1", "")
	if resp.Code != http.StatusInternalServerError {
		t.Fatalf("expected delete notification failure 500, got %d", resp.Code)
	}
	store.deleteInboxNotificationErr = nil
	store.markInboxNotificationErr = errors.New("mark failed")
	resp = performAdminRequest(handler, token, http.MethodPost, "/v1/inbox-notifications/in_1/read", "")
	if resp.Code != http.StatusInternalServerError {
		t.Fatalf("expected mark notification failure 500, got %d", resp.Code)
	}
	store.markInboxNotificationErr = nil
	store.createInboxNotificationErr = errors.New("create failed")
	resp = performAdminRequest(handler, token, http.MethodPost, "/v1/inbox-notifications/trigger", `{"id":"in_1","userId":"user-1","kind":"thread"}`)
	if resp.Code != http.StatusInternalServerError {
		t.Fatalf("expected create notification failure 500, got %d", resp.Code)
	}
	store.createInboxNotificationErr = nil
	store.getNotificationSettingsErr = errors.New("settings failed")
	resp = performAdminRequest(handler, token, http.MethodGet, "/v1/users/user-1/notification-settings", "")
	if resp.Code != http.StatusInternalServerError {
		t.Fatalf("expected get settings failure 500, got %d", resp.Code)
	}
	store.getNotificationSettingsErr = nil
	store.setNotificationSettingsErr = errors.New("set settings failed")
	resp = performAdminRequest(handler, token, http.MethodPost, "/v1/users/user-1/notification-settings", `{}`)
	if resp.Code != http.StatusInternalServerError {
		t.Fatalf("expected set settings failure 500, got %d", resp.Code)
	}
	store.setNotificationSettingsErr = nil
	store.deleteNotificationSettingsErr = errors.New("delete settings failed")
	resp = performAdminRequest(handler, token, http.MethodDelete, "/v1/users/user-1/notification-settings", "")
	if resp.Code != http.StatusInternalServerError {
		t.Fatalf("expected delete settings failure 500, got %d", resp.Code)
	}
	store.deleteNotificationSettingsErr = nil
	store.getRoomSubscriptionSettingsErr = errors.New("room settings failed")
	resp = performAdminRequest(handler, token, http.MethodGet, "/v1/rooms/tenant-a%3Aroom-1/users/user-1/subscription-settings", "")
	if resp.Code != http.StatusInternalServerError {
		t.Fatalf("expected get room settings failure 500, got %d", resp.Code)
	}
	store.getRoomSubscriptionSettingsErr = nil
	store.setRoomSubscriptionSettingsErr = errors.New("set room settings failed")
	resp = performAdminRequest(handler, token, http.MethodPost, "/v1/rooms/tenant-a%3Aroom-1/users/user-1/subscription-settings", `{}`)
	if resp.Code != http.StatusInternalServerError {
		t.Fatalf("expected set room settings failure 500, got %d", resp.Code)
	}
	store.setRoomSubscriptionSettingsErr = nil
	store.deleteRoomSubscriptionSettingsErr = errors.New("delete room settings failed")
	resp = performAdminRequest(handler, token, http.MethodDelete, "/v1/rooms/tenant-a%3Aroom-1/users/user-1/subscription-settings", "")
	if resp.Code != http.StatusInternalServerError {
		t.Fatalf("expected delete room settings failure 500, got %d", resp.Code)
	}
	store.deleteRoomSubscriptionSettingsErr = nil
	store.listRoomSubscriptionSettingsErr = errors.New("list room settings failed")
	resp = performAdminRequest(handler, token, http.MethodGet, "/v1/users/user-1/room-subscription-settings", "")
	if resp.Code != http.StatusInternalServerError {
		t.Fatalf("expected list room settings failure 500, got %d", resp.Code)
	}
	store.listRoomSubscriptionSettingsErr = nil
	resp = performAdminRequest(handler, token, http.MethodGet, "/v1/users/user-1/room-subscription-settings?limit=99", "")
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid room settings limit 400, got %d", resp.Code)
	}
	resp = performAdminRequest(handler, token, http.MethodGet, "/v1/users/user-1/room-subscription-settings?cursor=bad", "")
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid room settings cursor 400, got %d", resp.Code)
	}
	resp = performAdminRequest(handler, token, http.MethodPatch, "/v1/users/user-1/notification-settings", "")
	if resp.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected notification settings method 405, got %d", resp.Code)
	}
	resp = performAdminRequest(handler, token, http.MethodPatch, "/v1/users/user-1/inbox-notifications", "")
	if resp.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected inbox collection method 405, got %d", resp.Code)
	}
	resp = performAdminRequest(handler, token, http.MethodPatch, "/v1/users/user-1/inbox-notifications/in_1", "")
	if resp.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected inbox item method 405, got %d", resp.Code)
	}
	resp = performAdminRequest(handler, token, http.MethodGet, "/v1/inbox-notifications/trigger", "")
	if resp.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected trigger method 405, got %d", resp.Code)
	}
	resp = performAdminRequest(handler, token, http.MethodGet, "/v1/inbox-notifications/in_1/read", "")
	if resp.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected read action method 405, got %d", resp.Code)
	}
	resp = performAdminRequest(handler, token, http.MethodPatch, "/v1/rooms/tenant-a%3Aroom-1/users/user-1/subscription-settings", "")
	if resp.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected room subscription settings method 405, got %d", resp.Code)
	}
	resp = performAdminRequest(handler, token, http.MethodPost, "/v1/users/user-1/room-subscription-settings", "")
	if resp.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected room subscription list method 405, got %d", resp.Code)
	}
	resp = performAdminRequest(handler, token, http.MethodGet, "/v1/users/user-1/unknown", "")
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected unsupported user subresource 400, got %d", resp.Code)
	}
	resp = performAdminRequest(handler, token, http.MethodGet, "/v1/users/user-1/inbox-notifications/in_1/extra", "")
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected unsupported user path 400, got %d", resp.Code)
	}

	forbiddenVerifier, forbiddenToken, forbiddenCleanup := newAdminTestVerifier(t, map[string]any{
		"tenant": "tenant-a",
		"scope":  "notifications:user-2",
	})
	defer forbiddenCleanup()
	forbiddenHandler := newTestAdminService(forbiddenVerifier, &fakeAdminStore{}).Handler()
	resp = performAdminRequest(forbiddenHandler, forbiddenToken, http.MethodGet, "/v1/users/user-1/inbox-notifications", "")
	if resp.Code != http.StatusForbidden {
		t.Fatalf("expected notification list forbidden 403, got %d", resp.Code)
	}
}

func TestAdminNotificationAuthAndStoreBranches(t *testing.T) {
	verifier, token, cleanup := newAdminTestVerifier(t, map[string]any{
		"tenant": "tenant-a",
		"scope":  "notifications:user-1",
	})
	defer cleanup()

	store := &fakeAdminStore{
		getInboxNotificationRecord: cluster.InboxNotificationRecord{
			ID:     "in_1",
			UserID: "user-1",
			Kind:   "thread",
		},
	}
	handler := newTestAdminService(verifier, store).Handler()
	noAuth := httptest.NewRequest(http.MethodPost, "/v1/inbox-notifications/trigger", strings.NewReader(`{}`))
	noAuthResp := httptest.NewRecorder()
	handler.ServeHTTP(noAuthResp, noAuth)
	if noAuthResp.Code != http.StatusUnauthorized {
		t.Fatalf("expected trigger without auth 401, got %d", noAuthResp.Code)
	}
	noAuth = httptest.NewRequest(http.MethodGet, "/v1/users/user-1/inbox-notifications", nil)
	noAuthResp = httptest.NewRecorder()
	handler.ServeHTTP(noAuthResp, noAuth)
	if noAuthResp.Code != http.StatusUnauthorized {
		t.Fatalf("expected inbox list without auth 401, got %d", noAuthResp.Code)
	}
	noAuth = httptest.NewRequest(http.MethodPost, "/v1/inbox-notifications/in_1/read", nil)
	noAuthResp = httptest.NewRecorder()
	handler.ServeHTTP(noAuthResp, noAuth)
	if noAuthResp.Code != http.StatusUnauthorized {
		t.Fatalf("expected read without auth 401, got %d", noAuthResp.Code)
	}
	noAuth = httptest.NewRequest(http.MethodGet, "/v1/users/user-1/notification-settings", nil)
	noAuthResp = httptest.NewRecorder()
	handler.ServeHTTP(noAuthResp, noAuth)
	if noAuthResp.Code != http.StatusUnauthorized {
		t.Fatalf("expected settings without auth 401, got %d", noAuthResp.Code)
	}
	noAuth = httptest.NewRequest(http.MethodGet, "/v1/rooms/tenant-a%3Aroom-1/users/user-1/subscription-settings", nil)
	noAuthResp = httptest.NewRecorder()
	handler.ServeHTTP(noAuthResp, noAuth)
	if noAuthResp.Code != http.StatusUnauthorized {
		t.Fatalf("expected room settings without auth 401, got %d", noAuthResp.Code)
	}
	noAuth = httptest.NewRequest(http.MethodGet, "/v1/users/user-1/room-subscription-settings", nil)
	noAuthResp = httptest.NewRecorder()
	handler.ServeHTTP(noAuthResp, noAuth)
	if noAuthResp.Code != http.StatusUnauthorized {
		t.Fatalf("expected room settings list without auth 401, got %d", noAuthResp.Code)
	}

	noStoreHandler := newTestAdminService(verifier, nil).Handler()
	noStoreCases := []struct {
		method string
		path   string
		body   string
	}{
		{method: http.MethodGet, path: "/v1/users/user-1/inbox-notifications"},
		{method: http.MethodGet, path: "/v1/users/user-1/notification-settings"},
		{method: http.MethodGet, path: "/v1/rooms/tenant-a%3Aroom-1/users/user-1/subscription-settings"},
		{method: http.MethodGet, path: "/v1/users/user-1/room-subscription-settings"},
		{method: http.MethodPost, path: "/v1/inbox-notifications/in_1/read"},
	}
	for _, tc := range noStoreCases {
		resp := performAdminRequest(noStoreHandler, token, tc.method, tc.path, tc.body)
		if resp.Code != http.StatusServiceUnavailable {
			t.Fatalf("expected no-store %s %s to return 503, got %d", tc.method, tc.path, resp.Code)
		}
	}

	forbiddenVerifier, forbiddenToken, forbiddenCleanup := newAdminTestVerifier(t, map[string]any{
		"tenant": "tenant-a",
		"scope":  "notifications:user-2",
	})
	defer forbiddenCleanup()
	forbiddenHandler := newTestAdminService(forbiddenVerifier, store).Handler()
	forbiddenCases := []struct {
		method string
		path   string
		body   string
	}{
		{method: http.MethodPost, path: "/v1/inbox-notifications/trigger", body: `{"id":"in_1","userId":"user-1","kind":"thread"}`},
		{method: http.MethodPost, path: "/v1/inbox-notifications/in_1/read"},
		{method: http.MethodGet, path: "/v1/users/user-1/notification-settings"},
		{method: http.MethodGet, path: "/v1/rooms/tenant-a%3Aroom-1/users/user-1/subscription-settings"},
		{method: http.MethodGet, path: "/v1/users/user-1/room-subscription-settings"},
	}
	for _, tc := range forbiddenCases {
		resp := performAdminRequest(forbiddenHandler, forbiddenToken, tc.method, tc.path, tc.body)
		if resp.Code != http.StatusForbidden {
			t.Fatalf("expected forbidden %s %s to return 403, got %d", tc.method, tc.path, resp.Code)
		}
	}

	resp := performAdminRequest(handler, token, http.MethodGet, "/v1/users/user-1/notification-settings/extra", "")
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected notification settings extra path 400, got %d", resp.Code)
	}
	resp = performAdminRequest(handler, token, http.MethodGet, "/v1/users/user-1/room-subscription-settings/extra", "")
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected room subscription settings extra path 400, got %d", resp.Code)
	}
	resp = performAdminRequest(handler, token, http.MethodGet, "/v1/rooms/tenant-a%3Aroom-1/users/bad%20user/subscription-settings", "")
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected bad room user path 400, got %d", resp.Code)
	}
}

func TestAdminRoomAndStorageHandlers(t *testing.T) {
	verifier, token, cleanup := newAdminTestVerifier(t, map[string]any{
		"tenant": "tenant-a",
		"scope":  "rooms:tenant-a:* storage:tenant-a:*",
	})
	defer cleanup()

	now := time.Unix(100, 0).UTC()
	room := cluster.RoomRecord{
		ID:              "tenant-a:room-1",
		Metadata:        json.RawMessage(`{"title":"Draft"}`),
		DefaultAccesses: []string{cluster.PermissionRoomRead},
		UsersAccesses: map[string][]string{
			"user-1": {cluster.PermissionRoomWrite},
		},
		GroupsAccesses: map[string][]string{
			"team-1": {cluster.PermissionRoomPresenceWrite},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	updatedRoom := room
	updatedRoom.Metadata = json.RawMessage(`{"title":"Published"}`)
	store := &fakeAdminStore{
		getRoomRecord:    room,
		updateRoomRecord: updatedRoom,
		deleteRoomFound:  true,
		listRooms: cluster.RoomList{
			Rooms:      []cluster.RoomRecord{room},
			NextCursor: 42,
		},
		getStorageDoc:   json.RawMessage(`{"title":"Draft"}`),
		setStorageDoc:   json.RawMessage(`{"title":"Stored"}`),
		applyStorageDoc: json.RawMessage(`{"title":"Patched"}`),
		deleteStorageOK: true,
	}
	handler := newTestAdminService(verifier, store).Handler()

	listResp := performAdminRequest(handler, token, http.MethodGet, "/v1/rooms?prefix=tenant-a%3A&limit=10&cursor=3", "")
	if listResp.Code != http.StatusOK {
		t.Fatalf("expected list rooms 200, got %d", listResp.Code)
	}
	var list roomListResponse
	if err := json.NewDecoder(listResp.Body).Decode(&list); err != nil {
		t.Fatalf("decode list rooms response: %v", err)
	}
	if len(list.Rooms) != 1 || list.Rooms[0].ID != "tenant-a:room-1" || list.NextCursor != "42" {
		t.Fatalf("unexpected list response: %+v", list)
	}
	if store.listRoomsPrefix != "tenant-a:" || store.listRoomsCursor != 3 || store.listRoomsLimit != 10 {
		t.Fatalf("unexpected list parameters: prefix=%q cursor=%d limit=%d", store.listRoomsPrefix, store.listRoomsCursor, store.listRoomsLimit)
	}
	store.listRooms = cluster.RoomList{
		Rooms: []cluster.RoomRecord{
			room,
			{ID: "tenant-a:room-2", Metadata: json.RawMessage(`{"title":"Archived","status":"archived"}`)},
		},
	}
	queryResp := performAdminRequest(handler, token, http.MethodGet, "/v1/rooms?prefix=tenant-a%3A&query=metadata.title%3A%22Draft%22", "")
	if queryResp.Code != http.StatusOK {
		t.Fatalf("expected query rooms 200, got %d body=%q", queryResp.Code, queryResp.Body.String())
	}
	var queryList roomListResponse
	if err := json.NewDecoder(queryResp.Body).Decode(&queryList); err != nil {
		t.Fatalf("decode query rooms response: %v", err)
	}
	if len(queryList.Rooms) != 1 || queryList.Rooms[0].ID != "tenant-a:room-1" {
		t.Fatalf("unexpected query list response: %+v", queryList)
	}

	getRoomResp := performAdminRequest(handler, token, http.MethodGet, "/v1/rooms/tenant-a%3Aroom-1", "")
	if getRoomResp.Code != http.StatusOK {
		t.Fatalf("expected get room 200, got %d", getRoomResp.Code)
	}
	var gotRoom cluster.RoomRecord
	if err := json.NewDecoder(getRoomResp.Body).Decode(&gotRoom); err != nil {
		t.Fatalf("decode get room response: %v", err)
	}
	if gotRoom.ID != "tenant-a:room-1" || string(gotRoom.Metadata) != `{"title":"Draft"}` {
		t.Fatalf("unexpected get room response: %+v", gotRoom)
	}

	updateBody := `{"metadata":{"title":"Published"},"defaultAccesses":["room:read"],"usersAccesses":{"user-1":["room:write"]},"groupsAccesses":{"team-1":["room:presence:write"]}}`
	updateRoomResp := performAdminRequest(handler, token, http.MethodPatch, "/v1/rooms/tenant-a%3Aroom-1", updateBody)
	if updateRoomResp.Code != http.StatusOK {
		t.Fatalf("expected update room 200, got %d", updateRoomResp.Code)
	}
	if store.updatedRoom != "tenant-a:room-1" || !store.roomUpdate.MetadataSet || !store.roomUpdate.DefaultAccessesSet || !store.roomUpdate.UsersAccessesSet || !store.roomUpdate.GroupsAccessesSet {
		t.Fatalf("unexpected room update capture: room=%q update=%+v", store.updatedRoom, store.roomUpdate)
	}
	var gotUpdatedRoom cluster.RoomRecord
	if err := json.NewDecoder(updateRoomResp.Body).Decode(&gotUpdatedRoom); err != nil {
		t.Fatalf("decode update room response: %v", err)
	}
	if string(gotUpdatedRoom.Metadata) != `{"title":"Published"}` {
		t.Fatalf("unexpected update room response: %+v", gotUpdatedRoom)
	}

	deleteRoomResp := performAdminRequest(handler, token, http.MethodDelete, "/v1/rooms/tenant-a%3Aroom-1", "")
	if deleteRoomResp.Code != http.StatusNoContent {
		t.Fatalf("expected delete room 204, got %d", deleteRoomResp.Code)
	}

	getStorageResp := performAdminRequest(handler, token, http.MethodGet, "/v1/rooms/tenant-a%3Aroom-1/storage", "")
	if getStorageResp.Code != http.StatusOK || strings.TrimSpace(getStorageResp.Body.String()) != `{"title":"Draft"}` {
		t.Fatalf("unexpected get storage response: %d %q", getStorageResp.Code, getStorageResp.Body.String())
	}

	setStorageResp := performAdminRequest(handler, token, http.MethodPut, "/v1/rooms/tenant-a%3Aroom-1/storage", `{"title":"Stored"}`)
	if setStorageResp.Code != http.StatusOK || strings.TrimSpace(setStorageResp.Body.String()) != `{"title":"Stored"}` {
		t.Fatalf("unexpected set storage response: %d %q", setStorageResp.Code, setStorageResp.Body.String())
	}
	if string(store.setStorageInput) != `{"title":"Stored"}` {
		t.Fatalf("unexpected stored storage input: %s", store.setStorageInput)
	}

	patchStorageResp := performAdminRequest(handler, token, http.MethodPatch, "/v1/rooms/tenant-a%3Aroom-1/storage/json-patch", `[{"op":"replace","path":"/title","value":"Patched"}]`)
	if patchStorageResp.Code != http.StatusOK || strings.TrimSpace(patchStorageResp.Body.String()) != `{"title":"Patched"}` {
		t.Fatalf("unexpected patch storage response: %d %q", patchStorageResp.Code, patchStorageResp.Body.String())
	}
	if len(store.storagePatchOperations) != 1 || store.storagePatchOperations[0].Op != "replace" {
		t.Fatalf("unexpected storage patch operations: %+v", store.storagePatchOperations)
	}

	deleteStorageResp := performAdminRequest(handler, token, http.MethodDelete, "/v1/rooms/tenant-a%3Aroom-1/storage", "")
	if deleteStorageResp.Code != http.StatusNoContent {
		t.Fatalf("expected delete storage 204, got %d", deleteStorageResp.Code)
	}

	readOnlyVerifier, readOnlyToken, readOnlyCleanup := newAdminTestVerifier(t, map[string]any{
		"tenant": "tenant-a",
		"scope":  "storage:read:tenant-a:* comments:read:tenant-a:*",
	})
	defer readOnlyCleanup()
	readOnlyHandler := newTestAdminService(readOnlyVerifier, store).Handler()
	readOnlyStorageResp := performAdminRequest(readOnlyHandler, readOnlyToken, http.MethodGet, "/v1/rooms/tenant-a%3Aroom-1/storage", "")
	if readOnlyStorageResp.Code != http.StatusOK {
		t.Fatalf("expected read-only storage get 200, got %d", readOnlyStorageResp.Code)
	}
	readOnlyStorageWriteResp := performAdminRequest(readOnlyHandler, readOnlyToken, http.MethodPut, "/v1/rooms/tenant-a%3Aroom-1/storage", `{"title":"Denied"}`)
	if readOnlyStorageWriteResp.Code != http.StatusForbidden {
		t.Fatalf("expected read-only storage write 403, got %d", readOnlyStorageWriteResp.Code)
	}
	readOnlyThreadsResp := performAdminRequest(readOnlyHandler, readOnlyToken, http.MethodGet, "/v1/rooms/tenant-a%3Aroom-1/threads", "")
	if readOnlyThreadsResp.Code != http.StatusOK {
		t.Fatalf("expected read-only threads list 200, got %d", readOnlyThreadsResp.Code)
	}
	readOnlyThreadWriteResp := performAdminRequest(readOnlyHandler, readOnlyToken, http.MethodPost, "/v1/rooms/tenant-a%3Aroom-1/threads", `{"comment":{"userId":"user-1","body":{}}}`)
	if readOnlyThreadWriteResp.Code != http.StatusForbidden {
		t.Fatalf("expected read-only thread create 403, got %d", readOnlyThreadWriteResp.Code)
	}
}

func TestAdminRoomAndStorageErrorBranches(t *testing.T) {
	verifier, token, cleanup := newAdminTestVerifier(t, map[string]any{
		"tenant": "tenant-a",
		"scope":  "rooms:tenant-a:* storage:tenant-a:*",
	})
	defer cleanup()

	store := &fakeAdminStore{}
	handler := newTestAdminService(verifier, store).Handler()

	noAuthCases := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{name: "create room", method: http.MethodPost, path: "/v1/rooms", body: `{"id":"tenant-a:room-1","metadata":{}}`},
		{name: "list rooms", method: http.MethodGet, path: "/v1/rooms"},
		{name: "get room", method: http.MethodGet, path: "/v1/rooms/tenant-a%3Aroom-1"},
		{name: "update room", method: http.MethodPatch, path: "/v1/rooms/tenant-a%3Aroom-1", body: `{"metadata":{}}`},
		{name: "delete room", method: http.MethodDelete, path: "/v1/rooms/tenant-a%3Aroom-1"},
		{name: "active users", method: http.MethodGet, path: "/v1/rooms/tenant-a%3Aroom-1/active_users"},
		{name: "get storage", method: http.MethodGet, path: "/v1/rooms/tenant-a%3Aroom-1/storage"},
		{name: "set storage", method: http.MethodPut, path: "/v1/rooms/tenant-a%3Aroom-1/storage", body: `{}`},
		{name: "delete storage", method: http.MethodDelete, path: "/v1/rooms/tenant-a%3Aroom-1/storage"},
		{name: "patch storage", method: http.MethodPatch, path: "/v1/rooms/tenant-a%3Aroom-1/storage/json-patch", body: `[{"op":"test","path":"","value":{}}]`},
	}
	for _, tc := range noAuthCases {
		t.Run("no auth "+tc.name, func(t *testing.T) {
			request := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusUnauthorized {
				t.Fatalf("expected no-auth %s to return 401, got %d", tc.name, response.Code)
			}
		})
	}

	store.listRoomsErr = errors.New("list failed")
	resp := performAdminRequest(handler, token, http.MethodGet, "/v1/rooms", "")
	if resp.Code != http.StatusInternalServerError {
		t.Fatalf("expected list rooms failure 500, got %d", resp.Code)
	}
	store.listRoomsErr = nil
	resp = performAdminRequest(handler, token, http.MethodGet, "/v1/rooms?limit=bad", "")
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid room list limit 400, got %d", resp.Code)
	}
	resp = performAdminRequest(handler, token, http.MethodGet, "/v1/rooms?cursor=bad", "")
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid room list cursor 400, got %d", resp.Code)
	}
	resp = performAdminRequest(handler, token, http.MethodGet, "/v1/rooms?query=metadata.bad%2Fkey%3Avalue", "")
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid room query 400, got %d", resp.Code)
	}
	roomReq := httptest.NewRequest(http.MethodGet, "/v1/rooms/tenant-a%3Aroom-1", nil)
	roomReq.URL.Path = "/v1/rooms/%zz"
	roomReq.Header.Set("Authorization", "Bearer "+token)
	roomResp := httptest.NewRecorder()
	newTestAdminService(verifier, store).handleRoom(roomResp, roomReq)
	if roomResp.Code != http.StatusBadRequest {
		t.Fatalf("expected malformed room path 400, got %d", roomResp.Code)
	}

	resp = performAdminRequest(handler, token, http.MethodPost, "/v1/rooms", `{`)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected malformed create room body 400, got %d", resp.Code)
	}

	resp = performAdminRequest(handler, token, http.MethodGet, "/v1/rooms/tenant-a%3Aroom-1", "")
	if resp.Code != http.StatusNotFound {
		t.Fatalf("expected get missing room 404, got %d", resp.Code)
	}
	store.getRoomErr = errors.New("get failed")
	resp = performAdminRequest(handler, token, http.MethodGet, "/v1/rooms/tenant-a%3Aroom-1", "")
	if resp.Code != http.StatusInternalServerError {
		t.Fatalf("expected get room failure 500, got %d", resp.Code)
	}
	store.getRoomErr = nil

	updateBody := `{"metadata":{"title":"Published"}}`
	resp = performAdminRequest(handler, token, http.MethodPatch, "/v1/rooms/tenant-a%3Aroom-1", `{`)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected malformed room update body 400, got %d", resp.Code)
	}
	resp = performAdminRequest(handler, token, http.MethodPatch, "/v1/rooms/tenant-a%3Aroom-1", `{"metadata":[]}`)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid room update metadata 400, got %d", resp.Code)
	}
	resp = performAdminRequest(handler, token, http.MethodPatch, "/v1/rooms/tenant-a%3Aroom-1", `{"defaultAccesses":["room:delete"]}`)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid room update access 400, got %d", resp.Code)
	}
	resp = performAdminRequest(handler, token, http.MethodPatch, "/v1/rooms/tenant-a%3Aroom-1", updateBody)
	if resp.Code != http.StatusNotFound {
		t.Fatalf("expected update missing room 404, got %d", resp.Code)
	}
	store.updateRoomErr = errors.New("update failed")
	resp = performAdminRequest(handler, token, http.MethodPatch, "/v1/rooms/tenant-a%3Aroom-1", updateBody)
	if resp.Code != http.StatusInternalServerError {
		t.Fatalf("expected update room failure 500, got %d", resp.Code)
	}
	store.updateRoomErr = nil

	resp = performAdminRequest(handler, token, http.MethodDelete, "/v1/rooms/tenant-a%3Aroom-1", "")
	if resp.Code != http.StatusNotFound {
		t.Fatalf("expected delete missing room 404, got %d", resp.Code)
	}
	store.deleteRoomErr = errors.New("delete failed")
	resp = performAdminRequest(handler, token, http.MethodDelete, "/v1/rooms/tenant-a%3Aroom-1", "")
	if resp.Code != http.StatusInternalServerError {
		t.Fatalf("expected delete room failure 500, got %d", resp.Code)
	}
	store.deleteRoomErr = nil

	resp = performAdminRequest(handler, token, http.MethodGet, "/v1/rooms/tenant-a%3Aroom-1/storage", "")
	if resp.Code != http.StatusNotFound {
		t.Fatalf("expected get missing storage 404, got %d", resp.Code)
	}
	store.getStorageErr = errors.New("get storage failed")
	resp = performAdminRequest(handler, token, http.MethodGet, "/v1/rooms/tenant-a%3Aroom-1/storage", "")
	if resp.Code != http.StatusInternalServerError {
		t.Fatalf("expected get storage failure 500, got %d", resp.Code)
	}
	store.getStorageErr = nil

	resp = performAdminRequest(handler, token, http.MethodPut, "/v1/rooms/tenant-a%3Aroom-1/storage", `{`)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected malformed storage body 400, got %d", resp.Code)
	}

	store.setStorageErr = errors.New("set storage failed")
	resp = performAdminRequest(handler, token, http.MethodPut, "/v1/rooms/tenant-a%3Aroom-1/storage", `{"title":"Stored"}`)
	if resp.Code != http.StatusInternalServerError {
		t.Fatalf("expected set storage failure 500, got %d", resp.Code)
	}
	store.setStorageErr = nil

	resp = performAdminRequest(handler, token, http.MethodPatch, "/v1/rooms/tenant-a%3Aroom-1/storage/json-patch", `{`)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected malformed storage patch body 400, got %d", resp.Code)
	}

	resp = performAdminRequest(handler, token, http.MethodPatch, "/v1/rooms/tenant-a%3Aroom-1/storage/json-patch", `[{"op":"replace","path":"/title","value":"Patched"}]`)
	if resp.Code != http.StatusNotFound {
		t.Fatalf("expected patch missing storage 404, got %d", resp.Code)
	}
	store.applyStorageErr = cluster.ErrStoragePatch
	resp = performAdminRequest(handler, token, http.MethodPatch, "/v1/rooms/tenant-a%3Aroom-1/storage/json-patch", `[{"op":"replace","path":"/title","value":"Patched"}]`)
	if resp.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected storage patch failure 422, got %d", resp.Code)
	}
	store.applyStorageErr = errors.New("patch storage failed")
	resp = performAdminRequest(handler, token, http.MethodPatch, "/v1/rooms/tenant-a%3Aroom-1/storage/json-patch", `[{"op":"replace","path":"/title","value":"Patched"}]`)
	if resp.Code != http.StatusInternalServerError {
		t.Fatalf("expected storage patch internal failure 500, got %d", resp.Code)
	}
	store.applyStorageErr = nil

	resp = performAdminRequest(handler, token, http.MethodDelete, "/v1/rooms/tenant-a%3Aroom-1/storage", "")
	if resp.Code != http.StatusNotFound {
		t.Fatalf("expected delete missing storage 404, got %d", resp.Code)
	}
	store.deleteStorageErr = errors.New("delete storage failed")
	resp = performAdminRequest(handler, token, http.MethodDelete, "/v1/rooms/tenant-a%3Aroom-1/storage", "")
	if resp.Code != http.StatusInternalServerError {
		t.Fatalf("expected delete storage failure 500, got %d", resp.Code)
	}
}

func TestAdminRoomAndStorageAuthorizationBranches(t *testing.T) {
	verifier, token, cleanup := newAdminTestVerifier(t, map[string]any{
		"tenant": "tenant-a",
		"scope":  "rooms:tenant-a:* storage:tenant-a:*",
	})
	defer cleanup()

	noStoreHandler := newTestAdminService(verifier, nil).Handler()
	noStoreCases := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{name: "create room", method: http.MethodPost, path: "/v1/rooms", body: `{"id":"tenant-a:room-1","metadata":{}}`},
		{name: "list rooms", method: http.MethodGet, path: "/v1/rooms"},
		{name: "get room", method: http.MethodGet, path: "/v1/rooms/tenant-a%3Aroom-1"},
		{name: "update room", method: http.MethodPatch, path: "/v1/rooms/tenant-a%3Aroom-1", body: `{"metadata":{}}`},
		{name: "delete room", method: http.MethodDelete, path: "/v1/rooms/tenant-a%3Aroom-1"},
		{name: "get storage", method: http.MethodGet, path: "/v1/rooms/tenant-a%3Aroom-1/storage"},
		{name: "set storage", method: http.MethodPut, path: "/v1/rooms/tenant-a%3Aroom-1/storage", body: `{}`},
		{name: "delete storage", method: http.MethodDelete, path: "/v1/rooms/tenant-a%3Aroom-1/storage"},
		{name: "patch storage", method: http.MethodPatch, path: "/v1/rooms/tenant-a%3Aroom-1/storage/json-patch", body: `[{"op":"test","path":"","value":{}}]`},
	}
	for _, tc := range noStoreCases {
		t.Run("no store "+tc.name, func(t *testing.T) {
			resp := performAdminRequest(noStoreHandler, token, tc.method, tc.path, tc.body)
			if resp.Code != http.StatusServiceUnavailable {
				t.Fatalf("expected no-store %s to return 503, got %d", tc.name, resp.Code)
			}
		})
	}

	forbiddenVerifier, forbiddenToken, forbiddenCleanup := newAdminTestVerifier(t, map[string]any{
		"tenant": "tenant-a",
		"scope":  "rooms:tenant-b:* storage:tenant-b:*",
	})
	defer forbiddenCleanup()
	forbiddenHandler := newTestAdminService(forbiddenVerifier, &fakeAdminStore{}).Handler()
	forbiddenCases := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{name: "create room", method: http.MethodPost, path: "/v1/rooms", body: `{"id":"tenant-a:room-1","metadata":{}}`},
		{name: "list rooms", method: http.MethodGet, path: "/v1/rooms?prefix=tenant-a%3A"},
		{name: "get room", method: http.MethodGet, path: "/v1/rooms/tenant-a%3Aroom-1"},
		{name: "update room", method: http.MethodPatch, path: "/v1/rooms/tenant-a%3Aroom-1", body: `{"metadata":{}}`},
		{name: "delete room", method: http.MethodDelete, path: "/v1/rooms/tenant-a%3Aroom-1"},
		{name: "get storage", method: http.MethodGet, path: "/v1/rooms/tenant-a%3Aroom-1/storage"},
		{name: "set storage", method: http.MethodPut, path: "/v1/rooms/tenant-a%3Aroom-1/storage", body: `{}`},
		{name: "delete storage", method: http.MethodDelete, path: "/v1/rooms/tenant-a%3Aroom-1/storage"},
		{name: "patch storage", method: http.MethodPatch, path: "/v1/rooms/tenant-a%3Aroom-1/storage/json-patch", body: `[{"op":"test","path":"","value":{}}]`},
	}
	for _, tc := range forbiddenCases {
		t.Run("forbidden "+tc.name, func(t *testing.T) {
			resp := performAdminRequest(forbiddenHandler, forbiddenToken, tc.method, tc.path, tc.body)
			if resp.Code != http.StatusForbidden {
				t.Fatalf("expected forbidden %s to return 403, got %d body=%q", tc.name, resp.Code, resp.Body.String())
			}
		})
	}

	handler := newTestAdminService(verifier, &fakeAdminStore{}).Handler()
	methodCases := []struct {
		name   string
		method string
		path   string
	}{
		{name: "rooms", method: http.MethodPut, path: "/v1/rooms"},
		{name: "room", method: http.MethodPost, path: "/v1/rooms/tenant-a%3Aroom-1"},
		{name: "storage", method: http.MethodPost, path: "/v1/rooms/tenant-a%3Aroom-1/storage"},
		{name: "patch storage", method: http.MethodGet, path: "/v1/rooms/tenant-a%3Aroom-1/storage/json-patch"},
	}
	for _, tc := range methodCases {
		t.Run("method "+tc.name, func(t *testing.T) {
			resp := performAdminRequest(handler, token, tc.method, tc.path, "")
			if resp.Code != http.StatusMethodNotAllowed {
				t.Fatalf("expected %s method gate 405, got %d", tc.name, resp.Code)
			}
		})
	}

	resp := performAdminRequest(handler, token, http.MethodGet, "/v1/rooms/tenant-a%3Aroom-1/unknown", "")
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected unsupported subresource 400, got %d", resp.Code)
	}
	resp = performAdminRequest(handler, token, http.MethodGet, "/v1/rooms/tenant-a%2Froom-1", "")
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid room path 400, got %d", resp.Code)
	}
}

func performAdminRequest(handler http.Handler, token string, method string, path string, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func newTestAdminService(verifier *auth.Verifier, store cluster.Store) *Service {
	cfg := config.RuntimeConfig{
		Mode:   config.ModeCluster,
		NodeID: "node-a",
	}
	cfg.Tenant.EnforcePrefix = true
	cfg.Tenant.Separator = ":"
	cfg.Limits.PayloadMaxBytes = 256
	cfg.Limits.EnvelopeMaxBytes = 512
	return &Service{
		cfg:      cfg,
		verifier: verifier,
		store:    store,
		metrics:  observability.NewAdminMetrics(),
	}
}

func newAdminTestVerifier(t *testing.T, extra map[string]any) (*auth.Verifier, string, func()) {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	jwksServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"keys": []map[string]any{
				{
					"kty": "RSA",
					"kid": "admin-test-key",
					"n":   base64.RawURLEncoding.EncodeToString(privateKey.PublicKey.N.Bytes()),
					"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(privateKey.PublicKey.E)).Bytes()),
				},
			},
		})
	}))

	claims := jwt.MapClaims{
		"iss": "https://issuer.example.com",
		"aud": "openrtc-admin",
		"exp": time.Now().Add(time.Hour).Unix(),
		"sub": "admin-1",
	}
	for key, value := range extra {
		claims[key] = value
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = "admin-test-key"
	rawToken, err := token.SignedString(privateKey)
	if err != nil {
		jwksServer.Close()
		t.Fatalf("sign token: %v", err)
	}

	return auth.NewVerifier("https://issuer.example.com", "openrtc-admin", jwksServer.URL), rawToken, jwksServer.Close
}

type fakeAdminStore struct {
	healthyErr         error
	publishErr         error
	publishPresenceErr error
	setEphemeralErr    error
	aggregateStatsErr  error
	createRoomErr      error
	getRoomErr         error
	updateRoomErr      error
	deleteRoomErr      error
	listRoomsErr       error
	getStorageErr      error
	setStorageErr      error
	deleteStorageErr   error
	applyStorageErr    error

	stats              stats.Snapshot
	publishedEvents    []cluster.PublishedEvent
	presenceEvents     []cluster.PresenceEvent
	syncedStats        []stats.Snapshot
	createRoomRecord   cluster.RoomRecord
	getRoomRecord      cluster.RoomRecord
	updateRoomRecord   cluster.RoomRecord
	listRooms          cluster.RoomList
	createdThread      cluster.ThreadRecord
	createThreadRecord cluster.ThreadRecord
	createThreadErr    error
	listThreads        []cluster.ThreadRecord
	listThreadsErr     error
	addedComment       cluster.CommentRecord
	addCommentThread   cluster.ThreadRecord
	addCommentErr      error
	updatedComment     struct {
		Room      string
		ThreadID  string
		CommentID string
		Update    cluster.CommentUpdate
	}
	updateCommentThread               cluster.ThreadRecord
	updateCommentErr                  error
	createdInboxNotification          cluster.InboxNotificationRecord
	createInboxNotificationRecord     cluster.InboxNotificationRecord
	createInboxNotificationErr        error
	listInboxNotifications            cluster.InboxNotificationList
	listInboxNotificationsErr         error
	getInboxNotificationRecord        cluster.InboxNotificationRecord
	getInboxNotificationErr           error
	markInboxNotificationRecord       cluster.InboxNotificationRecord
	markInboxNotificationErr          error
	deleteInboxNotificationErr        error
	deleteAllInboxNotificationsErr    error
	notificationSettings              json.RawMessage
	setNotificationSettingsInput      json.RawMessage
	getNotificationSettingsErr        error
	setNotificationSettingsErr        error
	deleteNotificationSettingsErr     error
	roomSubscriptionSettings          cluster.RoomSubscriptionSettings
	setRoomSubscriptionSettingsInput  cluster.RoomSubscriptionSettings
	getRoomSubscriptionSettingsErr    error
	setRoomSubscriptionSettingsErr    error
	deleteRoomSubscriptionSettingsErr error
	listRoomSubscriptionSettings      cluster.RoomSubscriptionSettingsList
	listRoomSubscriptionSettingsErr   error
	createdRoom                       cluster.RoomRecord
	updatedRoom                       string
	roomUpdate                        cluster.RoomUpdate
	listRoomsPrefix                   string
	listRoomsCursor                   uint64
	listRoomsLimit                    int
	deleteRoomFound                   bool
	getStorageDoc                     json.RawMessage
	setStorageDoc                     json.RawMessage
	setStorageInput                   json.RawMessage
	deleteStorageOK                   bool
	applyStorageDoc                   json.RawMessage
	storagePatchOperations            []cluster.JSONPatchOperation
	activeUsers                       []cluster.ActiveUser
	activeUsersErr                    error
	ephemeralPresence                 struct {
		ConnID string
		Room   string
		State  json.RawMessage
		TTL    time.Duration
	}
}

func (s *fakeAdminStore) Healthy(context.Context) error {
	return s.healthyErr
}

func (s *fakeAdminStore) PublishEvent(_ context.Context, event cluster.PublishedEvent) error {
	if s.publishErr != nil {
		return s.publishErr
	}
	s.publishedEvents = append(s.publishedEvents, event)
	return nil
}

func (s *fakeAdminStore) Subscribe(context.Context, func(cluster.PublishedEvent)) error {
	return nil
}

func (s *fakeAdminStore) PublishPresence(_ context.Context, event cluster.PresenceEvent) error {
	if s.publishPresenceErr != nil {
		return s.publishPresenceErr
	}
	s.presenceEvents = append(s.presenceEvents, event)
	return nil
}

func (s *fakeAdminStore) SubscribePresence(context.Context, func(cluster.PresenceEvent)) error {
	return nil
}

func (s *fakeAdminStore) PublishYJSEvent(context.Context, cluster.YJSEvent) error {
	return nil
}

func (s *fakeAdminStore) SubscribeYJSEvents(context.Context, func(cluster.YJSEvent)) error {
	return nil
}

func (s *fakeAdminStore) TouchConnection(context.Context, string, cluster.ConnectionMeta) error {
	return nil
}

func (s *fakeAdminStore) JoinRoom(context.Context, string, string) error {
	return nil
}

func (s *fakeAdminStore) LeaveRoom(context.Context, string, string) error {
	return nil
}

func (s *fakeAdminStore) SetPresence(context.Context, string, string, json.RawMessage) error {
	return nil
}

func (s *fakeAdminStore) SetEphemeralPresence(_ context.Context, connID string, room string, payload json.RawMessage, ttl time.Duration) error {
	if s.setEphemeralErr != nil {
		return s.setEphemeralErr
	}
	s.ephemeralPresence.ConnID = connID
	s.ephemeralPresence.Room = room
	s.ephemeralPresence.State = payload
	s.ephemeralPresence.TTL = ttl
	return nil
}

func (s *fakeAdminStore) ClearPresence(context.Context, string, string) error {
	return nil
}

func (s *fakeAdminStore) SnapshotRoom(context.Context, string) (cluster.Snapshot, error) {
	return cluster.Snapshot{}, nil
}

func (s *fakeAdminStore) ActiveUsers(context.Context, string) ([]cluster.ActiveUser, error) {
	if s.activeUsersErr != nil {
		return nil, s.activeUsersErr
	}
	return s.activeUsers, nil
}

func (s *fakeAdminStore) CreateRoom(_ context.Context, room cluster.RoomRecord) (cluster.RoomRecord, error) {
	if s.createRoomErr != nil {
		return cluster.RoomRecord{}, s.createRoomErr
	}
	s.createdRoom = room
	if s.createRoomRecord.ID != "" {
		return s.createRoomRecord, nil
	}
	return room, nil
}

func (s *fakeAdminStore) GetRoom(context.Context, string) (cluster.RoomRecord, error) {
	if s.getRoomErr != nil {
		return cluster.RoomRecord{}, s.getRoomErr
	}
	if s.getRoomRecord.ID != "" {
		return s.getRoomRecord, nil
	}
	return cluster.RoomRecord{}, cluster.ErrRoomNotFound
}

func (s *fakeAdminStore) UpdateRoom(_ context.Context, room string, update cluster.RoomUpdate) (cluster.RoomRecord, error) {
	if s.updateRoomErr != nil {
		return cluster.RoomRecord{}, s.updateRoomErr
	}
	s.updatedRoom = room
	s.roomUpdate = update
	if s.updateRoomRecord.ID != "" {
		return s.updateRoomRecord, nil
	}
	return cluster.RoomRecord{}, cluster.ErrRoomNotFound
}

func (s *fakeAdminStore) DeleteRoom(context.Context, string) error {
	if s.deleteRoomErr != nil {
		return s.deleteRoomErr
	}
	if !s.deleteRoomFound {
		return cluster.ErrRoomNotFound
	}
	return nil
}

func (s *fakeAdminStore) ListRooms(_ context.Context, prefix string, cursor uint64, limit int) (cluster.RoomList, error) {
	if s.listRoomsErr != nil {
		return cluster.RoomList{}, s.listRoomsErr
	}
	s.listRoomsPrefix = prefix
	s.listRoomsCursor = cursor
	s.listRoomsLimit = limit
	return s.listRooms, nil
}

func (s *fakeAdminStore) CreateThread(_ context.Context, _ string, thread cluster.ThreadRecord) (cluster.ThreadRecord, error) {
	if s.createThreadErr != nil {
		return cluster.ThreadRecord{}, s.createThreadErr
	}
	s.createdThread = thread
	if s.createThreadRecord.ID != "" {
		return s.createThreadRecord, nil
	}
	return thread, nil
}

func (s *fakeAdminStore) ListThreads(context.Context, string) ([]cluster.ThreadRecord, error) {
	if s.listThreadsErr != nil {
		return nil, s.listThreadsErr
	}
	return s.listThreads, nil
}

func (s *fakeAdminStore) AddComment(_ context.Context, _ string, _ string, comment cluster.CommentRecord) (cluster.ThreadRecord, error) {
	if s.addCommentErr != nil {
		return cluster.ThreadRecord{}, s.addCommentErr
	}
	s.addedComment = comment
	if s.addCommentThread.ID != "" {
		return s.addCommentThread, nil
	}
	return cluster.ThreadRecord{}, cluster.ErrThreadNotFound
}

func (s *fakeAdminStore) UpdateComment(_ context.Context, room string, threadID string, commentID string, update cluster.CommentUpdate) (cluster.ThreadRecord, error) {
	if s.updateCommentErr != nil {
		return cluster.ThreadRecord{}, s.updateCommentErr
	}
	s.updatedComment.Room = room
	s.updatedComment.ThreadID = threadID
	s.updatedComment.CommentID = commentID
	s.updatedComment.Update = update
	if s.updateCommentThread.ID != "" {
		return s.updateCommentThread, nil
	}
	return cluster.ThreadRecord{}, cluster.ErrCommentNotFound
}

func (s *fakeAdminStore) CreateInboxNotification(_ context.Context, notification cluster.InboxNotificationRecord) (cluster.InboxNotificationRecord, error) {
	if s.createInboxNotificationErr != nil {
		return cluster.InboxNotificationRecord{}, s.createInboxNotificationErr
	}
	s.createdInboxNotification = notification
	if s.createInboxNotificationRecord.ID != "" {
		return s.createInboxNotificationRecord, nil
	}
	return notification, nil
}

func (s *fakeAdminStore) ListInboxNotifications(context.Context, string, cluster.InboxNotificationListFilter) (cluster.InboxNotificationList, error) {
	if s.listInboxNotificationsErr != nil {
		return cluster.InboxNotificationList{}, s.listInboxNotificationsErr
	}
	return s.listInboxNotifications, nil
}

func (s *fakeAdminStore) GetInboxNotification(_ context.Context, userID string, _ string) (cluster.InboxNotificationRecord, error) {
	if s.getInboxNotificationErr != nil {
		return cluster.InboxNotificationRecord{}, s.getInboxNotificationErr
	}
	if s.getInboxNotificationRecord.ID != "" && (userID == "" || s.getInboxNotificationRecord.UserID == userID) {
		return s.getInboxNotificationRecord, nil
	}
	return cluster.InboxNotificationRecord{}, cluster.ErrInboxNotFound
}

func (s *fakeAdminStore) MarkInboxNotificationRead(context.Context, string) (cluster.InboxNotificationRecord, error) {
	if s.markInboxNotificationErr != nil {
		return cluster.InboxNotificationRecord{}, s.markInboxNotificationErr
	}
	if s.markInboxNotificationRecord.ID != "" {
		return s.markInboxNotificationRecord, nil
	}
	return cluster.InboxNotificationRecord{}, cluster.ErrInboxNotFound
}

func (s *fakeAdminStore) DeleteInboxNotification(context.Context, string, string) error {
	if s.deleteInboxNotificationErr != nil {
		return s.deleteInboxNotificationErr
	}
	return nil
}

func (s *fakeAdminStore) DeleteAllInboxNotifications(context.Context, string) error {
	if s.deleteAllInboxNotificationsErr != nil {
		return s.deleteAllInboxNotificationsErr
	}
	return nil
}

func (s *fakeAdminStore) GetNotificationSettings(context.Context, string) (json.RawMessage, error) {
	if s.getNotificationSettingsErr != nil {
		return nil, s.getNotificationSettingsErr
	}
	if s.notificationSettings != nil {
		return s.notificationSettings, nil
	}
	return json.RawMessage(`{}`), nil
}

func (s *fakeAdminStore) SetNotificationSettings(_ context.Context, _ string, settings json.RawMessage) (json.RawMessage, error) {
	if s.setNotificationSettingsErr != nil {
		return nil, s.setNotificationSettingsErr
	}
	s.setNotificationSettingsInput = append(json.RawMessage(nil), settings...)
	return settings, nil
}

func (s *fakeAdminStore) DeleteNotificationSettings(context.Context, string) error {
	if s.deleteNotificationSettingsErr != nil {
		return s.deleteNotificationSettingsErr
	}
	return nil
}

func (s *fakeAdminStore) GetRoomSubscriptionSettings(context.Context, string, string) (cluster.RoomSubscriptionSettings, error) {
	if s.getRoomSubscriptionSettingsErr != nil {
		return cluster.RoomSubscriptionSettings{}, s.getRoomSubscriptionSettingsErr
	}
	return s.roomSubscriptionSettings, nil
}

func (s *fakeAdminStore) SetRoomSubscriptionSettings(_ context.Context, settings cluster.RoomSubscriptionSettings) (cluster.RoomSubscriptionSettings, error) {
	if s.setRoomSubscriptionSettingsErr != nil {
		return cluster.RoomSubscriptionSettings{}, s.setRoomSubscriptionSettingsErr
	}
	s.setRoomSubscriptionSettingsInput = settings
	if s.roomSubscriptionSettings.RoomID != "" {
		return s.roomSubscriptionSettings, nil
	}
	return settings, nil
}

func (s *fakeAdminStore) DeleteRoomSubscriptionSettings(context.Context, string, string) error {
	if s.deleteRoomSubscriptionSettingsErr != nil {
		return s.deleteRoomSubscriptionSettingsErr
	}
	return nil
}

func (s *fakeAdminStore) ListRoomSubscriptionSettings(context.Context, string, uint64, int) (cluster.RoomSubscriptionSettingsList, error) {
	if s.listRoomSubscriptionSettingsErr != nil {
		return cluster.RoomSubscriptionSettingsList{}, s.listRoomSubscriptionSettingsErr
	}
	return s.listRoomSubscriptionSettings, nil
}

func (s *fakeAdminStore) GetStorage(context.Context, string) (json.RawMessage, error) {
	if s.getStorageErr != nil {
		return nil, s.getStorageErr
	}
	if s.getStorageDoc != nil {
		return s.getStorageDoc, nil
	}
	return nil, cluster.ErrStorageNotFound
}

func (s *fakeAdminStore) SetStorage(_ context.Context, _ string, document json.RawMessage) (json.RawMessage, error) {
	if s.setStorageErr != nil {
		return nil, s.setStorageErr
	}
	s.setStorageInput = append(json.RawMessage(nil), document...)
	if s.setStorageDoc != nil {
		return s.setStorageDoc, nil
	}
	return document, nil
}

func (s *fakeAdminStore) DeleteStorage(context.Context, string) error {
	if s.deleteStorageErr != nil {
		return s.deleteStorageErr
	}
	if !s.deleteStorageOK {
		return cluster.ErrStorageNotFound
	}
	return nil
}

func (s *fakeAdminStore) ApplyStoragePatch(_ context.Context, _ string, operations []cluster.JSONPatchOperation, _ int) (json.RawMessage, error) {
	if s.applyStorageErr != nil {
		return nil, s.applyStorageErr
	}
	s.storagePatchOperations = append([]cluster.JSONPatchOperation(nil), operations...)
	if s.applyStorageDoc != nil {
		return s.applyStorageDoc, nil
	}
	return nil, cluster.ErrStorageNotFound
}

func (s *fakeAdminStore) LoadYJSDocument(context.Context, string) (cluster.YJSDocument, error) {
	return cluster.YJSDocument{}, nil
}

func (s *fakeAdminStore) AppendYJSUpdate(context.Context, string, cluster.YJSEventKind, []byte) (int64, error) {
	return 0, nil
}

func (s *fakeAdminStore) StoreYJSSnapshot(context.Context, string, []byte) error {
	return nil
}

func (s *fakeAdminStore) CleanupConnection(context.Context, string, string) error {
	return nil
}

func (s *fakeAdminStore) ReconcileNode(context.Context, string) error {
	return nil
}

func (s *fakeAdminStore) SyncStats(_ context.Context, _ string, snapshot stats.Snapshot) error {
	s.syncedStats = append(s.syncedStats, snapshot)
	return nil
}

func (s *fakeAdminStore) AggregateStats(context.Context) (stats.Snapshot, error) {
	if s.aggregateStatsErr != nil {
		return stats.Snapshot{}, s.aggregateStatsErr
	}
	return s.stats, nil
}

func (s *fakeAdminStore) Close() error {
	return nil
}
