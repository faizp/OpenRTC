package admin

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/openrtc/openrtc/server/internal/auth"
	"github.com/openrtc/openrtc/server/internal/cluster"
	"github.com/openrtc/openrtc/server/internal/config"
	openrtcerr "github.com/openrtc/openrtc/server/internal/errors"
	"github.com/openrtc/openrtc/server/internal/observability"
	"github.com/openrtc/openrtc/server/internal/protocol"
	"github.com/openrtc/openrtc/server/internal/roomengine"
	"github.com/openrtc/openrtc/server/internal/stats"
)

var randomRead = rand.Read

type Service struct {
	cfg      config.RuntimeConfig
	logger   *log.Logger
	verifier *auth.Verifier
	store    cluster.Store
	metrics  *observability.AdminMetrics

	webhookClient *http.Client

	mu    sync.Mutex
	stats stats.Snapshot
}

type PublishRequest struct {
	Room                string          `json:"room"`
	Event               string          `json:"event"`
	Payload             json.RawMessage `json:"payload"`
	ExcludeSenderConnID string          `json:"exclude_sender_conn_id,omitempty"`
	TraceID             string          `json:"trace_id,omitempty"`
}

type PresenceRequest struct {
	Room       string          `json:"room"`
	ConnID     string          `json:"conn_id"`
	State      json.RawMessage `json:"state"`
	TTLSeconds int             `json:"ttl_seconds,omitempty"`
}

type RoomCreateRequest struct {
	ID              string              `json:"id"`
	Metadata        json.RawMessage     `json:"metadata,omitempty"`
	DefaultAccesses []string            `json:"defaultAccesses,omitempty"`
	UsersAccesses   map[string][]string `json:"usersAccesses,omitempty"`
	GroupsAccesses  map[string][]string `json:"groupsAccesses,omitempty"`
}

type RoomUpdateRequest struct {
	Metadata        *json.RawMessage    `json:"metadata,omitempty"`
	DefaultAccesses *[]string           `json:"defaultAccesses,omitempty"`
	UsersAccesses   map[string][]string `json:"usersAccesses,omitempty"`
	GroupsAccesses  map[string][]string `json:"groupsAccesses,omitempty"`
}

type ThreadCreateRequest struct {
	ID       string               `json:"id,omitempty"`
	Metadata json.RawMessage      `json:"metadata,omitempty"`
	Comment  CommentCreateRequest `json:"comment"`
}

type ThreadUpdateRequest struct {
	Metadata *json.RawMessage `json:"metadata,omitempty"`
	Resolved *bool            `json:"resolved,omitempty"`
}

type CommentCreateRequest struct {
	ID        string                    `json:"id,omitempty"`
	UserID    string                    `json:"userId"`
	Body      json.RawMessage           `json:"body"`
	Metadata  json.RawMessage           `json:"metadata,omitempty"`
	Mentions  []string                  `json:"mentions,omitempty"`
	Reactions []cluster.CommentReaction `json:"reactions,omitempty"`
}

type CommentUpdateRequest struct {
	Body      *json.RawMessage           `json:"body,omitempty"`
	Metadata  *json.RawMessage           `json:"metadata,omitempty"`
	Mentions  *[]string                  `json:"mentions,omitempty"`
	Reactions *[]cluster.CommentReaction `json:"reactions,omitempty"`
}

type InboxNotificationTriggerRequest struct {
	ID           string          `json:"id,omitempty"`
	UserID       string          `json:"userId"`
	Kind         string          `json:"kind"`
	SubjectID    string          `json:"subjectId,omitempty"`
	ThreadID     string          `json:"threadId,omitempty"`
	RoomID       string          `json:"roomId,omitempty"`
	ActivityData json.RawMessage `json:"activityData,omitempty"`
}

type RoomSubscriptionSettingsRequest struct {
	Threads      string `json:"threads,omitempty"`
	TextMentions string `json:"textMentions,omitempty"`
}

const (
	maxStoragePatchOperations    = 100
	maxCommentMentions           = 100
	maxCommentReactions          = 500
	maxCommentReactionEmojiBytes = 64
	maxRoomListQueryBytes        = 1024
	maxRoomListQueryClauses      = 20
	maxRoomListQueryPathDepth    = 8
	maxRoomListQueryPathKeyBytes = 64
)

type roomListResponse struct {
	Rooms      []cluster.RoomRecord `json:"rooms"`
	NextCursor string               `json:"next_cursor,omitempty"`
}

type roomListQuery struct {
	Clauses []roomListQueryClause
}

type roomListQueryField int

const (
	roomListQueryFieldID roomListQueryField = iota
	roomListQueryFieldMetadata
)

type roomListQueryClause struct {
	Field        roomListQueryField
	MetadataPath []string
	Value        any
	Exists       bool
	IDContains   string
}

type activeUsersResponse struct {
	Data []cluster.ActiveUser `json:"data"`
}

type threadListResponse struct {
	Data       []cluster.ThreadRecord `json:"data"`
	NextCursor string                 `json:"next_cursor,omitempty"`
}

type threadListQuery struct {
	Clauses []threadListQueryClause
}

type threadListQueryField int

const (
	threadListQueryFieldResolved threadListQueryField = iota
	threadListQueryFieldMetadata
)

type threadListQueryClause struct {
	Field        threadListQueryField
	MetadataPath []string
	Value        any
	Exists       bool
	Resolved     bool
}

type inboxNotificationListResponse struct {
	Data       []cluster.InboxNotificationRecord `json:"data"`
	NextCursor string                            `json:"next_cursor,omitempty"`
}

type roomSubscriptionSettingsListResponse struct {
	Data       []cluster.RoomSubscriptionSettings `json:"data"`
	NextCursor string                             `json:"next_cursor,omitempty"`
}

func NewService(cfg config.RuntimeConfig, logger *log.Logger) (*Service, error) {
	var verifier *auth.Verifier
	if cfg.AdminAuth != nil {
		verifier = auth.NewVerifier(cfg.AdminAuth.Issuer, cfg.AdminAuth.Audience, cfg.AdminAuth.JWKSURL)
	}

	service := &Service{
		cfg:      cfg,
		logger:   logger,
		verifier: verifier,
		metrics:  observability.NewAdminMetrics(),
	}

	if cfg.Webhooks != nil && len(cfg.Webhooks.URLs) > 0 {
		service.webhookClient = &http.Client{Timeout: time.Duration(cfg.Webhooks.TimeoutMS) * time.Millisecond}
	}

	if cfg.Redis != nil {
		store, err := cluster.NewRedisStore(cfg.Redis.URL, cfg.Redis.ChannelPrefix)
		if err != nil {
			return nil, err
		}
		service.store = store
	}

	return service, nil
}

func (s *Service) Close() error {
	if s.store != nil {
		return s.store.Close()
	}
	return nil
}

func (s *Service) allowsRoomAction(ctx context.Context, claims *auth.Claims, action string, room string) bool {
	return roomengine.AllowsRoomAction(ctx, claims, s.store, action, room, roomengine.RoomAuthorizationOptions{
		EnforceTenantPrefix: s.cfg.Tenant.EnforcePrefix,
		TenantSeparator:     s.cfg.Tenant.Separator,
	})
}

func (s *Service) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/publish", s.handlePublish)
	mux.HandleFunc("/v1/presence", s.handlePresence)
	mux.HandleFunc("/v1/inbox-notifications/trigger", s.handleTriggerInboxNotification)
	mux.HandleFunc("/v1/inbox-notifications/", s.handleInboxNotificationAction)
	mux.HandleFunc("/v1/rooms", s.handleRooms)
	mux.HandleFunc("/v1/rooms/", s.handleRoom)
	mux.HandleFunc("/v1/users/", s.handleUser)
	mux.HandleFunc("/v1/stats", s.handleStats)
	mux.HandleFunc("/healthz", s.handleHealth)
	mux.HandleFunc("/readyz", s.handleReady)
	mux.Handle("/metrics", s.metrics.Handler())
	return mux
}

func (s *Service) handlePublish(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	claims, err := s.authenticate(r)
	if err != nil {
		s.writeError(w, openrtcerr.CodeAuthInvalid, "invalid bearer token", "", http.StatusUnauthorized)
		return
	}
	if s.store == nil {
		s.writeError(w, openrtcerr.CodeInternal, "admin publish requires redis backing", "", http.StatusServiceUnavailable)
		return
	}

	var request PublishRequest
	if err := decodeRequest(w, r, s.cfg.Limits.EnvelopeMaxBytes, &request); err != nil {
		s.writeError(w, openrtcerr.CodeBadRequest, "request body must be valid JSON", "", http.StatusBadRequest)
		return
	}
	if request.Room == "" || request.Event == "" || len(request.Payload) == 0 || !json.Valid(request.Payload) {
		s.writeError(w, openrtcerr.CodeBadRequest, "room, event, and payload are required", "", http.StatusBadRequest)
		return
	}
	if err := protocol.ValidateRoomName(request.Room); err != nil {
		s.writeError(w, openrtcerr.CodeBadRequest, err.Error(), "", http.StatusBadRequest)
		return
	}
	if err := protocol.ValidateEventName(request.Event); err != nil {
		s.writeError(w, openrtcerr.CodeBadRequest, err.Error(), "", http.StatusBadRequest)
		return
	}
	if request.ExcludeSenderConnID != "" {
		if err := protocol.ValidateConnectionID(request.ExcludeSenderConnID); err != nil {
			s.writeError(w, openrtcerr.CodeBadRequest, err.Error(), "", http.StatusBadRequest)
			return
		}
	}
	if len(request.Payload) > s.cfg.Limits.PayloadMaxBytes {
		s.writeError(w, openrtcerr.CodePayloadTooLarge, "payload exceeds max size", "", http.StatusRequestEntityTooLarge)
		return
	}
	if !s.allowsRoomAction(r.Context(), claims, "publish", request.Room) {
		s.writeError(w, openrtcerr.CodeRoomForbidden, "room publish is not permitted", "", http.StatusForbidden)
		return
	}

	event := roomengine.NewEvent(request.Room, request.Event, request.Payload, roomengine.EventOptions{
		ExcludeSenderConnID: request.ExcludeSenderConnID,
		TraceID:             request.TraceID,
		OriginNode:          "admin:" + s.cfg.NodeID,
	})
	if _, err := s.store.PublishEvent(r.Context(), event); err != nil {
		s.writeError(w, openrtcerr.CodeInternal, err.Error(), "", http.StatusInternalServerError)
		return
	}

	s.mu.Lock()
	s.stats.AdminPublishesTotal++
	snapshot := s.stats
	s.mu.Unlock()
	s.metrics.AdminPublishesTotal.Inc()
	_ = s.store.SyncStats(context.Background(), "admin:"+s.cfg.NodeID, snapshot)

	w.WriteHeader(http.StatusAccepted)
}

func (s *Service) handlePresence(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	claims, err := s.authenticate(r)
	if err != nil {
		s.writeError(w, openrtcerr.CodeAuthInvalid, "invalid bearer token", "", http.StatusUnauthorized)
		return
	}
	if s.store == nil {
		s.writeError(w, openrtcerr.CodeInternal, "admin presence requires redis backing", "", http.StatusServiceUnavailable)
		return
	}

	var request PresenceRequest
	if err := decodeRequest(w, r, s.cfg.Limits.EnvelopeMaxBytes, &request); err != nil {
		s.writeError(w, openrtcerr.CodeBadRequest, "request body must be valid JSON", "", http.StatusBadRequest)
		return
	}
	if request.Room == "" || request.ConnID == "" || len(request.State) == 0 || !json.Valid(request.State) || request.State[0] != '{' {
		s.writeError(w, openrtcerr.CodeBadRequest, "room, conn_id, and object state are required", "", http.StatusBadRequest)
		return
	}
	if err := protocol.ValidateRoomName(request.Room); err != nil {
		s.writeError(w, openrtcerr.CodeBadRequest, err.Error(), "", http.StatusBadRequest)
		return
	}
	if err := protocol.ValidateConnectionID(request.ConnID); err != nil {
		s.writeError(w, openrtcerr.CodeBadRequest, err.Error(), "", http.StatusBadRequest)
		return
	}
	if len(request.State) > s.cfg.Limits.PayloadMaxBytes {
		s.writeError(w, openrtcerr.CodePayloadTooLarge, "state exceeds max size", "", http.StatusRequestEntityTooLarge)
		return
	}
	if !s.allowsRoomAction(r.Context(), claims, "presence", request.Room) {
		s.writeError(w, openrtcerr.CodeRoomForbidden, "room presence is not permitted", "", http.StatusForbidden)
		return
	}

	ttl := 60 * time.Second
	if request.TTLSeconds > 0 {
		if request.TTLSeconds > 3600 {
			s.writeError(w, openrtcerr.CodeBadRequest, "ttl_seconds must be between 1 and 3600", "", http.StatusBadRequest)
			return
		}
		ttl = time.Duration(request.TTLSeconds) * time.Second
	}

	if err := s.store.SetEphemeralPresence(r.Context(), request.ConnID, request.Room, request.State, ttl); err != nil {
		s.writeError(w, openrtcerr.CodeInternal, err.Error(), "", http.StatusInternalServerError)
		return
	}
	if err := s.store.PublishPresence(r.Context(), cluster.PresenceEvent{
		Room:       request.Room,
		ConnID:     request.ConnID,
		State:      request.State,
		OriginNode: "admin:" + s.cfg.NodeID,
	}); err != nil {
		s.writeError(w, openrtcerr.CodeInternal, err.Error(), "", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusAccepted)
}

func (s *Service) handleRooms(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		s.handleCreateRoom(w, r)
	case http.MethodGet:
		s.handleListRooms(w, r)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Service) handleRoom(w http.ResponseWriter, r *http.Request) {
	room, subresource, err := roomPathParts(r.URL.Path)
	if err != nil {
		s.writeError(w, openrtcerr.CodeBadRequest, err.Error(), "", http.StatusBadRequest)
		return
	}
	if subresource == "storage" {
		s.handleStorage(w, r, room)
		return
	}
	if subresource == "storage/json-patch" {
		s.handleStoragePatch(w, r, room)
		return
	}
	if subresource == "active_users" || subresource == "active-users" {
		s.handleActiveUsers(w, r, room)
		return
	}
	if subresource == "threads" {
		s.handleThreads(w, r, room)
		return
	}
	if strings.HasPrefix(subresource, "threads/") {
		s.handleThreadSubresource(w, r, room, subresource)
		return
	}
	if strings.HasPrefix(subresource, "users/") {
		s.handleRoomUserSubresource(w, r, room, subresource)
		return
	}
	if subresource != "" {
		s.writeError(w, openrtcerr.CodeBadRequest, "unsupported room subresource", "", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.handleGetRoom(w, r, room)
	case http.MethodPatch:
		s.handleUpdateRoom(w, r, room)
	case http.MethodDelete:
		s.handleDeleteRoom(w, r, room)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Service) handleStorage(w http.ResponseWriter, r *http.Request, room string) {
	switch r.Method {
	case http.MethodGet:
		s.handleGetStorage(w, r, room)
	case http.MethodPut:
		s.handleSetStorage(w, r, room)
	case http.MethodDelete:
		s.handleDeleteStorage(w, r, room)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Service) handleStoragePatch(w http.ResponseWriter, r *http.Request, room string) {
	if r.Method != http.MethodPatch {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	claims, err := s.authenticate(r)
	if err != nil {
		s.writeError(w, openrtcerr.CodeAuthInvalid, "invalid bearer token", "", http.StatusUnauthorized)
		return
	}
	if s.store == nil {
		s.writeError(w, openrtcerr.CodeInternal, "storage APIs require redis backing", "", http.StatusServiceUnavailable)
		return
	}
	if !s.allowsRoomAction(r.Context(), claims, "storage:write", room) {
		s.writeError(w, openrtcerr.CodeRoomForbidden, "room storage is not permitted", "", http.StatusForbidden)
		return
	}

	operations, parseErr := decodeStoragePatch(w, r, s.cfg.Limits.PayloadMaxBytes)
	if parseErr != nil {
		s.writeError(w, parseErr.Code, parseErr.Message, "", openrtcerr.DescriptorFor(parseErr.Code).HTTPStatus)
		return
	}
	plan, err := s.applyStoragePatchMutationEventPlan(r.Context(), room, operations)
	if errors.Is(err, cluster.ErrStorageNotFound) {
		s.writeError(w, openrtcerr.CodeStorageNotFound, "storage document not found", "", http.StatusNotFound)
		return
	}
	if errors.Is(err, cluster.ErrStoragePatch) {
		s.writeError(w, openrtcerr.CodePatchFailed, err.Error(), "", http.StatusUnprocessableEntity)
		return
	}
	if err != nil {
		s.writeError(w, openrtcerr.CodeInternal, err.Error(), "", http.StatusInternalServerError)
		return
	}
	if err := s.publishStorageMutationEventPlan(r.Context(), plan); err != nil {
		s.writeError(w, openrtcerr.CodeInternal, err.Error(), "", http.StatusInternalServerError)
		return
	}
	writeRawJSON(w, http.StatusOK, plan.Mutation.Document)
}

func (s *Service) handleActiveUsers(w http.ResponseWriter, r *http.Request, room string) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	claims, err := s.authenticate(r)
	if err != nil {
		s.writeError(w, openrtcerr.CodeAuthInvalid, "invalid bearer token", "", http.StatusUnauthorized)
		return
	}
	if s.store == nil {
		s.writeError(w, openrtcerr.CodeInternal, "active users API requires redis backing", "", http.StatusServiceUnavailable)
		return
	}
	if !s.allowsRoomAction(r.Context(), claims, "presence", room) {
		s.writeError(w, openrtcerr.CodeRoomForbidden, "room presence is not permitted", "", http.StatusForbidden)
		return
	}

	users, err := s.store.ActiveUsers(r.Context(), room)
	if err != nil {
		s.writeError(w, openrtcerr.CodeInternal, err.Error(), "", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, activeUsersResponse{Data: users})
}

func (s *Service) handleThreads(w http.ResponseWriter, r *http.Request, room string) {
	switch r.Method {
	case http.MethodGet:
		s.handleListThreads(w, r, room)
	case http.MethodPost:
		s.handleCreateThread(w, r, room)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Service) handleThreadSubresource(w http.ResponseWriter, r *http.Request, room string, subresource string) {
	threadID, child, err := threadPathParts(subresource)
	if err != nil {
		s.writeError(w, openrtcerr.CodeBadRequest, err.Error(), "", http.StatusBadRequest)
		return
	}
	if child == "" {
		s.handleThread(w, r, room, threadID)
		return
	}
	if child == "comments" {
		s.handleAddComment(w, r, room, threadID)
		return
	}
	if strings.HasPrefix(child, "comments/") {
		commentID, err := commentPathPart(child)
		if err != nil {
			s.writeError(w, openrtcerr.CodeBadRequest, err.Error(), "", http.StatusBadRequest)
			return
		}
		s.handleUpdateComment(w, r, room, threadID, commentID)
		return
	}
	s.writeError(w, openrtcerr.CodeBadRequest, "unsupported thread subresource", "", http.StatusBadRequest)
}

func (s *Service) handleThread(w http.ResponseWriter, r *http.Request, room string, threadID string) {
	switch r.Method {
	case http.MethodGet:
		s.handleGetThread(w, r, room, threadID)
	case http.MethodPatch:
		s.handleUpdateThread(w, r, room, threadID)
	case http.MethodDelete:
		s.handleDeleteThread(w, r, room, threadID)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Service) handleRoomUserSubresource(w http.ResponseWriter, r *http.Request, room string, subresource string) {
	userID, child, err := roomUserPathParts(subresource)
	if err != nil {
		s.writeError(w, openrtcerr.CodeBadRequest, err.Error(), "", http.StatusBadRequest)
		return
	}
	if child == "subscription-settings" {
		s.handleRoomSubscriptionSettings(w, r, room, userID)
		return
	}
	s.writeError(w, openrtcerr.CodeBadRequest, "unsupported room user subresource", "", http.StatusBadRequest)
}

func (s *Service) handleListThreads(w http.ResponseWriter, r *http.Request, room string) {
	claims, err := s.authenticate(r)
	if err != nil {
		s.writeError(w, openrtcerr.CodeAuthInvalid, "invalid bearer token", "", http.StatusUnauthorized)
		return
	}
	if s.store == nil {
		s.writeError(w, openrtcerr.CodeInternal, "thread APIs require redis backing", "", http.StatusServiceUnavailable)
		return
	}
	if !s.allowsRoomAction(r.Context(), claims, "comments:read", room) {
		s.writeError(w, openrtcerr.CodeRoomForbidden, "room comments are not permitted", "", http.StatusForbidden)
		return
	}
	limit, err := parseThreadListLimit(r.URL.Query().Get("limit"), r.URL.Query().Get("cursor"))
	if err != nil {
		s.writeError(w, openrtcerr.CodeBadRequest, err.Error(), "", http.StatusBadRequest)
		return
	}
	cursor, err := parseCursor(r.URL.Query().Get("cursor"))
	if err != nil {
		s.writeError(w, openrtcerr.CodeBadRequest, err.Error(), "", http.StatusBadRequest)
		return
	}
	query, parseErr := parseThreadListQuery(r.URL.Query().Get("query"))
	if parseErr != nil {
		s.writeError(w, parseErr.Code, parseErr.Message, "", openrtcerr.DescriptorFor(parseErr.Code).HTTPStatus)
		return
	}
	threads, err := s.store.ListThreads(r.Context(), room)
	if err != nil {
		s.writeError(w, openrtcerr.CodeInternal, err.Error(), "", http.StatusInternalServerError)
		return
	}
	threads = filterThreads(threads, query)
	page, nextCursor := paginateThreads(threads, cursor, limit)
	response := threadListResponse{Data: page}
	if nextCursor != 0 {
		response.NextCursor = strconv.FormatUint(nextCursor, 10)
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Service) handleGetThread(w http.ResponseWriter, r *http.Request, room string, threadID string) {
	claims, err := s.authenticate(r)
	if err != nil {
		s.writeError(w, openrtcerr.CodeAuthInvalid, "invalid bearer token", "", http.StatusUnauthorized)
		return
	}
	if s.store == nil {
		s.writeError(w, openrtcerr.CodeInternal, "thread APIs require redis backing", "", http.StatusServiceUnavailable)
		return
	}
	if !s.allowsRoomAction(r.Context(), claims, "comments:read", room) {
		s.writeError(w, openrtcerr.CodeRoomForbidden, "room comments are not permitted", "", http.StatusForbidden)
		return
	}
	thread, err := s.store.GetThread(r.Context(), room, threadID)
	if errors.Is(err, cluster.ErrThreadNotFound) {
		s.writeError(w, openrtcerr.CodeThreadNotFound, "thread not found", "", http.StatusNotFound)
		return
	}
	if err != nil {
		s.writeError(w, openrtcerr.CodeInternal, err.Error(), "", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, thread)
}

func (s *Service) handleCreateThread(w http.ResponseWriter, r *http.Request, room string) {
	claims, err := s.authenticate(r)
	if err != nil {
		s.writeError(w, openrtcerr.CodeAuthInvalid, "invalid bearer token", "", http.StatusUnauthorized)
		return
	}
	if s.store == nil {
		s.writeError(w, openrtcerr.CodeInternal, "thread APIs require redis backing", "", http.StatusServiceUnavailable)
		return
	}
	if !s.allowsRoomAction(r.Context(), claims, "comments:write", room) {
		s.writeError(w, openrtcerr.CodeRoomForbidden, "room comments are not permitted", "", http.StatusForbidden)
		return
	}

	var request ThreadCreateRequest
	if err := decodeRequest(w, r, s.cfg.Limits.EnvelopeMaxBytes, &request); err != nil {
		s.writeError(w, openrtcerr.CodeBadRequest, "request body must be valid JSON", "", http.StatusBadRequest)
		return
	}
	threadID := request.ID
	if threadID == "" {
		threadID = newRecordID("th")
	}
	if request.Comment.ID == "" {
		request.Comment.ID = newRecordID("cm")
	}
	if parseErr := validateThreadRequest(threadID, request.Metadata, request.Comment, s.cfg.Limits.PayloadMaxBytes); parseErr != nil {
		s.writeError(w, parseErr.Code, parseErr.Message, "", openrtcerr.DescriptorFor(parseErr.Code).HTTPStatus)
		return
	}
	comment := commentRecordFromRequest(room, threadID, request.Comment)
	thread, err := s.store.CreateThread(r.Context(), room, cluster.ThreadRecord{
		ID:       threadID,
		RoomID:   room,
		Metadata: normalizedMetadata(request.Metadata),
		Comments: []cluster.CommentRecord{comment},
	})
	if errors.Is(err, cluster.ErrThreadAlreadyExists) {
		s.writeError(w, openrtcerr.CodeThreadConflict, "thread already exists", "", http.StatusConflict)
		return
	}
	if err != nil {
		s.writeError(w, openrtcerr.CodeInternal, err.Error(), "", http.StatusInternalServerError)
		return
	}
	if err := s.publishCommentEvent(r.Context(), roomengine.CommentThreadCreated, thread, firstThreadComment(thread)); err != nil {
		s.writeError(w, openrtcerr.CodeInternal, err.Error(), "", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, thread)
}

func (s *Service) handleUpdateThread(w http.ResponseWriter, r *http.Request, room string, threadID string) {
	claims, err := s.authenticate(r)
	if err != nil {
		s.writeError(w, openrtcerr.CodeAuthInvalid, "invalid bearer token", "", http.StatusUnauthorized)
		return
	}
	if s.store == nil {
		s.writeError(w, openrtcerr.CodeInternal, "thread APIs require redis backing", "", http.StatusServiceUnavailable)
		return
	}
	if !s.allowsRoomAction(r.Context(), claims, "comments:write", room) {
		s.writeError(w, openrtcerr.CodeRoomForbidden, "room comments are not permitted", "", http.StatusForbidden)
		return
	}
	var request ThreadUpdateRequest
	if err := decodeRequest(w, r, s.cfg.Limits.EnvelopeMaxBytes, &request); err != nil {
		s.writeError(w, openrtcerr.CodeBadRequest, "request body must be valid JSON", "", http.StatusBadRequest)
		return
	}
	if parseErr := validateThreadUpdateRequest(request, s.cfg.Limits.PayloadMaxBytes); parseErr != nil {
		s.writeError(w, parseErr.Code, parseErr.Message, "", openrtcerr.DescriptorFor(parseErr.Code).HTTPStatus)
		return
	}
	thread, err := s.store.UpdateThread(r.Context(), room, threadID, threadUpdateFromRequest(request))
	if errors.Is(err, cluster.ErrThreadNotFound) {
		s.writeError(w, openrtcerr.CodeThreadNotFound, "thread not found", "", http.StatusNotFound)
		return
	}
	if err != nil {
		s.writeError(w, openrtcerr.CodeInternal, err.Error(), "", http.StatusInternalServerError)
		return
	}
	if err := s.publishCommentEvent(r.Context(), roomengine.CommentThreadUpdated, thread, nil); err != nil {
		s.writeError(w, openrtcerr.CodeInternal, err.Error(), "", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, thread)
}

func (s *Service) handleDeleteThread(w http.ResponseWriter, r *http.Request, room string, threadID string) {
	claims, err := s.authenticate(r)
	if err != nil {
		s.writeError(w, openrtcerr.CodeAuthInvalid, "invalid bearer token", "", http.StatusUnauthorized)
		return
	}
	if s.store == nil {
		s.writeError(w, openrtcerr.CodeInternal, "thread APIs require redis backing", "", http.StatusServiceUnavailable)
		return
	}
	if !s.allowsRoomAction(r.Context(), claims, "comments:write", room) {
		s.writeError(w, openrtcerr.CodeRoomForbidden, "room comments are not permitted", "", http.StatusForbidden)
		return
	}
	thread, err := s.store.DeleteThread(r.Context(), room, threadID)
	if errors.Is(err, cluster.ErrThreadNotFound) {
		s.writeError(w, openrtcerr.CodeThreadNotFound, "thread not found", "", http.StatusNotFound)
		return
	}
	if err != nil {
		s.writeError(w, openrtcerr.CodeInternal, err.Error(), "", http.StatusInternalServerError)
		return
	}
	if err := s.publishCommentEvent(r.Context(), roomengine.CommentThreadDeleted, thread, nil); err != nil {
		s.writeError(w, openrtcerr.CodeInternal, err.Error(), "", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) handleAddComment(w http.ResponseWriter, r *http.Request, room string, threadID string) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	claims, err := s.authenticate(r)
	if err != nil {
		s.writeError(w, openrtcerr.CodeAuthInvalid, "invalid bearer token", "", http.StatusUnauthorized)
		return
	}
	if s.store == nil {
		s.writeError(w, openrtcerr.CodeInternal, "thread APIs require redis backing", "", http.StatusServiceUnavailable)
		return
	}
	if !s.allowsRoomAction(r.Context(), claims, "comments:write", room) {
		s.writeError(w, openrtcerr.CodeRoomForbidden, "room comments are not permitted", "", http.StatusForbidden)
		return
	}

	var request CommentCreateRequest
	if err := decodeRequest(w, r, s.cfg.Limits.EnvelopeMaxBytes, &request); err != nil {
		s.writeError(w, openrtcerr.CodeBadRequest, "request body must be valid JSON", "", http.StatusBadRequest)
		return
	}
	if request.ID == "" {
		request.ID = newRecordID("cm")
	}
	if parseErr := validateCommentRequest(request, s.cfg.Limits.PayloadMaxBytes); parseErr != nil {
		s.writeError(w, parseErr.Code, parseErr.Message, "", openrtcerr.DescriptorFor(parseErr.Code).HTTPStatus)
		return
	}
	thread, err := s.store.AddComment(r.Context(), room, threadID, commentRecordFromRequest(room, threadID, request))
	if errors.Is(err, cluster.ErrThreadNotFound) {
		s.writeError(w, openrtcerr.CodeThreadNotFound, "thread not found", "", http.StatusNotFound)
		return
	}
	if err != nil {
		s.writeError(w, openrtcerr.CodeInternal, err.Error(), "", http.StatusInternalServerError)
		return
	}
	if err := s.publishCommentEvent(r.Context(), roomengine.CommentCreated, thread, findThreadComment(thread, request.ID)); err != nil {
		s.writeError(w, openrtcerr.CodeInternal, err.Error(), "", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, thread)
}

func (s *Service) handleUpdateComment(w http.ResponseWriter, r *http.Request, room string, threadID string, commentID string) {
	if r.Method != http.MethodPatch {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	claims, err := s.authenticate(r)
	if err != nil {
		s.writeError(w, openrtcerr.CodeAuthInvalid, "invalid bearer token", "", http.StatusUnauthorized)
		return
	}
	if s.store == nil {
		s.writeError(w, openrtcerr.CodeInternal, "thread APIs require redis backing", "", http.StatusServiceUnavailable)
		return
	}
	if !s.allowsRoomAction(r.Context(), claims, "comments:write", room) {
		s.writeError(w, openrtcerr.CodeRoomForbidden, "room comments are not permitted", "", http.StatusForbidden)
		return
	}

	var request CommentUpdateRequest
	if err := decodeRequest(w, r, s.cfg.Limits.EnvelopeMaxBytes, &request); err != nil {
		s.writeError(w, openrtcerr.CodeBadRequest, "request body must be valid JSON", "", http.StatusBadRequest)
		return
	}
	if parseErr := validateCommentUpdateRequest(request, s.cfg.Limits.PayloadMaxBytes); parseErr != nil {
		s.writeError(w, parseErr.Code, parseErr.Message, "", openrtcerr.DescriptorFor(parseErr.Code).HTTPStatus)
		return
	}
	thread, err := s.store.UpdateComment(r.Context(), room, threadID, commentID, commentUpdateFromRequest(request))
	if errors.Is(err, cluster.ErrThreadNotFound) || errors.Is(err, cluster.ErrCommentNotFound) {
		s.writeError(w, openrtcerr.CodeThreadNotFound, err.Error(), "", http.StatusNotFound)
		return
	}
	if err != nil {
		s.writeError(w, openrtcerr.CodeInternal, err.Error(), "", http.StatusInternalServerError)
		return
	}
	if err := s.publishCommentEvent(r.Context(), roomengine.CommentUpdated, thread, findThreadComment(thread, commentID)); err != nil {
		s.writeError(w, openrtcerr.CodeInternal, err.Error(), "", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, thread)
}

func (s *Service) handleUser(w http.ResponseWriter, r *http.Request) {
	userID, subresource, itemID, err := userPathParts(r.URL.Path)
	if err != nil {
		s.writeError(w, openrtcerr.CodeBadRequest, err.Error(), "", http.StatusBadRequest)
		return
	}
	switch subresource {
	case "inbox-notifications":
		s.handleUserInboxNotifications(w, r, userID, itemID)
	case "notification-settings":
		if itemID != "" {
			s.writeError(w, openrtcerr.CodeBadRequest, "unsupported notification settings subresource", "", http.StatusBadRequest)
			return
		}
		s.handleNotificationSettings(w, r, userID)
	case "room-subscription-settings":
		if itemID != "" {
			s.writeError(w, openrtcerr.CodeBadRequest, "unsupported room subscription settings subresource", "", http.StatusBadRequest)
			return
		}
		s.handleUserRoomSubscriptionSettings(w, r, userID)
	default:
		s.writeError(w, openrtcerr.CodeBadRequest, "unsupported user subresource", "", http.StatusBadRequest)
	}
}

func (s *Service) handleUserInboxNotifications(w http.ResponseWriter, r *http.Request, userID string, notificationID string) {
	claims, err := s.authenticate(r)
	if err != nil {
		s.writeError(w, openrtcerr.CodeAuthInvalid, "invalid bearer token", "", http.StatusUnauthorized)
		return
	}
	if s.store == nil {
		s.writeError(w, openrtcerr.CodeInternal, "notification APIs require redis backing", "", http.StatusServiceUnavailable)
		return
	}
	if !s.notificationAllowed(claims, userID) {
		s.writeError(w, openrtcerr.CodeRoomForbidden, "user notifications are not permitted", "", http.StatusForbidden)
		return
	}

	if notificationID == "" {
		switch r.Method {
		case http.MethodGet:
			limit, parseErr := parseNotificationListLimit(r.URL.Query().Get("limit"))
			if parseErr != nil {
				s.writeError(w, openrtcerr.CodeBadRequest, parseErr.Error(), "", http.StatusBadRequest)
				return
			}
			cursor, parseErr := parseCursor(firstNonEmpty(r.URL.Query().Get("cursor"), r.URL.Query().Get("startingAfter")))
			if parseErr != nil {
				s.writeError(w, openrtcerr.CodeBadRequest, parseErr.Error(), "", http.StatusBadRequest)
				return
			}
			list, err := s.store.ListInboxNotifications(r.Context(), userID, cluster.InboxNotificationListFilter{
				UnreadOnly: unreadOnlyQuery(r.URL.Query().Get("query"), r.URL.Query().Get("unread")),
				Cursor:     cursor,
				Limit:      limit,
			})
			if err != nil {
				s.writeError(w, openrtcerr.CodeInternal, err.Error(), "", http.StatusInternalServerError)
				return
			}
			response := inboxNotificationListResponse{Data: list.Data}
			if list.NextCursor != 0 {
				response.NextCursor = strconv.FormatUint(list.NextCursor, 10)
			}
			writeJSON(w, http.StatusOK, response)
		case http.MethodDelete:
			if err := s.store.DeleteAllInboxNotifications(r.Context(), userID); err != nil {
				s.writeError(w, openrtcerr.CodeInternal, err.Error(), "", http.StatusInternalServerError)
				return
			}
			if err := s.publishNotificationEvent(r.Context(), roomengine.NotificationInboxDeletedAll, userID, nil); err != nil {
				s.writeError(w, openrtcerr.CodeInternal, err.Error(), "", http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
		return
	}

	switch r.Method {
	case http.MethodGet:
		notification, err := s.store.GetInboxNotification(r.Context(), userID, notificationID)
		if errors.Is(err, cluster.ErrInboxNotFound) {
			s.writeError(w, openrtcerr.CodeInboxNotificationNotFound, "inbox notification not found", "", http.StatusNotFound)
			return
		}
		if err != nil {
			s.writeError(w, openrtcerr.CodeInternal, err.Error(), "", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, notification)
	case http.MethodDelete:
		notification, err := s.store.GetInboxNotification(r.Context(), userID, notificationID)
		if errors.Is(err, cluster.ErrInboxNotFound) {
			s.writeError(w, openrtcerr.CodeInboxNotificationNotFound, "inbox notification not found", "", http.StatusNotFound)
			return
		}
		if err != nil {
			s.writeError(w, openrtcerr.CodeInternal, err.Error(), "", http.StatusInternalServerError)
			return
		}
		err = s.store.DeleteInboxNotification(r.Context(), userID, notificationID)
		if errors.Is(err, cluster.ErrInboxNotFound) {
			s.writeError(w, openrtcerr.CodeInboxNotificationNotFound, "inbox notification not found", "", http.StatusNotFound)
			return
		}
		if err != nil {
			s.writeError(w, openrtcerr.CodeInternal, err.Error(), "", http.StatusInternalServerError)
			return
		}
		if err := s.publishNotificationEvent(r.Context(), roomengine.NotificationInboxDeleted, userID, &notification); err != nil {
			s.writeError(w, openrtcerr.CodeInternal, err.Error(), "", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Service) handleTriggerInboxNotification(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	claims, err := s.authenticate(r)
	if err != nil {
		s.writeError(w, openrtcerr.CodeAuthInvalid, "invalid bearer token", "", http.StatusUnauthorized)
		return
	}
	if s.store == nil {
		s.writeError(w, openrtcerr.CodeInternal, "notification APIs require redis backing", "", http.StatusServiceUnavailable)
		return
	}

	var request InboxNotificationTriggerRequest
	if err := decodeRequest(w, r, s.cfg.Limits.EnvelopeMaxBytes, &request); err != nil {
		s.writeError(w, openrtcerr.CodeBadRequest, "request body must be valid JSON", "", http.StatusBadRequest)
		return
	}
	if request.ID == "" {
		request.ID = newRecordID("in")
	}
	if parseErr := validateInboxNotificationRequest(request, s.cfg.Limits.PayloadMaxBytes); parseErr != nil {
		s.writeError(w, parseErr.Code, parseErr.Message, "", openrtcerr.DescriptorFor(parseErr.Code).HTTPStatus)
		return
	}
	if !s.notificationAllowed(claims, request.UserID) {
		s.writeError(w, openrtcerr.CodeRoomForbidden, "user notifications are not permitted", "", http.StatusForbidden)
		return
	}

	notification, err := s.store.CreateInboxNotification(r.Context(), cluster.InboxNotificationRecord{
		ID:           request.ID,
		UserID:       request.UserID,
		Kind:         request.Kind,
		SubjectID:    request.SubjectID,
		ThreadID:     request.ThreadID,
		RoomID:       request.RoomID,
		ActivityData: normalizedMetadata(request.ActivityData),
	})
	if errors.Is(err, cluster.ErrInboxAlreadyExists) {
		s.writeError(w, openrtcerr.CodeInboxNotificationConflict, "inbox notification already exists", "", http.StatusConflict)
		return
	}
	if err != nil {
		s.writeError(w, openrtcerr.CodeInternal, err.Error(), "", http.StatusInternalServerError)
		return
	}
	if err := s.publishNotificationEvent(r.Context(), roomengine.NotificationInboxCreated, notification.UserID, &notification); err != nil {
		s.writeError(w, openrtcerr.CodeInternal, err.Error(), "", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, notification)
}

func (s *Service) handleInboxNotificationAction(w http.ResponseWriter, r *http.Request) {
	notificationID, child, err := inboxNotificationActionParts(r.URL.Path)
	if err != nil {
		s.writeError(w, openrtcerr.CodeBadRequest, err.Error(), "", http.StatusBadRequest)
		return
	}
	if child != "read" {
		s.writeError(w, openrtcerr.CodeBadRequest, "unsupported inbox notification action", "", http.StatusBadRequest)
		return
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	claims, err := s.authenticate(r)
	if err != nil {
		s.writeError(w, openrtcerr.CodeAuthInvalid, "invalid bearer token", "", http.StatusUnauthorized)
		return
	}
	if s.store == nil {
		s.writeError(w, openrtcerr.CodeInternal, "notification APIs require redis backing", "", http.StatusServiceUnavailable)
		return
	}
	existing, err := s.store.GetInboxNotification(r.Context(), "", notificationID)
	if errors.Is(err, cluster.ErrInboxNotFound) {
		s.writeError(w, openrtcerr.CodeInboxNotificationNotFound, "inbox notification not found", "", http.StatusNotFound)
		return
	}
	if err != nil {
		s.writeError(w, openrtcerr.CodeInternal, err.Error(), "", http.StatusInternalServerError)
		return
	}
	if !s.notificationAllowed(claims, existing.UserID) {
		s.writeError(w, openrtcerr.CodeRoomForbidden, "user notifications are not permitted", "", http.StatusForbidden)
		return
	}
	notification, err := s.store.MarkInboxNotificationRead(r.Context(), notificationID)
	if errors.Is(err, cluster.ErrInboxNotFound) {
		s.writeError(w, openrtcerr.CodeInboxNotificationNotFound, "inbox notification not found", "", http.StatusNotFound)
		return
	}
	if err != nil {
		s.writeError(w, openrtcerr.CodeInternal, err.Error(), "", http.StatusInternalServerError)
		return
	}
	if err := s.publishNotificationEvent(r.Context(), roomengine.NotificationInboxRead, notification.UserID, &notification); err != nil {
		s.writeError(w, openrtcerr.CodeInternal, err.Error(), "", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, notification)
}

func (s *Service) handleNotificationSettings(w http.ResponseWriter, r *http.Request, userID string) {
	claims, err := s.authenticate(r)
	if err != nil {
		s.writeError(w, openrtcerr.CodeAuthInvalid, "invalid bearer token", "", http.StatusUnauthorized)
		return
	}
	if s.store == nil {
		s.writeError(w, openrtcerr.CodeInternal, "notification APIs require redis backing", "", http.StatusServiceUnavailable)
		return
	}
	if !s.notificationAllowed(claims, userID) {
		s.writeError(w, openrtcerr.CodeRoomForbidden, "user notifications are not permitted", "", http.StatusForbidden)
		return
	}
	switch r.Method {
	case http.MethodGet:
		settings, err := s.store.GetNotificationSettings(r.Context(), userID)
		if err != nil {
			s.writeError(w, openrtcerr.CodeInternal, err.Error(), "", http.StatusInternalServerError)
			return
		}
		writeRawJSON(w, http.StatusOK, settings)
	case http.MethodPost:
		settings, parseErr := readNotificationSettings(w, r, s.cfg.Limits.PayloadMaxBytes)
		if parseErr != nil {
			s.writeError(w, parseErr.Code, parseErr.Message, "", openrtcerr.DescriptorFor(parseErr.Code).HTTPStatus)
			return
		}
		stored, err := s.store.SetNotificationSettings(r.Context(), userID, settings)
		if err != nil {
			s.writeError(w, openrtcerr.CodeInternal, err.Error(), "", http.StatusInternalServerError)
			return
		}
		writeRawJSON(w, http.StatusOK, stored)
	case http.MethodDelete:
		if err := s.store.DeleteNotificationSettings(r.Context(), userID); err != nil {
			s.writeError(w, openrtcerr.CodeInternal, err.Error(), "", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Service) handleRoomSubscriptionSettings(w http.ResponseWriter, r *http.Request, room string, userID string) {
	claims, err := s.authenticate(r)
	if err != nil {
		s.writeError(w, openrtcerr.CodeAuthInvalid, "invalid bearer token", "", http.StatusUnauthorized)
		return
	}
	if s.store == nil {
		s.writeError(w, openrtcerr.CodeInternal, "notification APIs require redis backing", "", http.StatusServiceUnavailable)
		return
	}
	if !s.notificationAllowed(claims, userID) {
		s.writeError(w, openrtcerr.CodeRoomForbidden, "user notifications are not permitted", "", http.StatusForbidden)
		return
	}
	switch r.Method {
	case http.MethodGet:
		settings, err := s.store.GetRoomSubscriptionSettings(r.Context(), room, userID)
		if err != nil {
			s.writeError(w, openrtcerr.CodeInternal, err.Error(), "", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, settings)
	case http.MethodPost:
		var request RoomSubscriptionSettingsRequest
		if err := decodeRequest(w, r, s.cfg.Limits.EnvelopeMaxBytes, &request); err != nil {
			s.writeError(w, openrtcerr.CodeBadRequest, "request body must be valid JSON", "", http.StatusBadRequest)
			return
		}
		if parseErr := validateRoomSubscriptionSettingsRequest(request); parseErr != nil {
			s.writeError(w, parseErr.Code, parseErr.Message, "", openrtcerr.DescriptorFor(parseErr.Code).HTTPStatus)
			return
		}
		settings, err := s.store.SetRoomSubscriptionSettings(r.Context(), cluster.RoomSubscriptionSettings{
			RoomID:       room,
			UserID:       userID,
			Threads:      request.Threads,
			TextMentions: request.TextMentions,
		})
		if err != nil {
			s.writeError(w, openrtcerr.CodeInternal, err.Error(), "", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, settings)
	case http.MethodDelete:
		if err := s.store.DeleteRoomSubscriptionSettings(r.Context(), room, userID); err != nil {
			s.writeError(w, openrtcerr.CodeInternal, err.Error(), "", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Service) handleUserRoomSubscriptionSettings(w http.ResponseWriter, r *http.Request, userID string) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	claims, err := s.authenticate(r)
	if err != nil {
		s.writeError(w, openrtcerr.CodeAuthInvalid, "invalid bearer token", "", http.StatusUnauthorized)
		return
	}
	if s.store == nil {
		s.writeError(w, openrtcerr.CodeInternal, "notification APIs require redis backing", "", http.StatusServiceUnavailable)
		return
	}
	if !s.notificationAllowed(claims, userID) {
		s.writeError(w, openrtcerr.CodeRoomForbidden, "user notifications are not permitted", "", http.StatusForbidden)
		return
	}
	limit, parseErr := parseNotificationListLimit(r.URL.Query().Get("limit"))
	if parseErr != nil {
		s.writeError(w, openrtcerr.CodeBadRequest, parseErr.Error(), "", http.StatusBadRequest)
		return
	}
	cursor, parseErr := parseCursor(firstNonEmpty(r.URL.Query().Get("cursor"), r.URL.Query().Get("startingAfter")))
	if parseErr != nil {
		s.writeError(w, openrtcerr.CodeBadRequest, parseErr.Error(), "", http.StatusBadRequest)
		return
	}
	list, err := s.store.ListRoomSubscriptionSettings(r.Context(), userID, cursor, limit)
	if err != nil {
		s.writeError(w, openrtcerr.CodeInternal, err.Error(), "", http.StatusInternalServerError)
		return
	}
	response := roomSubscriptionSettingsListResponse{Data: list.Data}
	if list.NextCursor != 0 {
		response.NextCursor = strconv.FormatUint(list.NextCursor, 10)
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Service) handleCreateRoom(w http.ResponseWriter, r *http.Request) {
	claims, err := s.authenticate(r)
	if err != nil {
		s.writeError(w, openrtcerr.CodeAuthInvalid, "invalid bearer token", "", http.StatusUnauthorized)
		return
	}
	if s.store == nil {
		s.writeError(w, openrtcerr.CodeInternal, "room APIs require redis backing", "", http.StatusServiceUnavailable)
		return
	}

	var request RoomCreateRequest
	if err := decodeRequest(w, r, s.cfg.Limits.EnvelopeMaxBytes, &request); err != nil {
		s.writeError(w, openrtcerr.CodeBadRequest, "request body must be valid JSON", "", http.StatusBadRequest)
		return
	}
	if err := validateRoomRequest(request.ID, request.Metadata, true, s.cfg.Limits.PayloadMaxBytes); err != nil {
		s.writeError(w, err.Code, err.Message, "", openrtcerr.DescriptorFor(err.Code).HTTPStatus)
		return
	}
	if err := validateRoomAccesses(request.DefaultAccesses, request.UsersAccesses, request.GroupsAccesses); err != nil {
		s.writeError(w, err.Code, err.Message, "", openrtcerr.DescriptorFor(err.Code).HTTPStatus)
		return
	}
	if !claims.Allows("rooms", request.ID, s.cfg.Tenant.EnforcePrefix, s.cfg.Tenant.Separator) {
		s.writeError(w, openrtcerr.CodeRoomForbidden, "room administration is not permitted", "", http.StatusForbidden)
		return
	}

	record, err := s.store.CreateRoom(r.Context(), cluster.RoomRecord{
		ID:              request.ID,
		Metadata:        normalizedMetadata(request.Metadata),
		DefaultAccesses: request.DefaultAccesses,
		UsersAccesses:   request.UsersAccesses,
		GroupsAccesses:  request.GroupsAccesses,
	})
	if errors.Is(err, cluster.ErrRoomAlreadyExists) {
		s.writeError(w, openrtcerr.CodeRoomConflict, "room already exists", "", http.StatusConflict)
		return
	}
	if err != nil {
		s.writeError(w, openrtcerr.CodeInternal, err.Error(), "", http.StatusInternalServerError)
		return
	}
	s.dispatchRoomWebhook(r.Context(), roomengine.RoomCreated, record.ID, &record)
	writeJSON(w, http.StatusCreated, record)
}

func (s *Service) handleGetStorage(w http.ResponseWriter, r *http.Request, room string) {
	claims, err := s.authenticate(r)
	if err != nil {
		s.writeError(w, openrtcerr.CodeAuthInvalid, "invalid bearer token", "", http.StatusUnauthorized)
		return
	}
	if s.store == nil {
		s.writeError(w, openrtcerr.CodeInternal, "storage APIs require redis backing", "", http.StatusServiceUnavailable)
		return
	}
	if !s.allowsRoomAction(r.Context(), claims, "storage:read", room) {
		s.writeError(w, openrtcerr.CodeRoomForbidden, "room storage is not permitted", "", http.StatusForbidden)
		return
	}

	document, err := s.store.GetStorage(r.Context(), room)
	if errors.Is(err, cluster.ErrStorageNotFound) {
		s.writeError(w, openrtcerr.CodeStorageNotFound, "storage document not found", "", http.StatusNotFound)
		return
	}
	if err != nil {
		s.writeError(w, openrtcerr.CodeInternal, err.Error(), "", http.StatusInternalServerError)
		return
	}
	writeRawJSON(w, http.StatusOK, document)
}

func (s *Service) handleSetStorage(w http.ResponseWriter, r *http.Request, room string) {
	claims, err := s.authenticate(r)
	if err != nil {
		s.writeError(w, openrtcerr.CodeAuthInvalid, "invalid bearer token", "", http.StatusUnauthorized)
		return
	}
	if s.store == nil {
		s.writeError(w, openrtcerr.CodeInternal, "storage APIs require redis backing", "", http.StatusServiceUnavailable)
		return
	}
	if !s.allowsRoomAction(r.Context(), claims, "storage:write", room) {
		s.writeError(w, openrtcerr.CodeRoomForbidden, "room storage is not permitted", "", http.StatusForbidden)
		return
	}

	document, parseErr := readStorageDocument(w, r, s.cfg.Limits.PayloadMaxBytes)
	if parseErr != nil {
		s.writeError(w, parseErr.Code, parseErr.Message, "", openrtcerr.DescriptorFor(parseErr.Code).HTTPStatus)
		return
	}
	plan, err := s.setStorageMutationEventPlan(r.Context(), room, document)
	if err != nil {
		s.writeError(w, openrtcerr.CodeInternal, err.Error(), "", http.StatusInternalServerError)
		return
	}
	if err := s.publishStorageMutationEventPlan(r.Context(), plan); err != nil {
		s.writeError(w, openrtcerr.CodeInternal, err.Error(), "", http.StatusInternalServerError)
		return
	}
	writeRawJSON(w, http.StatusOK, plan.Mutation.Document)
}

func (s *Service) handleDeleteStorage(w http.ResponseWriter, r *http.Request, room string) {
	claims, err := s.authenticate(r)
	if err != nil {
		s.writeError(w, openrtcerr.CodeAuthInvalid, "invalid bearer token", "", http.StatusUnauthorized)
		return
	}
	if s.store == nil {
		s.writeError(w, openrtcerr.CodeInternal, "storage APIs require redis backing", "", http.StatusServiceUnavailable)
		return
	}
	if !s.allowsRoomAction(r.Context(), claims, "storage:write", room) {
		s.writeError(w, openrtcerr.CodeRoomForbidden, "room storage is not permitted", "", http.StatusForbidden)
		return
	}

	plan, err := s.deleteStorageMutationEventPlan(r.Context(), room)
	if errors.Is(err, cluster.ErrStorageNotFound) {
		s.writeError(w, openrtcerr.CodeStorageNotFound, "storage document not found", "", http.StatusNotFound)
		return
	}
	if err != nil {
		s.writeError(w, openrtcerr.CodeInternal, err.Error(), "", http.StatusInternalServerError)
		return
	}
	if err := s.publishStorageMutationEventPlan(r.Context(), plan); err != nil {
		s.writeError(w, openrtcerr.CodeInternal, err.Error(), "", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) handleGetRoom(w http.ResponseWriter, r *http.Request, room string) {
	claims, err := s.authenticate(r)
	if err != nil {
		s.writeError(w, openrtcerr.CodeAuthInvalid, "invalid bearer token", "", http.StatusUnauthorized)
		return
	}
	if s.store == nil {
		s.writeError(w, openrtcerr.CodeInternal, "room APIs require redis backing", "", http.StatusServiceUnavailable)
		return
	}
	if !claims.Allows("rooms", room, s.cfg.Tenant.EnforcePrefix, s.cfg.Tenant.Separator) {
		s.writeError(w, openrtcerr.CodeRoomForbidden, "room administration is not permitted", "", http.StatusForbidden)
		return
	}

	record, err := s.store.GetRoom(r.Context(), room)
	if errors.Is(err, cluster.ErrRoomNotFound) {
		s.writeError(w, openrtcerr.CodeRoomNotFound, "room not found", "", http.StatusNotFound)
		return
	}
	if err != nil {
		s.writeError(w, openrtcerr.CodeInternal, err.Error(), "", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, record)
}

func (s *Service) handleUpdateRoom(w http.ResponseWriter, r *http.Request, room string) {
	claims, err := s.authenticate(r)
	if err != nil {
		s.writeError(w, openrtcerr.CodeAuthInvalid, "invalid bearer token", "", http.StatusUnauthorized)
		return
	}
	if s.store == nil {
		s.writeError(w, openrtcerr.CodeInternal, "room APIs require redis backing", "", http.StatusServiceUnavailable)
		return
	}
	if !claims.Allows("rooms", room, s.cfg.Tenant.EnforcePrefix, s.cfg.Tenant.Separator) {
		s.writeError(w, openrtcerr.CodeRoomForbidden, "room administration is not permitted", "", http.StatusForbidden)
		return
	}

	var request RoomUpdateRequest
	if err := decodeRequest(w, r, s.cfg.Limits.EnvelopeMaxBytes, &request); err != nil {
		s.writeError(w, openrtcerr.CodeBadRequest, "request body must be valid JSON", "", http.StatusBadRequest)
		return
	}
	update := cluster.RoomUpdate{}
	if request.Metadata != nil {
		if err := validateMetadata(*request.Metadata, false, s.cfg.Limits.PayloadMaxBytes); err != nil {
			s.writeError(w, err.Code, err.Message, "", openrtcerr.DescriptorFor(err.Code).HTTPStatus)
			return
		}
		update.Metadata = normalizedMetadata(*request.Metadata)
		update.MetadataSet = true
	}
	if request.DefaultAccesses != nil {
		update.DefaultAccesses = *request.DefaultAccesses
		update.DefaultAccessesSet = true
	}
	if request.UsersAccesses != nil {
		update.UsersAccesses = request.UsersAccesses
		update.UsersAccessesSet = true
	}
	if request.GroupsAccesses != nil {
		update.GroupsAccesses = request.GroupsAccesses
		update.GroupsAccessesSet = true
	}
	if err := validateRoomAccesses(update.DefaultAccesses, update.UsersAccesses, update.GroupsAccesses); err != nil {
		s.writeError(w, err.Code, err.Message, "", openrtcerr.DescriptorFor(err.Code).HTTPStatus)
		return
	}

	record, err := s.store.UpdateRoom(r.Context(), room, update)
	if errors.Is(err, cluster.ErrRoomNotFound) {
		s.writeError(w, openrtcerr.CodeRoomNotFound, "room not found", "", http.StatusNotFound)
		return
	}
	if err != nil {
		s.writeError(w, openrtcerr.CodeInternal, err.Error(), "", http.StatusInternalServerError)
		return
	}
	s.dispatchRoomWebhook(r.Context(), roomengine.RoomUpdated, record.ID, &record)
	writeJSON(w, http.StatusOK, record)
}

func (s *Service) handleDeleteRoom(w http.ResponseWriter, r *http.Request, room string) {
	claims, err := s.authenticate(r)
	if err != nil {
		s.writeError(w, openrtcerr.CodeAuthInvalid, "invalid bearer token", "", http.StatusUnauthorized)
		return
	}
	if s.store == nil {
		s.writeError(w, openrtcerr.CodeInternal, "room APIs require redis backing", "", http.StatusServiceUnavailable)
		return
	}
	if !claims.Allows("rooms", room, s.cfg.Tenant.EnforcePrefix, s.cfg.Tenant.Separator) {
		s.writeError(w, openrtcerr.CodeRoomForbidden, "room administration is not permitted", "", http.StatusForbidden)
		return
	}

	err = s.store.DeleteRoom(r.Context(), room)
	if errors.Is(err, cluster.ErrRoomNotFound) {
		s.writeError(w, openrtcerr.CodeRoomNotFound, "room not found", "", http.StatusNotFound)
		return
	}
	if err != nil {
		s.writeError(w, openrtcerr.CodeInternal, err.Error(), "", http.StatusInternalServerError)
		return
	}
	s.dispatchRoomWebhook(r.Context(), roomengine.RoomDeleted, room, nil)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) handleListRooms(w http.ResponseWriter, r *http.Request) {
	claims, err := s.authenticate(r)
	if err != nil {
		s.writeError(w, openrtcerr.CodeAuthInvalid, "invalid bearer token", "", http.StatusUnauthorized)
		return
	}
	if s.store == nil {
		s.writeError(w, openrtcerr.CodeInternal, "room APIs require redis backing", "", http.StatusServiceUnavailable)
		return
	}

	prefix, parseErr := s.authorizedRoomListPrefix(claims, r.URL.Query().Get("prefix"))
	if parseErr != nil {
		s.writeError(w, parseErr.Code, parseErr.Message, "", openrtcerr.DescriptorFor(parseErr.Code).HTTPStatus)
		return
	}
	limit, err := parseListLimit(r.URL.Query().Get("limit"))
	if err != nil {
		s.writeError(w, openrtcerr.CodeBadRequest, err.Error(), "", http.StatusBadRequest)
		return
	}
	cursor, err := parseCursor(r.URL.Query().Get("cursor"))
	if err != nil {
		s.writeError(w, openrtcerr.CodeBadRequest, err.Error(), "", http.StatusBadRequest)
		return
	}
	query, parseErr := parseRoomListQuery(r.URL.Query().Get("query"))
	if parseErr != nil {
		s.writeError(w, parseErr.Code, parseErr.Message, "", openrtcerr.DescriptorFor(parseErr.Code).HTTPStatus)
		return
	}

	list, err := s.listRooms(r.Context(), prefix, cursor, limit, query)
	if err != nil {
		s.writeError(w, openrtcerr.CodeInternal, err.Error(), "", http.StatusInternalServerError)
		return
	}
	response := roomListResponse{Rooms: list.Rooms}
	if list.NextCursor != 0 {
		response.NextCursor = strconv.FormatUint(list.NextCursor, 10)
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Service) listRooms(ctx context.Context, prefix string, cursor uint64, limit int, query roomListQuery) (cluster.RoomList, error) {
	if !query.Active() {
		return s.store.ListRooms(ctx, prefix, cursor, limit)
	}
	rooms := make([]cluster.RoomRecord, 0, limit)
	scanCursor := cursor
	nextCursor := uint64(0)
	for {
		list, err := s.store.ListRooms(ctx, prefix, scanCursor, limit)
		if err != nil {
			return cluster.RoomList{}, err
		}
		for _, room := range list.Rooms {
			if query.Matches(room) {
				rooms = append(rooms, room)
				if len(rooms) >= limit {
					break
				}
			}
		}
		nextCursor = list.NextCursor
		if len(rooms) >= limit || nextCursor == 0 || nextCursor == scanCursor {
			break
		}
		scanCursor = nextCursor
	}
	return cluster.RoomList{Rooms: rooms, NextCursor: nextCursor}, nil
}

func (s *Service) handleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	if _, err := s.authenticate(r); err != nil {
		s.writeError(w, openrtcerr.CodeAuthInvalid, "invalid bearer token", "", http.StatusUnauthorized)
		return
	}

	var snapshot stats.Snapshot
	if s.store != nil {
		var err error
		snapshot, err = s.store.AggregateStats(r.Context())
		if err != nil {
			s.writeError(w, openrtcerr.CodeInternal, err.Error(), "", http.StatusInternalServerError)
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(snapshot)
}

func (s *Service) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func (s *Service) handleReady(w http.ResponseWriter, _ *http.Request) {
	if s.cfg.Mode == config.ModeCluster && s.store != nil {
		if err := s.store.Healthy(context.Background()); err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ready"))
}

func (s *Service) authenticate(r *http.Request) (*auth.Claims, error) {
	if s.verifier == nil {
		return nil, errors.New("admin auth verifier is not configured")
	}
	authHeader := r.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Bearer ") {
		return nil, errors.New("missing bearer token")
	}
	return s.verifier.Verify(r.Context(), strings.TrimPrefix(authHeader, "Bearer "))
}

func roomFromPath(path string) (string, error) {
	room, subresource, err := roomPathParts(path)
	if err != nil {
		return "", err
	}
	if subresource != "" {
		return "", errors.New("unsupported room subresource")
	}
	return room, nil
}

func roomPathParts(path string) (string, string, error) {
	raw := strings.TrimPrefix(path, "/v1/rooms/")
	if raw == "" {
		return "", "", errors.New("room id is required")
	}
	parts := strings.SplitN(raw, "/", 2)
	room, err := url.PathUnescape(parts[0])
	if err != nil {
		return "", "", errors.New("room id must be URL-escaped")
	}
	if err := protocol.ValidateRoomName(room); err != nil {
		return "", "", err
	}
	subresource := ""
	if len(parts) == 2 {
		subresource = parts[1]
	}
	return room, subresource, nil
}

func threadPathParts(subresource string) (string, string, error) {
	raw := strings.TrimPrefix(subresource, "threads/")
	if raw == "" || raw == subresource {
		return "", "", errors.New("thread id is required")
	}
	parts := strings.SplitN(raw, "/", 2)
	threadID, err := url.PathUnescape(parts[0])
	if err != nil {
		return "", "", errors.New("thread id must be URL-escaped")
	}
	if err := protocol.ValidateConnectionID(threadID); err != nil {
		return "", "", err
	}
	child := ""
	if len(parts) == 2 {
		child = parts[1]
	}
	return threadID, child, nil
}

func commentPathPart(child string) (string, error) {
	raw := strings.TrimPrefix(child, "comments/")
	if raw == "" || raw == child {
		return "", errors.New("comment id is required")
	}
	if strings.Contains(raw, "/") {
		return "", errors.New("unsupported comment subresource")
	}
	commentID, err := url.PathUnescape(raw)
	if err != nil {
		return "", errors.New("comment id must be URL-escaped")
	}
	if err := protocol.ValidateConnectionID(commentID); err != nil {
		return "", err
	}
	return commentID, nil
}

func roomUserPathParts(subresource string) (string, string, error) {
	raw := strings.TrimPrefix(subresource, "users/")
	if raw == "" || raw == subresource {
		return "", "", errors.New("user id is required")
	}
	parts := strings.SplitN(raw, "/", 2)
	userID, err := url.PathUnescape(parts[0])
	if err != nil {
		return "", "", errors.New("user id must be URL-escaped")
	}
	if err := protocol.ValidateConnectionID(userID); err != nil {
		return "", "", err
	}
	child := ""
	if len(parts) == 2 {
		child = parts[1]
	}
	return userID, child, nil
}

func userPathParts(path string) (string, string, string, error) {
	raw := strings.TrimPrefix(path, "/v1/users/")
	if raw == "" || raw == path {
		return "", "", "", errors.New("user id is required")
	}
	parts := strings.Split(raw, "/")
	userID, err := url.PathUnescape(parts[0])
	if err != nil {
		return "", "", "", errors.New("user id must be URL-escaped")
	}
	if err := protocol.ValidateConnectionID(userID); err != nil {
		return "", "", "", err
	}
	if len(parts) < 2 || parts[1] == "" {
		return "", "", "", errors.New("user subresource is required")
	}
	subresource := parts[1]
	itemID := ""
	if len(parts) >= 3 {
		itemID, err = url.PathUnescape(parts[2])
		if err != nil {
			return "", "", "", errors.New("subresource id must be URL-escaped")
		}
		if err := protocol.ValidateConnectionID(itemID); err != nil {
			return "", "", "", err
		}
	}
	if len(parts) > 3 {
		return "", "", "", errors.New("unsupported user path")
	}
	return userID, subresource, itemID, nil
}

func inboxNotificationActionParts(path string) (string, string, error) {
	raw := strings.TrimPrefix(path, "/v1/inbox-notifications/")
	if raw == "" || raw == path {
		return "", "", errors.New("inbox notification id is required")
	}
	parts := strings.SplitN(raw, "/", 2)
	notificationID, err := url.PathUnescape(parts[0])
	if err != nil {
		return "", "", errors.New("inbox notification id must be URL-escaped")
	}
	if err := protocol.ValidateConnectionID(notificationID); err != nil {
		return "", "", err
	}
	child := ""
	if len(parts) == 2 {
		child = parts[1]
	}
	return notificationID, child, nil
}

func validateRoomRequest(room string, metadata json.RawMessage, metadataOptional bool, maxBytes int) *protocol.ParseError {
	if err := protocol.ValidateRoomName(room); err != nil {
		return err.(*protocol.ParseError)
	}
	return validateMetadata(metadata, metadataOptional, maxBytes)
}

func validateThreadRequest(threadID string, metadata json.RawMessage, comment CommentCreateRequest, maxBytes int) *protocol.ParseError {
	if err := protocol.ValidateConnectionID(threadID); err != nil {
		return err.(*protocol.ParseError)
	}
	if err := validateMetadata(metadata, true, maxBytes); err != nil {
		return err
	}
	return validateCommentRequest(comment, maxBytes)
}

func validateThreadUpdateRequest(request ThreadUpdateRequest, maxBytes int) *protocol.ParseError {
	if request.Metadata == nil && request.Resolved == nil {
		return &protocol.ParseError{Code: openrtcerr.CodeBadRequest, Message: "thread update must include metadata or resolved"}
	}
	if request.Metadata != nil {
		if err := validateMetadata(*request.Metadata, false, maxBytes); err != nil {
			err.Message = "thread metadata " + err.Message
			return err
		}
	}
	return nil
}

func validateInboxNotificationRequest(request InboxNotificationTriggerRequest, maxBytes int) *protocol.ParseError {
	if err := protocol.ValidateConnectionID(request.ID); err != nil {
		return err.(*protocol.ParseError)
	}
	if err := protocol.ValidateConnectionID(request.UserID); err != nil {
		return err.(*protocol.ParseError)
	}
	if err := validateNotificationKind(request.Kind); err != nil {
		return err
	}
	if request.SubjectID != "" {
		if err := protocol.ValidateConnectionID(request.SubjectID); err != nil {
			return err.(*protocol.ParseError)
		}
	}
	if request.ThreadID != "" {
		if err := protocol.ValidateConnectionID(request.ThreadID); err != nil {
			return err.(*protocol.ParseError)
		}
	}
	if request.RoomID != "" {
		if err := protocol.ValidateRoomName(request.RoomID); err != nil {
			return err.(*protocol.ParseError)
		}
	}
	if err := validateActivityData(request.ActivityData, true, maxBytes); err != nil {
		return err
	}
	return nil
}

func validateNotificationKind(kind string) *protocol.ParseError {
	if kind == "" || len(kind) > protocol.MaxEventNameBytes {
		return &protocol.ParseError{Code: openrtcerr.CodeBadRequest, Message: "notification kind must be 1-128 characters"}
	}
	if kind[0] != '$' {
		if err := protocol.ValidateEventName(kind); err != nil {
			parseErr := err.(*protocol.ParseError)
			parseErr.Message = "notification kind must be safe ASCII"
			return parseErr
		}
		return nil
	}
	if len(kind) == 1 {
		return &protocol.ParseError{Code: openrtcerr.CodeBadRequest, Message: "custom notification kind is required after $"}
	}
	for _, r := range kind[1:] {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r == '_':
		default:
			return &protocol.ParseError{Code: openrtcerr.CodeBadRequest, Message: "custom notification kind must contain only letters and underscores"}
		}
	}
	return nil
}

func validateActivityData(data json.RawMessage, optional bool, maxBytes int) *protocol.ParseError {
	if len(data) == 0 {
		if optional {
			return nil
		}
		return &protocol.ParseError{Code: openrtcerr.CodeBadRequest, Message: "activityData is required"}
	}
	if len(data) > maxBytes {
		return &protocol.ParseError{Code: openrtcerr.CodePayloadTooLarge, Message: "activityData exceeds max size"}
	}
	var values map[string]any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&values); err != nil {
		return &protocol.ParseError{Code: openrtcerr.CodeBadRequest, Message: "activityData must be valid JSON"}
	}
	if values == nil {
		return &protocol.ParseError{Code: openrtcerr.CodeBadRequest, Message: "activityData must be a JSON object"}
	}
	for _, value := range values {
		switch value.(type) {
		case string, bool, json.Number, nil:
		default:
			return &protocol.ParseError{Code: openrtcerr.CodeBadRequest, Message: "activityData values must be scalar"}
		}
	}
	return nil
}

func validateRoomSubscriptionSettingsRequest(request RoomSubscriptionSettingsRequest) *protocol.ParseError {
	if request.Threads != "" {
		switch request.Threads {
		case "all", "replies_and_mentions", "none":
		default:
			return &protocol.ParseError{Code: openrtcerr.CodeBadRequest, Message: "threads must be all, replies_and_mentions, or none"}
		}
	}
	if request.TextMentions != "" {
		switch request.TextMentions {
		case "mine", "none":
		default:
			return &protocol.ParseError{Code: openrtcerr.CodeBadRequest, Message: "textMentions must be mine or none"}
		}
	}
	return nil
}

func validateCommentRequest(comment CommentCreateRequest, maxBytes int) *protocol.ParseError {
	if err := protocol.ValidateConnectionID(comment.ID); err != nil {
		return err.(*protocol.ParseError)
	}
	if err := protocol.ValidateConnectionID(comment.UserID); err != nil {
		return err.(*protocol.ParseError)
	}
	if err := validateMetadata(comment.Body, false, maxBytes); err != nil {
		err.Message = "comment body " + err.Message
		return err
	}
	if err := validateMetadata(comment.Metadata, true, maxBytes); err != nil {
		err.Message = "comment metadata " + err.Message
		return err
	}
	if err := validateCommentMentions(comment.Mentions); err != nil {
		return err
	}
	if err := validateCommentReactions(comment.Reactions); err != nil {
		return err
	}
	return nil
}

func validateCommentUpdateRequest(request CommentUpdateRequest, maxBytes int) *protocol.ParseError {
	if request.Body == nil && request.Metadata == nil && request.Mentions == nil && request.Reactions == nil {
		return &protocol.ParseError{Code: openrtcerr.CodeBadRequest, Message: "comment update must include body, metadata, mentions, or reactions"}
	}
	if request.Body != nil {
		if err := validateMetadata(*request.Body, false, maxBytes); err != nil {
			err.Message = "comment body " + err.Message
			return err
		}
	}
	if request.Metadata != nil {
		if err := validateMetadata(*request.Metadata, false, maxBytes); err != nil {
			err.Message = "comment metadata " + err.Message
			return err
		}
	}
	if request.Mentions != nil {
		if err := validateCommentMentions(*request.Mentions); err != nil {
			return err
		}
	}
	if request.Reactions != nil {
		if err := validateCommentReactions(*request.Reactions); err != nil {
			return err
		}
	}
	return nil
}

func validateCommentMentions(mentions []string) *protocol.ParseError {
	if len(mentions) > maxCommentMentions {
		return &protocol.ParseError{Code: openrtcerr.CodeBadRequest, Message: "comment mentions support at most 100 user ids"}
	}
	for _, userID := range mentions {
		if err := protocol.ValidateConnectionID(userID); err != nil {
			parseErr := err.(*protocol.ParseError)
			parseErr.Message = "comment mentions must contain safe user ids"
			return parseErr
		}
	}
	return nil
}

func validateCommentReactions(reactions []cluster.CommentReaction) *protocol.ParseError {
	if len(reactions) > maxCommentReactions {
		return &protocol.ParseError{Code: openrtcerr.CodeBadRequest, Message: "comment reactions support at most 500 entries"}
	}
	for _, reaction := range reactions {
		if strings.TrimSpace(reaction.Emoji) == "" || len(reaction.Emoji) > maxCommentReactionEmojiBytes {
			return &protocol.ParseError{Code: openrtcerr.CodeBadRequest, Message: "comment reaction emoji must be 1-64 bytes"}
		}
		if err := protocol.ValidateConnectionID(reaction.UserID); err != nil {
			parseErr := err.(*protocol.ParseError)
			parseErr.Message = "comment reaction userId must be safe ASCII"
			return parseErr
		}
	}
	return nil
}

func validateMetadata(metadata json.RawMessage, optional bool, maxBytes int) *protocol.ParseError {
	if len(metadata) == 0 {
		if optional {
			return nil
		}
		return &protocol.ParseError{Code: openrtcerr.CodeBadRequest, Message: "metadata is required"}
	}
	if len(metadata) > maxBytes {
		return &protocol.ParseError{Code: openrtcerr.CodePayloadTooLarge, Message: "metadata exceeds max size"}
	}
	if !json.Valid(metadata) || metadata[0] != '{' {
		return &protocol.ParseError{Code: openrtcerr.CodeBadRequest, Message: "metadata must be a JSON object"}
	}
	return nil
}

func validateRoomAccesses(defaultAccesses []string, usersAccesses map[string][]string, groupsAccesses map[string][]string) *protocol.ParseError {
	if err := validateAccessList(defaultAccesses); err != nil {
		return err
	}
	if err := validateAccessMap(usersAccesses, "usersAccesses"); err != nil {
		return err
	}
	if err := validateAccessMap(groupsAccesses, "groupsAccesses"); err != nil {
		return err
	}
	return nil
}

func validateAccessMap(accesses map[string][]string, field string) *protocol.ParseError {
	if len(accesses) > 1000 {
		return &protocol.ParseError{Code: openrtcerr.CodeBadRequest, Message: field + " supports at most 1000 ids"}
	}
	for id, permissions := range accesses {
		if id == "" || len(id) > protocol.MaxRoomNameBytes {
			return &protocol.ParseError{Code: openrtcerr.CodeBadRequest, Message: field + " ids must be 1-256 characters"}
		}
		if err := validateAccessList(permissions); err != nil {
			return err
		}
	}
	return nil
}

func validateAccessList(permissions []string) *protocol.ParseError {
	for _, permission := range permissions {
		switch permission {
		case cluster.PermissionRoomWrite,
			cluster.PermissionRoomRead,
			cluster.PermissionRoomPresenceWrite,
			cluster.PermissionStorageWrite,
			cluster.PermissionStorageRead,
			cluster.PermissionCommentsWrite,
			cluster.PermissionCommentsRead,
			cluster.PermissionFeedsWrite,
			cluster.PermissionFeedsRead:
		default:
			return &protocol.ParseError{Code: openrtcerr.CodeBadRequest, Message: "unsupported room access permission"}
		}
	}
	return nil
}

func normalizedMetadata(metadata json.RawMessage) json.RawMessage {
	if len(metadata) == 0 {
		return json.RawMessage(`{}`)
	}
	return append(json.RawMessage(nil), metadata...)
}

func commentRecordFromRequest(room string, threadID string, request CommentCreateRequest) cluster.CommentRecord {
	return cluster.CommentRecord{
		ID:        request.ID,
		ThreadID:  threadID,
		RoomID:    room,
		UserID:    request.UserID,
		Body:      normalizedMetadata(request.Body),
		Metadata:  normalizedMetadata(request.Metadata),
		Mentions:  request.Mentions,
		Reactions: request.Reactions,
	}
}

func commentUpdateFromRequest(request CommentUpdateRequest) cluster.CommentUpdate {
	update := cluster.CommentUpdate{}
	if request.Body != nil {
		update.Body = normalizedMetadata(*request.Body)
		update.BodySet = true
	}
	if request.Metadata != nil {
		update.Metadata = normalizedMetadata(*request.Metadata)
		update.MetadataSet = true
	}
	if request.Mentions != nil {
		update.Mentions = append([]string(nil), (*request.Mentions)...)
		update.MentionsSet = true
	}
	if request.Reactions != nil {
		update.Reactions = append([]cluster.CommentReaction(nil), (*request.Reactions)...)
		update.ReactionsSet = true
	}
	return update
}

func threadUpdateFromRequest(request ThreadUpdateRequest) cluster.ThreadUpdate {
	update := cluster.ThreadUpdate{}
	if request.Metadata != nil {
		update.Metadata = normalizedMetadata(*request.Metadata)
		update.MetadataSet = true
	}
	if request.Resolved != nil {
		update.Resolved = *request.Resolved
		update.ResolvedSet = true
	}
	return update
}

func (s *Service) dispatchRoomWebhook(ctx context.Context, eventName string, roomID string, room *cluster.RoomRecord) {
	_, payload, err := roomengine.NewRoomEvent(eventName, roomID, room, roomengine.EventOptions{
		OriginNode: "admin:" + s.cfg.NodeID,
	})
	if err != nil {
		s.logWebhookDelivery("marshal", eventName, 0, err)
		return
	}
	s.dispatchWebhook(ctx, eventName, payload)
}

func (s *Service) publishCommentEvent(ctx context.Context, eventName string, thread cluster.ThreadRecord, comment *cluster.CommentRecord) error {
	if s.store == nil {
		return nil
	}
	event, payload, err := roomengine.NewCommentEvent(eventName, thread, comment, roomengine.EventOptions{
		OriginNode: "admin:" + s.cfg.NodeID,
	})
	if err != nil {
		return err
	}
	if _, err := s.store.PublishEvent(ctx, event); err != nil {
		return err
	}
	s.dispatchWebhook(ctx, eventName, payload)
	return nil
}

func (s *Service) publishNotificationEvent(ctx context.Context, eventName string, userID string, notification *cluster.InboxNotificationRecord) error {
	if s.store == nil {
		return nil
	}
	event, payload, err := roomengine.NewNotificationEvent(eventName, userID, notification, roomengine.EventOptions{
		OriginNode: "admin:" + s.cfg.NodeID,
	})
	if err != nil {
		return err
	}
	if _, err := s.store.PublishEvent(ctx, event); err != nil {
		return err
	}
	s.dispatchWebhook(ctx, eventName, payload)
	return nil
}

func (s *Service) setStorageMutationEventPlan(ctx context.Context, room string, document json.RawMessage) (roomengine.StorageMutationEventPlan, error) {
	if sequenced, ok := s.store.(cluster.SequencedStorageWriter); ok {
		stored, sequence, err := sequenced.SetStorageWithOptions(ctx, room, document, cluster.StorageWriteOptions{
			MaxBytes: s.cfg.Limits.PayloadMaxBytes,
		})
		if err != nil {
			return roomengine.StorageMutationEventPlan{}, err
		}
		return roomengine.NewStorageMutationEventPlan(room, roomengine.StorageMutationSet, stored, nil, roomengine.StorageMutationOptions{
			Sequence: sequence,
		}, roomengine.StorageEventOptions{
			OriginNode: "admin:" + s.cfg.NodeID,
			Sequence:   sequence,
		})
	}
	stored, err := s.store.SetStorage(ctx, room, document)
	if err != nil {
		return roomengine.StorageMutationEventPlan{}, err
	}
	return roomengine.NewStorageMutationEventPlan(room, roomengine.StorageMutationSet, stored, nil, roomengine.StorageMutationOptions{}, roomengine.StorageEventOptions{
		OriginNode: "admin:" + s.cfg.NodeID,
	})
}

func (s *Service) applyStoragePatchMutationEventPlan(ctx context.Context, room string, operations []cluster.JSONPatchOperation) (roomengine.StorageMutationEventPlan, error) {
	if sequenced, ok := s.store.(cluster.SequencedStorageWriter); ok {
		document, sequence, err := sequenced.ApplyStoragePatchWithOptions(ctx, room, operations, cluster.StorageWriteOptions{
			MaxBytes: s.cfg.Limits.PayloadMaxBytes,
		})
		if err != nil {
			return roomengine.StorageMutationEventPlan{}, err
		}
		return roomengine.NewStorageMutationEventPlan(room, roomengine.StorageMutationPatch, document, operations, roomengine.StorageMutationOptions{
			Sequence: sequence,
		}, roomengine.StorageEventOptions{
			OriginNode: "admin:" + s.cfg.NodeID,
			Sequence:   sequence,
		})
	}
	document, err := s.store.ApplyStoragePatch(ctx, room, operations, s.cfg.Limits.PayloadMaxBytes)
	if err != nil {
		return roomengine.StorageMutationEventPlan{}, err
	}
	return roomengine.NewStorageMutationEventPlan(room, roomengine.StorageMutationPatch, document, operations, roomengine.StorageMutationOptions{}, roomengine.StorageEventOptions{
		OriginNode: "admin:" + s.cfg.NodeID,
	})
}

func (s *Service) deleteStorageMutationEventPlan(ctx context.Context, room string) (roomengine.StorageMutationEventPlan, error) {
	sequence := uint64(0)
	if sequenced, ok := s.store.(cluster.SequencedStorageDeleter); ok {
		deletedSequence, err := sequenced.DeleteStorageWithOptions(ctx, room, cluster.StorageDeleteOptions{})
		if err != nil {
			return roomengine.StorageMutationEventPlan{}, err
		}
		sequence = deletedSequence
	} else if err := s.store.DeleteStorage(ctx, room); err != nil {
		return roomengine.StorageMutationEventPlan{}, err
	}
	return roomengine.NewStorageMutationEventPlan(room, roomengine.StorageMutationDelete, nil, nil, roomengine.StorageMutationOptions{
		Sequence: sequence,
	}, roomengine.StorageEventOptions{
		OriginNode: "admin:" + s.cfg.NodeID,
		Sequence:   sequence,
	})
}

func (s *Service) publishStorageMutationEventPlan(ctx context.Context, plan roomengine.StorageMutationEventPlan) error {
	_, err := s.store.PublishEvent(ctx, plan.Event)
	return err
}

func firstThreadComment(thread cluster.ThreadRecord) *cluster.CommentRecord {
	if len(thread.Comments) == 0 {
		return nil
	}
	comment := thread.Comments[0]
	return &comment
}

func findThreadComment(thread cluster.ThreadRecord, commentID string) *cluster.CommentRecord {
	for _, comment := range thread.Comments {
		if comment.ID == commentID {
			found := comment
			return &found
		}
	}
	return nil
}

func newRecordID(prefix string) string {
	raw := make([]byte, 12)
	if _, err := randomRead(raw); err != nil {
		panic(err)
	}
	return prefix + "_" + hex.EncodeToString(raw)
}

func (s *Service) authorizedRoomListPrefix(claims *auth.Claims, requested string) (string, *protocol.ParseError) {
	prefix := requested
	if prefix == "" && s.cfg.Tenant.EnforcePrefix {
		if claims.Tenant == "" {
			return "", &protocol.ParseError{Code: openrtcerr.CodeRoomForbidden, Message: "tenant claim is required for room listing"}
		}
		prefix = claims.Tenant + s.cfg.Tenant.Separator
	}
	if err := protocol.ValidateRoomPrefix(prefix); err != nil {
		return "", err.(*protocol.ParseError)
	}
	if len(prefix)+len("__probe") > protocol.MaxRoomNameBytes {
		return "", &protocol.ParseError{Code: openrtcerr.CodeBadRequest, Message: "room prefix is too long"}
	}
	probeRoom := prefix + "__probe"
	if !claims.Allows("rooms", probeRoom, s.cfg.Tenant.EnforcePrefix, s.cfg.Tenant.Separator) {
		return "", &protocol.ParseError{Code: openrtcerr.CodeRoomForbidden, Message: "room administration is not permitted"}
	}
	return prefix, nil
}

func parseListLimit(raw string) (int, error) {
	if raw == "" {
		return 50, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 || limit > 200 {
		return 0, errors.New("limit must be an integer between 1 and 200")
	}
	return limit, nil
}

func parseThreadListLimit(raw string, cursor string) (int, error) {
	if raw == "" {
		if cursor == "" {
			return 0, nil
		}
		return 50, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 || limit > 200 {
		return 0, errors.New("limit must be an integer between 1 and 200")
	}
	return limit, nil
}

func parseCursor(raw string) (uint64, error) {
	if raw == "" {
		return 0, nil
	}
	cursor, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return 0, errors.New("cursor must be an unsigned integer")
	}
	return cursor, nil
}

func parseRoomListQuery(raw string) (roomListQuery, *protocol.ParseError) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return roomListQuery{}, nil
	}
	if len(raw) > maxRoomListQueryBytes {
		return roomListQuery{}, &protocol.ParseError{Code: openrtcerr.CodeBadRequest, Message: "room query must be at most 1024 bytes"}
	}
	parts, parseErr := splitRoomListQuery(raw)
	if parseErr != nil {
		return roomListQuery{}, parseErr
	}
	if len(parts) > maxRoomListQueryClauses {
		return roomListQuery{}, &protocol.ParseError{Code: openrtcerr.CodeBadRequest, Message: "room query supports at most 20 clauses"}
	}
	query := roomListQuery{Clauses: make([]roomListQueryClause, 0, len(parts))}
	for _, part := range parts {
		clause, parseErr := parseRoomListQueryClause(part)
		if parseErr != nil {
			return roomListQuery{}, parseErr
		}
		query.Clauses = append(query.Clauses, clause)
	}
	return query, nil
}

func parseThreadListQuery(raw string) (threadListQuery, *protocol.ParseError) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return threadListQuery{}, nil
	}
	if len(raw) > maxRoomListQueryBytes {
		return threadListQuery{}, &protocol.ParseError{Code: openrtcerr.CodeBadRequest, Message: "thread query must be at most 1024 bytes"}
	}
	parts, parseErr := splitRoomListQuery(raw)
	if parseErr != nil {
		return threadListQuery{}, parseErr
	}
	if len(parts) > maxRoomListQueryClauses {
		return threadListQuery{}, &protocol.ParseError{Code: openrtcerr.CodeBadRequest, Message: "thread query supports at most 20 clauses"}
	}
	query := threadListQuery{Clauses: make([]threadListQueryClause, 0, len(parts))}
	for _, part := range parts {
		clause, parseErr := parseThreadListQueryClause(part)
		if parseErr != nil {
			return threadListQuery{}, parseErr
		}
		query.Clauses = append(query.Clauses, clause)
	}
	return query, nil
}

func splitRoomListQuery(raw string) ([]string, *protocol.ParseError) {
	parts := make([]string, 0)
	start := -1
	inQuote := false
	escaped := false
	for index := 0; index < len(raw); index++ {
		ch := raw[index]
		if start == -1 {
			if isRoomQuerySpace(ch) {
				continue
			}
			start = index
		}
		if escaped {
			escaped = false
			continue
		}
		if ch == '\\' && inQuote {
			escaped = true
			continue
		}
		if ch == '"' {
			inQuote = !inQuote
			continue
		}
		if isRoomQuerySpace(ch) && !inQuote {
			parts = append(parts, raw[start:index])
			start = -1
		}
	}
	if escaped || inQuote {
		return nil, &protocol.ParseError{Code: openrtcerr.CodeBadRequest, Message: "room query contains an unterminated quoted value"}
	}
	if start != -1 {
		parts = append(parts, raw[start:])
	}
	if len(parts) == 0 {
		return nil, &protocol.ParseError{Code: openrtcerr.CodeBadRequest, Message: "room query must include at least one clause"}
	}
	return parts, nil
}

func parseRoomListQueryClause(raw string) (roomListQueryClause, *protocol.ParseError) {
	field, value, ok := strings.Cut(raw, ":")
	if !ok || field == "" || value == "" {
		return roomListQueryClause{}, &protocol.ParseError{Code: openrtcerr.CodeBadRequest, Message: "room query clauses must use field:value"}
	}
	if field == "id" {
		idValue, parseErr := parseRoomQueryStringValue(value)
		if parseErr != nil {
			return roomListQueryClause{}, parseErr
		}
		return roomListQueryClause{Field: roomListQueryFieldID, IDContains: idValue}, nil
	}
	path, parseErr := parseRoomQueryMetadataPath(field)
	if parseErr != nil {
		return roomListQueryClause{}, parseErr
	}
	if value == "*" {
		return roomListQueryClause{Field: roomListQueryFieldMetadata, MetadataPath: path, Exists: true}, nil
	}
	parsedValue, parseErr := parseRoomQueryScalarValue(value)
	if parseErr != nil {
		return roomListQueryClause{}, parseErr
	}
	return roomListQueryClause{Field: roomListQueryFieldMetadata, MetadataPath: path, Value: parsedValue}, nil
}

func parseThreadListQueryClause(raw string) (threadListQueryClause, *protocol.ParseError) {
	field, value, ok := strings.Cut(raw, ":")
	if !ok || field == "" || value == "" {
		return threadListQueryClause{}, &protocol.ParseError{Code: openrtcerr.CodeBadRequest, Message: "thread query clauses must use field:value"}
	}
	if field == "resolved" {
		if value == "*" {
			return threadListQueryClause{}, &protocol.ParseError{Code: openrtcerr.CodeBadRequest, Message: "resolved thread query does not support wildcard exists"}
		}
		parsedValue, parseErr := parseRoomQueryScalarValue(value)
		if parseErr != nil {
			return threadListQueryClause{}, parseErr
		}
		resolved, ok := parsedValue.(bool)
		if !ok {
			return threadListQueryClause{}, &protocol.ParseError{Code: openrtcerr.CodeBadRequest, Message: "resolved thread query value must be true or false"}
		}
		return threadListQueryClause{Field: threadListQueryFieldResolved, Resolved: resolved}, nil
	}
	path, parseErr := parseThreadQueryMetadataPath(field)
	if parseErr != nil {
		return threadListQueryClause{}, parseErr
	}
	if value == "*" {
		return threadListQueryClause{Field: threadListQueryFieldMetadata, MetadataPath: path, Exists: true}, nil
	}
	parsedValue, parseErr := parseRoomQueryScalarValue(value)
	if parseErr != nil {
		return threadListQueryClause{}, parseErr
	}
	return threadListQueryClause{Field: threadListQueryFieldMetadata, MetadataPath: path, Value: parsedValue}, nil
}

func parseRoomQueryStringValue(raw string) (string, *protocol.ParseError) {
	if raw == "*" {
		return "", &protocol.ParseError{Code: openrtcerr.CodeBadRequest, Message: "id room query does not support wildcard exists"}
	}
	if strings.HasPrefix(raw, "\"") {
		var value string
		if err := json.Unmarshal([]byte(raw), &value); err != nil {
			return "", &protocol.ParseError{Code: openrtcerr.CodeBadRequest, Message: "room query string value must be valid JSON string syntax"}
		}
		if value == "" {
			return "", &protocol.ParseError{Code: openrtcerr.CodeBadRequest, Message: "room query string value must not be empty"}
		}
		return value, nil
	}
	return raw, nil
}

func parseRoomQueryScalarValue(raw string) (any, *protocol.ParseError) {
	if strings.HasPrefix(raw, "\"") {
		var value string
		if err := json.Unmarshal([]byte(raw), &value); err != nil {
			return nil, &protocol.ParseError{Code: openrtcerr.CodeBadRequest, Message: "room query string value must be valid JSON string syntax"}
		}
		return value, nil
	}
	switch raw {
	case "true":
		return true, nil
	case "false":
		return false, nil
	case "null":
		return nil, nil
	}
	if strings.HasPrefix(raw, "{") || strings.HasPrefix(raw, "[") {
		return nil, &protocol.ParseError{Code: openrtcerr.CodeBadRequest, Message: "room query values must be scalar"}
	}
	if number, err := strconv.ParseFloat(raw, 64); err == nil {
		return number, nil
	}
	return raw, nil
}

func parseRoomQueryMetadataPath(raw string) ([]string, *protocol.ParseError) {
	if strings.HasPrefix(raw, "metadata.") {
		path := strings.Split(strings.TrimPrefix(raw, "metadata."), ".")
		return validateRoomQueryMetadataPath(path)
	}
	if strings.HasPrefix(raw, "metadata[") {
		return parseRoomQueryBracketPath(raw)
	}
	return nil, &protocol.ParseError{Code: openrtcerr.CodeBadRequest, Message: "room query fields must be id or metadata paths"}
}

func parseThreadQueryMetadataPath(raw string) ([]string, *protocol.ParseError) {
	if strings.HasPrefix(raw, "metadata.") {
		path := strings.Split(strings.TrimPrefix(raw, "metadata."), ".")
		return validateRoomQueryMetadataPath(path)
	}
	if strings.HasPrefix(raw, "metadata[") {
		return parseRoomQueryBracketPath(raw)
	}
	return nil, &protocol.ParseError{Code: openrtcerr.CodeBadRequest, Message: "thread query fields must be resolved or metadata paths"}
}

func parseRoomQueryBracketPath(raw string) ([]string, *protocol.ParseError) {
	rest := strings.TrimPrefix(raw, "metadata")
	path := make([]string, 0, 2)
	for rest != "" {
		if !strings.HasPrefix(rest, "[\"") {
			return nil, &protocol.ParseError{Code: openrtcerr.CodeBadRequest, Message: "metadata bracket paths must use metadata[\"key\"] syntax"}
		}
		rest = strings.TrimPrefix(rest, "[\"")
		end := strings.Index(rest, "\"]")
		if end < 0 {
			return nil, &protocol.ParseError{Code: openrtcerr.CodeBadRequest, Message: "metadata bracket paths must use metadata[\"key\"] syntax"}
		}
		path = append(path, rest[:end])
		rest = rest[end+2:]
	}
	return validateRoomQueryMetadataPath(path)
}

func validateRoomQueryMetadataPath(path []string) ([]string, *protocol.ParseError) {
	if len(path) == 0 || len(path) > maxRoomListQueryPathDepth {
		return nil, &protocol.ParseError{Code: openrtcerr.CodeBadRequest, Message: "metadata query path depth must be between 1 and 8"}
	}
	for _, key := range path {
		if !isRoomQueryPathKey(key) {
			return nil, &protocol.ParseError{Code: openrtcerr.CodeBadRequest, Message: "metadata query path keys must be 1-64 characters using letters, numbers, underscore, or dash"}
		}
	}
	return path, nil
}

func (q roomListQuery) Active() bool {
	return len(q.Clauses) > 0
}

func (q roomListQuery) Matches(room cluster.RoomRecord) bool {
	for _, clause := range q.Clauses {
		if !clause.Matches(room) {
			return false
		}
	}
	return true
}

func (q threadListQuery) Active() bool {
	return len(q.Clauses) > 0
}

func (q threadListQuery) Matches(thread cluster.ThreadRecord) bool {
	for _, clause := range q.Clauses {
		if !clause.Matches(thread) {
			return false
		}
	}
	return true
}

func (c roomListQueryClause) Matches(room cluster.RoomRecord) bool {
	switch c.Field {
	case roomListQueryFieldID:
		return strings.Contains(room.ID, c.IDContains)
	case roomListQueryFieldMetadata:
		value, ok := roomMetadataValue(room.Metadata, c.MetadataPath)
		if c.Exists {
			return ok
		}
		return ok && roomQueryScalarEqual(value, c.Value)
	default:
		return false
	}
}

func (c threadListQueryClause) Matches(thread cluster.ThreadRecord) bool {
	switch c.Field {
	case threadListQueryFieldResolved:
		return thread.Resolved == c.Resolved
	case threadListQueryFieldMetadata:
		value, ok := roomMetadataValue(thread.Metadata, c.MetadataPath)
		if c.Exists {
			return ok
		}
		return ok && roomQueryScalarEqual(value, c.Value)
	default:
		return false
	}
}

func filterThreads(threads []cluster.ThreadRecord, query threadListQuery) []cluster.ThreadRecord {
	if !query.Active() {
		return threads
	}
	filtered := make([]cluster.ThreadRecord, 0, len(threads))
	for _, thread := range threads {
		if query.Matches(thread) {
			filtered = append(filtered, thread)
		}
	}
	return filtered
}

func paginateThreads(threads []cluster.ThreadRecord, cursor uint64, limit int) ([]cluster.ThreadRecord, uint64) {
	if cursor >= uint64(len(threads)) {
		return []cluster.ThreadRecord{}, 0
	}
	start := int(cursor)
	if limit <= 0 {
		return threads[start:], 0
	}
	end := start + limit
	if end >= len(threads) {
		return threads[start:], 0
	}
	return threads[start:end], uint64(end)
}

func roomMetadataValue(metadata json.RawMessage, path []string) (any, bool) {
	if len(metadata) == 0 || !json.Valid(metadata) {
		return nil, false
	}
	var current any
	if err := json.Unmarshal(metadata, &current); err != nil {
		return nil, false
	}
	for _, key := range path {
		object, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = object[key]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

func roomQueryScalarEqual(actual any, expected any) bool {
	switch actualValue := actual.(type) {
	case string:
		expectedValue, ok := expected.(string)
		return ok && actualValue == expectedValue
	case float64:
		expectedValue, ok := expected.(float64)
		return ok && actualValue == expectedValue
	case bool:
		expectedValue, ok := expected.(bool)
		return ok && actualValue == expectedValue
	case nil:
		return expected == nil
	default:
		return false
	}
}

func isRoomQuerySpace(ch byte) bool {
	return ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r'
}

func isRoomQueryPathKey(key string) bool {
	if key == "" || len(key) > maxRoomListQueryPathKeyBytes {
		return false
	}
	for index := 0; index < len(key); index++ {
		ch := key[index]
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_' || ch == '-' {
			continue
		}
		return false
	}
	return true
}

func parseNotificationListLimit(raw string) (int, error) {
	if raw == "" {
		return 50, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 || limit > 50 {
		return 0, errors.New("limit must be an integer between 1 and 50")
	}
	return limit, nil
}

func unreadOnlyQuery(query string, unread string) bool {
	if strings.EqualFold(unread, "true") {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(query), "unread:true")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func (s *Service) notificationAllowed(claims *auth.Claims, userID string) bool {
	return claims.Allows("notifications", userID, false, s.cfg.Tenant.Separator)
}

func readNotificationSettings(w http.ResponseWriter, r *http.Request, maxBytes int) (json.RawMessage, *protocol.ParseError) {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, int64(maxBytes)))
	if err != nil {
		return nil, &protocol.ParseError{Code: openrtcerr.CodePayloadTooLarge, Message: "notification settings exceed max size"}
	}
	if !isJSONObject(raw) {
		return nil, &protocol.ParseError{Code: openrtcerr.CodeBadRequest, Message: "notification settings must be a JSON object"}
	}
	var compacted bytes.Buffer
	if err := json.Compact(&compacted, raw); err != nil {
		return nil, &protocol.ParseError{Code: openrtcerr.CodeBadRequest, Message: "notification settings must be valid JSON"}
	}
	return json.RawMessage(compacted.Bytes()), nil
}

func readStorageDocument(w http.ResponseWriter, r *http.Request, maxBytes int) (json.RawMessage, *protocol.ParseError) {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, int64(maxBytes)))
	if err != nil {
		return nil, &protocol.ParseError{Code: openrtcerr.CodePayloadTooLarge, Message: "storage document exceeds max size"}
	}
	if !isJSONObject(raw) {
		return nil, &protocol.ParseError{Code: openrtcerr.CodeBadRequest, Message: "storage document must be a JSON object"}
	}
	var compacted bytes.Buffer
	if err := json.Compact(&compacted, raw); err != nil {
		return nil, &protocol.ParseError{Code: openrtcerr.CodeBadRequest, Message: "storage document must be valid JSON"}
	}
	document := json.RawMessage(compacted.Bytes())
	if err := cluster.ValidateStorageDocument(document); err != nil {
		return nil, &protocol.ParseError{Code: openrtcerr.CodeBadRequest, Message: err.Error()}
	}
	return document, nil
}

func decodeStoragePatch(w http.ResponseWriter, r *http.Request, maxBytes int) ([]cluster.JSONPatchOperation, *protocol.ParseError) {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, int64(maxBytes)))
	decoder.DisallowUnknownFields()
	var operations []cluster.JSONPatchOperation
	if err := decoder.Decode(&operations); err != nil {
		return nil, &protocol.ParseError{Code: openrtcerr.CodeBadRequest, Message: "storage patch must be a valid JSON Patch array"}
	}
	if len(operations) == 0 || len(operations) > maxStoragePatchOperations {
		return nil, &protocol.ParseError{Code: openrtcerr.CodeBadRequest, Message: "storage patch must contain 1-100 operations"}
	}
	for _, operation := range operations {
		if operation.Op == "" {
			return nil, &protocol.ParseError{Code: openrtcerr.CodeBadRequest, Message: "storage patch operation requires op"}
		}
		if operation.Op != "add" && operation.Op != "remove" && operation.Op != "replace" && operation.Op != "test" && operation.Op != "copy" && operation.Op != "move" {
			return nil, &protocol.ParseError{Code: openrtcerr.CodePatchFailed, Message: "unsupported storage patch operation"}
		}
		if operation.Path == "" && operation.Op == "remove" {
			return nil, &protocol.ParseError{Code: openrtcerr.CodePatchFailed, Message: "removing storage root is not supported"}
		}
	}
	return operations, nil
}

func isJSONObject(raw []byte) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && trimmed[0] == '{'
}

func decodeRequest(w http.ResponseWriter, r *http.Request, maxBytes int, dest any) error {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, int64(maxBytes)))
	decoder.DisallowUnknownFields()
	return decoder.Decode(dest)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeRawJSON(w http.ResponseWriter, status int, payload json.RawMessage) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(payload)
}

func (s *Service) writeError(w http.ResponseWriter, code openrtcerr.Code, message string, requestID string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(openrtcerr.APIError{
		Code:      code,
		Message:   message,
		RequestID: requestID,
	})
}
