package cluster

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/openrtc/openrtc/server/internal/stats"
)

const (
	aliveTTL           = 45 * time.Second
	eventLogMaxEntries = 1000
)

const (
	EventStorageUpdate = "$openrtc.storage.update"
)

var (
	ErrRoomAlreadyExists   = errors.New("room already exists")
	ErrRoomNotFound        = errors.New("room not found")
	ErrStorageNotFound     = errors.New("storage not found")
	ErrStoragePatch        = errors.New("storage patch failed")
	ErrThreadAlreadyExists = errors.New("thread already exists")
	ErrThreadNotFound      = errors.New("thread not found")
	ErrCommentNotFound     = errors.New("comment not found")
	ErrInboxAlreadyExists  = errors.New("inbox notification already exists")
	ErrInboxNotFound       = errors.New("inbox notification not found")
)

const (
	PermissionRoomWrite         = "room:write"
	PermissionRoomRead          = "room:read"
	PermissionRoomPresenceWrite = "room:presence:write"
	PermissionStorageWrite      = "storage:write"
	PermissionStorageRead       = "storage:read"
	PermissionCommentsWrite     = "comments:write"
	PermissionCommentsRead      = "comments:read"
	PermissionFeedsWrite        = "feeds:write"
	PermissionFeedsRead         = "feeds:read"
)

const (
	storageTypeKey = "liveblocksType"
	storageDataKey = "data"

	storageTypeLiveObject = "LiveObject"
	storageTypeLiveList   = "LiveList"
	storageTypeLiveMap    = "LiveMap"
)

type PublishedEvent struct {
	Room                string          `json:"room"`
	Event               string          `json:"event"`
	Payload             json.RawMessage `json:"payload"`
	ExcludeSenderConnID string          `json:"exclude_sender_conn_id,omitempty"`
	TraceID             string          `json:"trace_id,omitempty"`
	Sequence            uint64          `json:"seq,omitempty"`
	OriginNode          string          `json:"origin_node"`
}

type PresenceEvent struct {
	Room       string          `json:"room"`
	ConnID     string          `json:"conn_id"`
	State      json.RawMessage `json:"state,omitempty"`
	Offline    bool            `json:"offline,omitempty"`
	OriginNode string          `json:"origin_node"`
}

type YJSEventKind byte

const (
	YJSEventUpdate             YJSEventKind = 1
	YJSEventSnapshot           YJSEventKind = 2
	YJSEventStateVectorRequest YJSEventKind = 3
	YJSEventStateVectorDiff    YJSEventKind = 4
	YJSEventSubdocUpdate       YJSEventKind = 5
	YJSEventSubdocStateVector  YJSEventKind = 6
	YJSEventSubdocDiff         YJSEventKind = 7
)

type YJSEvent struct {
	Room         string       `json:"room"`
	Kind         YJSEventKind `json:"kind"`
	Update       []byte       `json:"update"`
	Sequence     int64        `json:"sequence,omitempty"`
	OriginNode   string       `json:"origin_node"`
	OriginConnID string       `json:"origin_conn_id,omitempty"`
}

type ConnectionMeta struct {
	NodeID      string
	Subject     string
	Tenant      string
	ConnectedAt time.Time
}

type Snapshot struct {
	Members  []string
	Presence map[string]json.RawMessage
}

type ActiveUser struct {
	Type         string          `json:"type"`
	ConnectionID string          `json:"connection_id"`
	ID           string          `json:"id"`
	Tenant       string          `json:"tenant,omitempty"`
	NodeID       string          `json:"node_id,omitempty"`
	ConnectedAt  time.Time       `json:"connected_at,omitempty"`
	Presence     json.RawMessage `json:"presence,omitempty"`
}

type YJSDocument struct {
	Snapshot           []byte
	SnapshotCheckpoint int64
	Updates            [][]byte
	UpdateSequences    []int64
	UpdateKinds        []YJSEventKind
}

type RoomRecord struct {
	ID              string              `json:"id"`
	Metadata        json.RawMessage     `json:"metadata"`
	DefaultAccesses []string            `json:"defaultAccesses"`
	UsersAccesses   map[string][]string `json:"usersAccesses"`
	GroupsAccesses  map[string][]string `json:"groupsAccesses"`
	CreatedAt       time.Time           `json:"created_at"`
	UpdatedAt       time.Time           `json:"updated_at"`
}

type RoomList struct {
	Rooms      []RoomRecord `json:"rooms"`
	NextCursor uint64       `json:"next_cursor,omitempty"`
}

type RoomUpdate struct {
	Metadata           json.RawMessage
	MetadataSet        bool
	DefaultAccesses    []string
	DefaultAccessesSet bool
	UsersAccesses      map[string][]string
	UsersAccessesSet   bool
	GroupsAccesses     map[string][]string
	GroupsAccessesSet  bool
}

type ThreadRecord struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`
	RoomID    string          `json:"roomId"`
	Comments  []CommentRecord `json:"comments"`
	Resolved  bool            `json:"resolved"`
	Metadata  json.RawMessage `json:"metadata"`
	CreatedAt time.Time       `json:"createdAt"`
	UpdatedAt time.Time       `json:"updatedAt"`
}

type CommentRecord struct {
	Type      string            `json:"type"`
	ThreadID  string            `json:"threadId"`
	RoomID    string            `json:"roomId"`
	ID        string            `json:"id"`
	UserID    string            `json:"userId"`
	CreatedAt time.Time         `json:"createdAt"`
	EditedAt  *time.Time        `json:"editedAt,omitempty"`
	DeletedAt *time.Time        `json:"deletedAt,omitempty"`
	Body      json.RawMessage   `json:"body"`
	Metadata  json.RawMessage   `json:"metadata"`
	Mentions  []string          `json:"mentions,omitempty"`
	Reactions []CommentReaction `json:"reactions,omitempty"`
}

type CommentReaction struct {
	Emoji  string `json:"emoji"`
	UserID string `json:"userId"`
}

type CommentUpdate struct {
	Body         json.RawMessage
	BodySet      bool
	Metadata     json.RawMessage
	MetadataSet  bool
	Mentions     []string
	MentionsSet  bool
	Reactions    []CommentReaction
	ReactionsSet bool
}

type InboxNotificationRecord struct {
	ID           string          `json:"id"`
	UserID       string          `json:"userId"`
	Kind         string          `json:"kind"`
	SubjectID    string          `json:"subjectId,omitempty"`
	ThreadID     string          `json:"threadId,omitempty"`
	RoomID       string          `json:"roomId,omitempty"`
	ReadAt       *time.Time      `json:"readAt,omitempty"`
	NotifiedAt   time.Time       `json:"notifiedAt"`
	ActivityData json.RawMessage `json:"activityData,omitempty"`
}

type InboxNotificationList struct {
	Data       []InboxNotificationRecord `json:"data"`
	NextCursor uint64                    `json:"next_cursor,omitempty"`
}

type InboxNotificationListFilter struct {
	UnreadOnly bool
	Cursor     uint64
	Limit      int
}

type RoomSubscriptionSettings struct {
	RoomID       string    `json:"roomId"`
	UserID       string    `json:"userId"`
	Threads      string    `json:"threads"`
	TextMentions string    `json:"textMentions"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

type RoomSubscriptionSettingsList struct {
	Data       []RoomSubscriptionSettings `json:"data"`
	NextCursor uint64                     `json:"next_cursor,omitempty"`
}

type JSONPatchOperation struct {
	Op    string          `json:"op"`
	Path  string          `json:"path"`
	From  string          `json:"from,omitempty"`
	Value json.RawMessage `json:"value,omitempty"`
}

type yjsUpdateRecord struct {
	Seq    int64  `json:"seq"`
	Kind   byte   `json:"kind,omitempty"`
	Update []byte `json:"update"`
}

func (r yjsUpdateRecord) KindValue() YJSEventKind {
	if r.Kind == 0 {
		return YJSEventUpdate
	}
	return YJSEventKind(r.Kind)
}

type yjsSnapshotRecord struct {
	CheckpointSeq int64  `json:"checkpoint_seq"`
	Snapshot      []byte `json:"snapshot"`
}

type PublishedEventList struct {
	Events []PublishedEvent
}

type Store interface {
	Healthy(ctx context.Context) error
	PublishEvent(ctx context.Context, event PublishedEvent) (PublishedEvent, error)
	ListPublishedEvents(ctx context.Context, room string, afterSequence uint64, limit int) (PublishedEventList, error)
	Subscribe(ctx context.Context, handler func(PublishedEvent)) error
	PublishPresence(ctx context.Context, event PresenceEvent) error
	SubscribePresence(ctx context.Context, handler func(PresenceEvent)) error
	PublishYJSEvent(ctx context.Context, event YJSEvent) error
	SubscribeYJSEvents(ctx context.Context, handler func(YJSEvent)) error
	TouchConnection(ctx context.Context, connID string, meta ConnectionMeta) error
	JoinRoom(ctx context.Context, connID string, room string) error
	LeaveRoom(ctx context.Context, connID string, room string) error
	SetPresence(ctx context.Context, connID string, room string, payload json.RawMessage) error
	SetEphemeralPresence(ctx context.Context, connID string, room string, payload json.RawMessage, ttl time.Duration) error
	ClearPresence(ctx context.Context, connID string, room string) error
	SnapshotRoom(ctx context.Context, room string) (Snapshot, error)
	ActiveUsers(ctx context.Context, room string) ([]ActiveUser, error)
	CreateRoom(ctx context.Context, room RoomRecord) (RoomRecord, error)
	GetRoom(ctx context.Context, room string) (RoomRecord, error)
	UpdateRoom(ctx context.Context, room string, update RoomUpdate) (RoomRecord, error)
	DeleteRoom(ctx context.Context, room string) error
	ListRooms(ctx context.Context, prefix string, cursor uint64, limit int) (RoomList, error)
	CreateThread(ctx context.Context, room string, thread ThreadRecord) (ThreadRecord, error)
	ListThreads(ctx context.Context, room string) ([]ThreadRecord, error)
	AddComment(ctx context.Context, room string, threadID string, comment CommentRecord) (ThreadRecord, error)
	UpdateComment(ctx context.Context, room string, threadID string, commentID string, update CommentUpdate) (ThreadRecord, error)
	CreateInboxNotification(ctx context.Context, notification InboxNotificationRecord) (InboxNotificationRecord, error)
	ListInboxNotifications(ctx context.Context, userID string, filter InboxNotificationListFilter) (InboxNotificationList, error)
	GetInboxNotification(ctx context.Context, userID string, notificationID string) (InboxNotificationRecord, error)
	MarkInboxNotificationRead(ctx context.Context, notificationID string) (InboxNotificationRecord, error)
	DeleteInboxNotification(ctx context.Context, userID string, notificationID string) error
	DeleteAllInboxNotifications(ctx context.Context, userID string) error
	GetNotificationSettings(ctx context.Context, userID string) (json.RawMessage, error)
	SetNotificationSettings(ctx context.Context, userID string, settings json.RawMessage) (json.RawMessage, error)
	DeleteNotificationSettings(ctx context.Context, userID string) error
	GetRoomSubscriptionSettings(ctx context.Context, room string, userID string) (RoomSubscriptionSettings, error)
	SetRoomSubscriptionSettings(ctx context.Context, settings RoomSubscriptionSettings) (RoomSubscriptionSettings, error)
	DeleteRoomSubscriptionSettings(ctx context.Context, room string, userID string) error
	ListRoomSubscriptionSettings(ctx context.Context, userID string, cursor uint64, limit int) (RoomSubscriptionSettingsList, error)
	GetStorage(ctx context.Context, room string) (json.RawMessage, error)
	SetStorage(ctx context.Context, room string, document json.RawMessage) (json.RawMessage, error)
	DeleteStorage(ctx context.Context, room string) error
	ApplyStoragePatch(ctx context.Context, room string, operations []JSONPatchOperation, maxBytes int) (json.RawMessage, error)
	LoadYJSDocument(ctx context.Context, room string) (YJSDocument, error)
	AppendYJSUpdate(ctx context.Context, room string, kind YJSEventKind, update []byte) (int64, error)
	StoreYJSSnapshot(ctx context.Context, room string, snapshot []byte) error
	CleanupConnection(ctx context.Context, nodeID string, connID string) error
	ReconcileNode(ctx context.Context, nodeID string) error
	SyncStats(ctx context.Context, nodeID string, snapshot stats.Snapshot) error
	AggregateStats(ctx context.Context) (stats.Snapshot, error)
	Close() error
}

type RedisStore struct {
	client        *redis.Client
	channelPrefix string
}

func NewRedisStore(redisURL string, channelPrefix string) (*RedisStore, error) {
	options, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, err
	}
	return &RedisStore{
		client:        redis.NewClient(options),
		channelPrefix: channelPrefix,
	}, nil
}

func (s *RedisStore) Healthy(ctx context.Context) error {
	return s.client.Ping(ctx).Err()
}

func (s *RedisStore) PublishEvent(ctx context.Context, event PublishedEvent) (PublishedEvent, error) {
	if event.Sequence == 0 {
		sequence, err := s.client.Incr(ctx, roomEventSequenceKey(event.Room)).Result()
		if err != nil {
			return PublishedEvent{}, err
		}
		event.Sequence = uint64(sequence)
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return PublishedEvent{}, err
	}
	logKey := roomEventLogKey(event.Room)
	pipe := s.client.TxPipeline()
	pipe.ZAdd(ctx, logKey, redis.Z{Score: float64(event.Sequence), Member: string(payload)})
	pipe.ZRemRangeByRank(ctx, logKey, 0, -eventLogMaxEntries-1)
	pipe.Publish(ctx, s.channelPrefix+event.Room, payload)
	if _, err := pipe.Exec(ctx); err != nil {
		return PublishedEvent{}, err
	}
	return event, nil
}

func (s *RedisStore) ListPublishedEvents(ctx context.Context, room string, afterSequence uint64, limit int) (PublishedEventList, error) {
	if limit <= 0 {
		limit = 100
	}
	if limit > eventLogMaxEntries {
		limit = eventLogMaxEntries
	}
	values, err := s.client.ZRangeByScore(ctx, roomEventLogKey(room), &redis.ZRangeBy{
		Min:   fmt.Sprintf("(%d", afterSequence),
		Max:   "+inf",
		Count: int64(limit),
	}).Result()
	if err != nil {
		return PublishedEventList{}, err
	}
	events := make([]PublishedEvent, 0, len(values))
	for _, value := range values {
		var event PublishedEvent
		if err := json.Unmarshal([]byte(value), &event); err != nil {
			continue
		}
		if event.Room == "" || event.Event == "" || event.Sequence == 0 {
			continue
		}
		events = append(events, event)
	}
	return PublishedEventList{Events: events}, nil
}

func (s *RedisStore) Subscribe(ctx context.Context, handler func(PublishedEvent)) error {
	pubsub := s.client.PSubscribe(ctx, s.channelPrefix+"*")
	if _, err := pubsub.Receive(ctx); err != nil {
		_ = pubsub.Close()
		return err
	}

	go closePubSubOnDone(ctx, pubsub)
	go func() {
		defer pubsub.Close()
		for message := range pubsub.Channel() {
			var event PublishedEvent
			if err := json.Unmarshal([]byte(message.Payload), &event); err != nil {
				continue
			}
			if event.Room == "" || event.Event == "" {
				continue
			}
			handler(event)
		}
	}()

	return nil
}

func (s *RedisStore) PublishPresence(ctx context.Context, event PresenceEvent) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}
	return s.client.Publish(ctx, s.channelPrefix+"presence:"+event.Room, payload).Err()
}

func (s *RedisStore) SubscribePresence(ctx context.Context, handler func(PresenceEvent)) error {
	pubsub := s.client.PSubscribe(ctx, s.channelPrefix+"presence:*")
	if _, err := pubsub.Receive(ctx); err != nil {
		_ = pubsub.Close()
		return err
	}

	go closePubSubOnDone(ctx, pubsub)
	go func() {
		defer pubsub.Close()
		for message := range pubsub.Channel() {
			var event PresenceEvent
			if err := json.Unmarshal([]byte(message.Payload), &event); err != nil {
				continue
			}
			if event.Room == "" || event.ConnID == "" {
				continue
			}
			handler(event)
		}
	}()

	return nil
}

func (s *RedisStore) PublishYJSEvent(ctx context.Context, event YJSEvent) error {
	payload, _ := json.Marshal(event)
	return s.client.Publish(ctx, s.channelPrefix+"yjs:"+event.Room, payload).Err()
}

func (s *RedisStore) SubscribeYJSEvents(ctx context.Context, handler func(YJSEvent)) error {
	pubsub := s.client.PSubscribe(ctx, s.channelPrefix+"yjs:*")
	if _, err := pubsub.Receive(ctx); err != nil {
		_ = pubsub.Close()
		return err
	}

	go closePubSubOnDone(ctx, pubsub)
	go func() {
		defer pubsub.Close()
		for message := range pubsub.Channel() {
			var event YJSEvent
			if err := json.Unmarshal([]byte(message.Payload), &event); err != nil {
				continue
			}
			if event.Room == "" || len(event.Update) == 0 {
				continue
			}
			handler(event)
		}
	}()

	return nil
}

func closePubSubOnDone(ctx context.Context, pubsub *redis.PubSub) {
	<-ctx.Done()
	_ = pubsub.Close()
}

func (s *RedisStore) TouchConnection(ctx context.Context, connID string, meta ConnectionMeta) error {
	pipe := s.client.TxPipeline()
	pipe.Set(ctx, connAliveKey(connID), "1", aliveTTL)
	pipe.HSet(ctx, connMetaKey(connID), map[string]any{
		"subject":      meta.Subject,
		"tenant":       meta.Tenant,
		"node":         meta.NodeID,
		"connected_at": meta.ConnectedAt.UTC().Format(time.RFC3339Nano),
	})
	pipe.SAdd(ctx, nodeConnsKey(meta.NodeID), connID)
	_, err := pipe.Exec(ctx)
	return err
}

func (s *RedisStore) JoinRoom(ctx context.Context, connID string, room string) error {
	pipe := s.client.TxPipeline()
	pipe.SAdd(ctx, roomMembersKey(room), connID)
	pipe.SAdd(ctx, connRoomsKey(connID), room)
	_, err := pipe.Exec(ctx)
	return err
}

func (s *RedisStore) LeaveRoom(ctx context.Context, connID string, room string) error {
	pipe := s.client.TxPipeline()
	pipe.SRem(ctx, roomMembersKey(room), connID)
	pipe.SRem(ctx, connRoomsKey(connID), room)
	pipe.HDel(ctx, roomPresenceKey(room), connID)
	_, err := pipe.Exec(ctx)
	return err
}

func (s *RedisStore) SetPresence(ctx context.Context, connID string, room string, payload json.RawMessage) error {
	return s.client.HSet(ctx, roomPresenceKey(room), connID, string(payload)).Err()
}

func (s *RedisStore) SetEphemeralPresence(ctx context.Context, connID string, room string, payload json.RawMessage, ttl time.Duration) error {
	pipe := s.client.TxPipeline()
	pipe.SAdd(ctx, roomMembersKey(room), connID)
	pipe.HSet(ctx, roomPresenceKey(room), connID, string(payload))
	pipe.SAdd(ctx, roomEphemeralPresenceKey(room), connID)
	pipe.Set(ctx, roomEphemeralPresenceAliveKey(room, connID), "1", ttl)
	_, err := pipe.Exec(ctx)
	return err
}

func (s *RedisStore) ClearPresence(ctx context.Context, connID string, room string) error {
	return s.client.HDel(ctx, roomPresenceKey(room), connID).Err()
}

func (s *RedisStore) SnapshotRoom(ctx context.Context, room string) (Snapshot, error) {
	pipe := s.client.TxPipeline()
	members := pipe.SMembers(ctx, roomMembersKey(room))
	presence := pipe.HGetAll(ctx, roomPresenceKey(room))
	ephemeral := pipe.SMembers(ctx, roomEphemeralPresenceKey(room))
	_, err := pipe.Exec(ctx)
	if err != nil && err != redis.Nil {
		return Snapshot{}, err
	}

	rawPresence := make(map[string]json.RawMessage, len(presence.Val()))
	for connID, state := range presence.Val() {
		rawPresence[connID] = json.RawMessage(state)
	}

	memberSet := make(map[string]struct{}, len(members.Val()))
	for _, connID := range members.Val() {
		memberSet[connID] = struct{}{}
	}
	for _, connID := range ephemeral.Val() {
		exists, err := s.client.Exists(ctx, roomEphemeralPresenceAliveKey(room, connID)).Result()
		if err != nil {
			return Snapshot{}, err
		}
		if exists == 0 {
			delete(rawPresence, connID)
			delete(memberSet, connID)
			pipe := s.client.TxPipeline()
			pipe.HDel(ctx, roomPresenceKey(room), connID)
			pipe.SRem(ctx, roomMembersKey(room), connID)
			pipe.SRem(ctx, roomEphemeralPresenceKey(room), connID)
			_, _ = pipe.Exec(ctx)
		}
	}

	filteredMembers := make([]string, 0, len(memberSet))
	for connID := range memberSet {
		filteredMembers = append(filteredMembers, connID)
	}

	return Snapshot{
		Members:  filteredMembers,
		Presence: rawPresence,
	}, nil
}

func (s *RedisStore) ActiveUsers(ctx context.Context, room string) ([]ActiveUser, error) {
	snapshot, err := s.SnapshotRoom(ctx, room)
	if err != nil {
		return nil, err
	}
	sort.Strings(snapshot.Members)

	users := make([]ActiveUser, 0, len(snapshot.Members))
	for _, connID := range snapshot.Members {
		values, err := s.client.HGetAll(ctx, connMetaKey(connID)).Result()
		if err != nil {
			return nil, err
		}

		user := ActiveUser{
			Type:         "user",
			ConnectionID: connID,
			ID:           defaultString(values["subject"], connID),
			Tenant:       values["tenant"],
			NodeID:       values["node"],
		}
		if connectedAt := values["connected_at"]; connectedAt != "" {
			if parsed, err := time.Parse(time.RFC3339Nano, connectedAt); err == nil {
				user.ConnectedAt = parsed.UTC()
			}
		}
		if presence, ok := snapshot.Presence[connID]; ok && json.Valid(presence) {
			user.Presence = append(json.RawMessage(nil), presence...)
		}
		users = append(users, user)
	}
	return users, nil
}

func (s *RedisStore) CreateRoom(ctx context.Context, room RoomRecord) (RoomRecord, error) {
	now := time.Now().UTC()
	room.CreatedAt = now
	room.UpdatedAt = now
	room = normalizeRoomRecord(room)
	key := roomRecordKey(room.ID)

	err := s.client.Watch(ctx, func(tx *redis.Tx) error {
		exists, err := tx.Exists(ctx, key).Result()
		if err != nil {
			return err
		}
		if exists > 0 {
			return ErrRoomAlreadyExists
		}
		_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
			pipe.HSet(ctx, key, encodeRoomRecord(room))
			return nil
		})
		return err
	}, key)
	if err != nil {
		return RoomRecord{}, err
	}
	return room, nil
}

func (s *RedisStore) GetRoom(ctx context.Context, room string) (RoomRecord, error) {
	record, err := s.loadRoomRecord(ctx, room)
	if err != nil {
		return RoomRecord{}, err
	}
	return record, nil
}

func (s *RedisStore) UpdateRoom(ctx context.Context, room string, update RoomUpdate) (RoomRecord, error) {
	key := roomRecordKey(room)
	updatedAt := time.Now().UTC()
	var updated RoomRecord

	err := s.client.Watch(ctx, func(tx *redis.Tx) error {
		values, err := tx.HGetAll(ctx, key).Result()
		if err != nil {
			return err
		}
		if len(values) == 0 {
			return ErrRoomNotFound
		}
		current, err := decodeRoomRecord(values)
		if err != nil {
			return err
		}
		if update.MetadataSet {
			current.Metadata = append(json.RawMessage(nil), update.Metadata...)
		}
		if update.DefaultAccessesSet {
			current.DefaultAccesses = append([]string(nil), update.DefaultAccesses...)
		}
		if update.UsersAccessesSet {
			current.UsersAccesses = cloneAccessMap(update.UsersAccesses)
		}
		if update.GroupsAccessesSet {
			current.GroupsAccesses = cloneAccessMap(update.GroupsAccesses)
		}
		current.UpdatedAt = updatedAt
		current = normalizeRoomRecord(current)
		_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
			pipe.HSet(ctx, key, encodeRoomRecord(current))
			return nil
		})
		if err != nil {
			return err
		}
		updated = current
		return nil
	}, key)
	if err != nil {
		return RoomRecord{}, err
	}
	return updated, nil
}

func (s *RedisStore) DeleteRoom(ctx context.Context, room string) error {
	deleted, err := s.client.Del(ctx, roomRecordKey(room)).Result()
	if err != nil {
		return err
	}
	if deleted == 0 {
		return ErrRoomNotFound
	}
	return nil
}

func (s *RedisStore) ListRooms(ctx context.Context, prefix string, cursor uint64, limit int) (RoomList, error) {
	if limit <= 0 {
		limit = 50
	}
	keys, nextCursor, err := s.client.Scan(ctx, cursor, roomRecordScanPattern(prefix), int64(limit)).Result()
	if err != nil {
		return RoomList{}, err
	}

	rooms := make([]RoomRecord, 0, len(keys))
	for _, key := range keys {
		room, ok := roomFromRecordKey(key)
		if !ok {
			continue
		}
		record, err := s.loadRoomRecord(ctx, room)
		if errors.Is(err, ErrRoomNotFound) {
			continue
		}
		if err != nil {
			return RoomList{}, err
		}
		rooms = append(rooms, record)
	}
	sort.Slice(rooms, func(i, j int) bool {
		return rooms[i].ID < rooms[j].ID
	})
	return RoomList{Rooms: rooms, NextCursor: nextCursor}, nil
}

func (s *RedisStore) CreateThread(ctx context.Context, room string, thread ThreadRecord) (ThreadRecord, error) {
	now := time.Now().UTC()
	thread.RoomID = room
	thread.CreatedAt = now
	thread.UpdatedAt = now
	for index := range thread.Comments {
		thread.Comments[index].RoomID = room
		thread.Comments[index].ThreadID = thread.ID
		if thread.Comments[index].CreatedAt.IsZero() {
			thread.Comments[index].CreatedAt = now
		}
	}
	thread = normalizeThreadRecord(thread)
	key := roomThreadKey(room, thread.ID)
	commentsKey := roomThreadCommentsKey(room, thread.ID)

	err := s.client.Watch(ctx, func(tx *redis.Tx) error {
		exists, err := tx.Exists(ctx, key).Result()
		if err != nil {
			return err
		}
		if exists > 0 {
			return ErrThreadAlreadyExists
		}
		comments := make([]any, 0, len(thread.Comments))
		for _, comment := range thread.Comments {
			raw, err := encodeCommentRecord(comment)
			if err != nil {
				return err
			}
			comments = append(comments, raw)
		}
		_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
			pipe.HSet(ctx, key, encodeThreadRecord(thread))
			pipe.SAdd(ctx, roomThreadsKey(room), thread.ID)
			if len(comments) > 0 {
				pipe.RPush(ctx, commentsKey, comments...)
			}
			return nil
		})
		return err
	}, key)
	if err != nil {
		return ThreadRecord{}, err
	}
	return thread, nil
}

func (s *RedisStore) ListThreads(ctx context.Context, room string) ([]ThreadRecord, error) {
	threadIDs, err := s.client.SMembers(ctx, roomThreadsKey(room)).Result()
	if err != nil && err != redis.Nil {
		return nil, err
	}
	threads := make([]ThreadRecord, 0, len(threadIDs))
	for _, threadID := range threadIDs {
		thread, err := s.loadThreadRecord(ctx, room, threadID)
		if errors.Is(err, ErrThreadNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		threads = append(threads, thread)
	}
	sort.Slice(threads, func(i, j int) bool {
		if threads[i].CreatedAt.Equal(threads[j].CreatedAt) {
			return threads[i].ID < threads[j].ID
		}
		return threads[i].CreatedAt.Before(threads[j].CreatedAt)
	})
	return threads, nil
}

func (s *RedisStore) AddComment(ctx context.Context, room string, threadID string, comment CommentRecord) (ThreadRecord, error) {
	key := roomThreadKey(room, threadID)
	commentsKey := roomThreadCommentsKey(room, threadID)
	now := time.Now().UTC()
	comment.RoomID = room
	comment.ThreadID = threadID
	if comment.CreatedAt.IsZero() {
		comment.CreatedAt = now
	}
	comment = normalizeCommentRecord(comment)

	rawComment, err := encodeCommentRecord(comment)
	if err != nil {
		return ThreadRecord{}, err
	}
	err = s.client.Watch(ctx, func(tx *redis.Tx) error {
		exists, err := tx.Exists(ctx, key).Result()
		if err != nil {
			return err
		}
		if exists == 0 {
			return ErrThreadNotFound
		}
		_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
			pipe.HSet(ctx, key, "updated_at", now.Format(time.RFC3339Nano))
			pipe.RPush(ctx, commentsKey, rawComment)
			return nil
		})
		return err
	}, key)
	if err != nil {
		return ThreadRecord{}, err
	}
	return s.loadThreadRecord(ctx, room, threadID)
}

func (s *RedisStore) UpdateComment(ctx context.Context, room string, threadID string, commentID string, update CommentUpdate) (ThreadRecord, error) {
	key := roomThreadKey(room, threadID)
	commentsKey := roomThreadCommentsKey(room, threadID)
	now := time.Now().UTC()

	err := s.client.Watch(ctx, func(tx *redis.Tx) error {
		exists, err := tx.Exists(ctx, key).Result()
		if err != nil {
			return err
		}
		if exists == 0 {
			return ErrThreadNotFound
		}
		rawComments, err := tx.LRange(ctx, commentsKey, 0, -1).Result()
		if err != nil && err != redis.Nil {
			return err
		}
		commentIndex := -1
		var comment CommentRecord
		for index, raw := range rawComments {
			decoded, err := decodeCommentRecord(raw)
			if err != nil {
				return err
			}
			if decoded.ID == commentID {
				commentIndex = index
				comment = decoded
				break
			}
		}
		if commentIndex < 0 {
			return ErrCommentNotFound
		}
		if update.BodySet {
			comment.Body = append(json.RawMessage(nil), update.Body...)
		}
		if update.MetadataSet {
			comment.Metadata = append(json.RawMessage(nil), update.Metadata...)
		}
		if update.MentionsSet {
			comment.Mentions = cloneStringList(update.Mentions)
		}
		if update.ReactionsSet {
			comment.Reactions = cloneCommentReactionList(update.Reactions)
		}
		comment.EditedAt = &now
		comment = normalizeCommentRecord(comment)

		rawComment, err := encodeCommentRecord(comment)
		if err != nil {
			return err
		}
		_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
			pipe.HSet(ctx, key, "updated_at", now.Format(time.RFC3339Nano))
			pipe.LSet(ctx, commentsKey, int64(commentIndex), rawComment)
			return nil
		})
		return err
	}, key, commentsKey)
	if err != nil {
		return ThreadRecord{}, err
	}
	return s.loadThreadRecord(ctx, room, threadID)
}

func (s *RedisStore) CreateInboxNotification(ctx context.Context, notification InboxNotificationRecord) (InboxNotificationRecord, error) {
	now := time.Now().UTC()
	if notification.NotifiedAt.IsZero() {
		notification.NotifiedAt = now
	}
	notification = normalizeInboxNotificationRecord(notification)
	key := inboxNotificationKey(notification.ID)
	score := float64(notification.NotifiedAt.UnixMilli())

	err := s.client.Watch(ctx, func(tx *redis.Tx) error {
		exists, err := tx.Exists(ctx, key).Result()
		if err != nil {
			return err
		}
		if exists > 0 {
			return ErrInboxAlreadyExists
		}
		_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
			pipe.HSet(ctx, key, encodeInboxNotificationRecord(notification))
			pipe.ZAdd(ctx, userInboxKey(notification.UserID), redis.Z{
				Score:  score,
				Member: notification.ID,
			})
			if notification.ReadAt == nil {
				pipe.ZAdd(ctx, userInboxUnreadKey(notification.UserID), redis.Z{
					Score:  score,
					Member: notification.ID,
				})
			}
			return nil
		})
		return err
	}, key)
	if err != nil {
		return InboxNotificationRecord{}, err
	}
	return notification, nil
}

func (s *RedisStore) ListInboxNotifications(ctx context.Context, userID string, filter InboxNotificationListFilter) (InboxNotificationList, error) {
	if filter.Limit <= 0 {
		filter.Limit = 50
	}
	key := userInboxKey(userID)
	if filter.UnreadOnly {
		key = userInboxUnreadKey(userID)
	}
	start := int64(filter.Cursor)
	stop := start + int64(filter.Limit)
	ids, err := s.client.ZRevRange(ctx, key, start, stop).Result()
	if err != nil && err != redis.Nil {
		return InboxNotificationList{}, err
	}
	limit := filter.Limit
	hasMore := len(ids) > limit
	if len(ids) > limit {
		ids = ids[:limit]
	}
	notifications := make([]InboxNotificationRecord, 0, len(ids))
	for _, id := range ids {
		notification, err := s.GetInboxNotification(ctx, userID, id)
		if errors.Is(err, ErrInboxNotFound) {
			continue
		}
		if err != nil {
			return InboxNotificationList{}, err
		}
		notifications = append(notifications, notification)
	}
	nextCursor := uint64(0)
	if hasMore {
		nextCursor = filter.Cursor + uint64(limit)
	}
	return InboxNotificationList{Data: notifications, NextCursor: nextCursor}, nil
}

func (s *RedisStore) GetInboxNotification(ctx context.Context, userID string, notificationID string) (InboxNotificationRecord, error) {
	notification, err := s.loadInboxNotification(ctx, notificationID)
	if err != nil {
		return InboxNotificationRecord{}, err
	}
	if userID != "" && notification.UserID != userID {
		return InboxNotificationRecord{}, ErrInboxNotFound
	}
	return notification, nil
}

func (s *RedisStore) MarkInboxNotificationRead(ctx context.Context, notificationID string) (InboxNotificationRecord, error) {
	key := inboxNotificationKey(notificationID)
	var notification InboxNotificationRecord
	now := time.Now().UTC()
	err := s.client.Watch(ctx, func(tx *redis.Tx) error {
		values, err := tx.HGetAll(ctx, key).Result()
		if err != nil {
			return err
		}
		if len(values) == 0 {
			return ErrInboxNotFound
		}
		notification, err = decodeInboxNotificationRecord(values)
		if err != nil {
			return err
		}
		if notification.ReadAt != nil {
			return nil
		}
		notification.ReadAt = &now
		_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
			pipe.HSet(ctx, key, "read_at", now.Format(time.RFC3339Nano))
			pipe.ZRem(ctx, userInboxUnreadKey(notification.UserID), notification.ID)
			return nil
		})
		return err
	}, key)
	if err != nil {
		return InboxNotificationRecord{}, err
	}
	return normalizeInboxNotificationRecord(notification), nil
}

func (s *RedisStore) DeleteInboxNotification(ctx context.Context, userID string, notificationID string) error {
	notification, err := s.GetInboxNotification(ctx, userID, notificationID)
	if err != nil {
		return err
	}
	_, err = s.client.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.Del(ctx, inboxNotificationKey(notificationID))
		pipe.ZRem(ctx, userInboxKey(userID), notification.ID)
		pipe.ZRem(ctx, userInboxUnreadKey(userID), notification.ID)
		return nil
	})
	return err
}

func (s *RedisStore) DeleteAllInboxNotifications(ctx context.Context, userID string) error {
	ids, err := s.client.ZRange(ctx, userInboxKey(userID), 0, -1).Result()
	if err != nil && err != redis.Nil {
		return err
	}
	_, err = s.client.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		for _, id := range ids {
			pipe.Del(ctx, inboxNotificationKey(id))
		}
		pipe.Del(ctx, userInboxKey(userID), userInboxUnreadKey(userID))
		return nil
	})
	return err
}

func (s *RedisStore) GetNotificationSettings(ctx context.Context, userID string) (json.RawMessage, error) {
	raw, err := s.client.Get(ctx, userNotificationSettingsKey(userID)).Result()
	if err == redis.Nil {
		return json.RawMessage(`{}`), nil
	}
	if err != nil {
		return nil, err
	}
	return json.RawMessage(raw), nil
}

func (s *RedisStore) SetNotificationSettings(ctx context.Context, userID string, settings json.RawMessage) (json.RawMessage, error) {
	compacted, err := compactJSON(settings)
	if err != nil {
		return nil, err
	}
	if err := s.client.Set(ctx, userNotificationSettingsKey(userID), string(compacted), 0).Err(); err != nil {
		return nil, err
	}
	return compacted, nil
}

func (s *RedisStore) DeleteNotificationSettings(ctx context.Context, userID string) error {
	return s.client.Del(ctx, userNotificationSettingsKey(userID)).Err()
}

func (s *RedisStore) GetRoomSubscriptionSettings(ctx context.Context, room string, userID string) (RoomSubscriptionSettings, error) {
	values, err := s.client.HGetAll(ctx, roomSubscriptionSettingsKey(room, userID)).Result()
	if err != nil {
		return RoomSubscriptionSettings{}, err
	}
	if len(values) == 0 {
		return defaultRoomSubscriptionSettings(room, userID), nil
	}
	return decodeRoomSubscriptionSettings(values)
}

func (s *RedisStore) SetRoomSubscriptionSettings(ctx context.Context, settings RoomSubscriptionSettings) (RoomSubscriptionSettings, error) {
	settings = normalizeRoomSubscriptionSettings(settings)
	if settings.UpdatedAt.IsZero() {
		settings.UpdatedAt = time.Now().UTC()
	}
	_, err := s.client.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.HSet(ctx, roomSubscriptionSettingsKey(settings.RoomID, settings.UserID), encodeRoomSubscriptionSettings(settings))
		pipe.ZAdd(ctx, userRoomSubscriptionSettingsKey(settings.UserID), redis.Z{
			Score:  float64(settings.UpdatedAt.UnixMilli()),
			Member: settings.RoomID,
		})
		return nil
	})
	if err != nil {
		return RoomSubscriptionSettings{}, err
	}
	return settings, nil
}

func (s *RedisStore) DeleteRoomSubscriptionSettings(ctx context.Context, room string, userID string) error {
	_, err := s.client.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.Del(ctx, roomSubscriptionSettingsKey(room, userID))
		pipe.ZRem(ctx, userRoomSubscriptionSettingsKey(userID), room)
		return nil
	})
	return err
}

func (s *RedisStore) ListRoomSubscriptionSettings(ctx context.Context, userID string, cursor uint64, limit int) (RoomSubscriptionSettingsList, error) {
	if limit <= 0 {
		limit = 50
	}
	start := int64(cursor)
	stop := start + int64(limit)
	rooms, err := s.client.ZRevRange(ctx, userRoomSubscriptionSettingsKey(userID), start, stop).Result()
	if err != nil && err != redis.Nil {
		return RoomSubscriptionSettingsList{}, err
	}
	hasMore := len(rooms) > limit
	if len(rooms) > limit {
		rooms = rooms[:limit]
	}
	settings := make([]RoomSubscriptionSettings, 0, len(rooms))
	for _, room := range rooms {
		record, err := s.GetRoomSubscriptionSettings(ctx, room, userID)
		if err != nil {
			return RoomSubscriptionSettingsList{}, err
		}
		settings = append(settings, record)
	}
	nextCursor := uint64(0)
	if hasMore {
		nextCursor = cursor + uint64(limit)
	}
	return RoomSubscriptionSettingsList{Data: settings, NextCursor: nextCursor}, nil
}

func (s *RedisStore) GetStorage(ctx context.Context, room string) (json.RawMessage, error) {
	raw, err := s.client.Get(ctx, roomStorageKey(room)).Result()
	if err == redis.Nil {
		return nil, ErrStorageNotFound
	}
	if err != nil {
		return nil, err
	}
	return json.RawMessage(raw), nil
}

func (s *RedisStore) SetStorage(ctx context.Context, room string, document json.RawMessage) (json.RawMessage, error) {
	compacted, err := compactJSON(document)
	if err != nil {
		return nil, err
	}
	if err := ValidateStorageDocument(compacted); err != nil {
		return nil, err
	}
	if err := s.client.Set(ctx, roomStorageKey(room), string(compacted), 0).Err(); err != nil {
		return nil, err
	}
	return compacted, nil
}

func (s *RedisStore) DeleteStorage(ctx context.Context, room string) error {
	deleted, err := s.client.Del(ctx, roomStorageKey(room)).Result()
	if err != nil {
		return err
	}
	if deleted == 0 {
		return ErrStorageNotFound
	}
	return nil
}

func (s *RedisStore) ApplyStoragePatch(ctx context.Context, room string, operations []JSONPatchOperation, maxBytes int) (json.RawMessage, error) {
	key := roomStorageKey(room)
	var patched json.RawMessage
	err := s.client.Watch(ctx, func(tx *redis.Tx) error {
		raw, err := tx.Get(ctx, key).Result()
		if err == redis.Nil {
			return ErrStorageNotFound
		}
		if err != nil {
			return err
		}
		result, err := ApplyJSONPatch(json.RawMessage(raw), operations)
		if err != nil {
			return err
		}
		if maxBytes > 0 && len(result) > maxBytes {
			return fmt.Errorf("%w: patched storage exceeds max size", ErrStoragePatch)
		}
		if err := ValidateStorageDocument(result); err != nil {
			return err
		}
		_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
			pipe.Set(ctx, key, string(result), 0)
			return nil
		})
		if err != nil {
			return err
		}
		patched = result
		return nil
	}, key)
	if err != nil {
		return nil, err
	}
	return patched, nil
}

func (s *RedisStore) LoadYJSDocument(ctx context.Context, room string) (YJSDocument, error) {
	doc := YJSDocument{}
	snapshotRecord, err := s.loadYJSSnapshotRecord(ctx, room)
	if err != nil {
		return YJSDocument{}, err
	}
	doc.Snapshot = snapshotRecord.Snapshot
	doc.SnapshotCheckpoint = snapshotRecord.CheckpointSeq

	legacyUpdates, err := s.client.LRange(ctx, roomYJSUpdatesKey(room), 0, -1).Result()
	if err != nil && err != redis.Nil {
		return YJSDocument{}, err
	}
	for _, update := range legacyUpdates {
		doc.Updates = append(doc.Updates, []byte(update))
		doc.UpdateSequences = append(doc.UpdateSequences, 0)
		doc.UpdateKinds = append(doc.UpdateKinds, YJSEventUpdate)
	}

	records, err := s.client.ZRangeByScore(ctx, roomYJSUpdatesV2Key(room), &redis.ZRangeBy{
		Min: fmt.Sprintf("(%d", doc.SnapshotCheckpoint),
		Max: "+inf",
	}).Result()
	if err != nil && err != redis.Nil {
		return YJSDocument{}, err
	}
	for _, raw := range records {
		record, err := decodeYJSUpdateRecord(raw)
		if err != nil || record.Seq <= doc.SnapshotCheckpoint {
			continue
		}
		doc.Updates = append(doc.Updates, append([]byte(nil), record.Update...))
		doc.UpdateSequences = append(doc.UpdateSequences, record.Seq)
		doc.UpdateKinds = append(doc.UpdateKinds, record.KindValue())
	}
	return doc, nil
}

func (s *RedisStore) AppendYJSUpdate(ctx context.Context, room string, kind YJSEventKind, update []byte) (int64, error) {
	if len(update) == 0 {
		return 0, errors.New("yjs update is required")
	}
	if !isDurableYJSEventKind(kind) {
		return 0, errors.New("unsupported durable yjs update kind")
	}
	seq, err := s.client.Incr(ctx, roomYJSSequenceKey(room)).Result()
	if err != nil {
		return 0, err
	}
	record := encodeYJSUpdateRecord(yjsUpdateRecord{
		Seq:    seq,
		Kind:   byte(kind),
		Update: append([]byte(nil), update...),
	})
	if err := s.client.ZAdd(ctx, roomYJSUpdatesV2Key(room), redis.Z{
		Score:  float64(seq),
		Member: record,
	}).Err(); err != nil {
		return 0, err
	}
	return seq, nil
}

func (s *RedisStore) StoreYJSSnapshot(ctx context.Context, room string, snapshot []byte) error {
	if len(snapshot) == 0 {
		return errors.New("snapshot is required")
	}
	current, err := s.loadYJSSnapshotRecord(ctx, room)
	if err != nil {
		return err
	}
	return s.storeYJSSnapshotRecord(ctx, room, yjsSnapshotRecord{
		CheckpointSeq: current.CheckpointSeq,
		Snapshot:      append([]byte(nil), snapshot...),
	})
}

func (s *RedisStore) CompactYJSDocument(ctx context.Context, room string, checkpointSeq int64, snapshot []byte) error {
	if checkpointSeq < 0 {
		return errors.New("checkpoint sequence must be non-negative")
	}
	if len(snapshot) == 0 {
		return errors.New("snapshot is required")
	}
	record := yjsSnapshotRecord{
		CheckpointSeq: checkpointSeq,
		Snapshot:      append([]byte(nil), snapshot...),
	}
	raw := encodeYJSSnapshotRecord(record)
	pipe := s.client.TxPipeline()
	pipe.Set(ctx, roomYJSSnapshotV2Key(room), raw, 0)
	pipe.ZRemRangeByScore(ctx, roomYJSUpdatesV2Key(room), "-inf", strconv.FormatInt(checkpointSeq, 10))
	_, err := pipe.Exec(ctx)
	return err
}

func (s *RedisStore) CleanupConnection(ctx context.Context, nodeID string, connID string) error {
	rooms, err := s.client.SMembers(ctx, connRoomsKey(connID)).Result()
	if err != nil && err != redis.Nil {
		return err
	}

	pipe := s.client.TxPipeline()
	for _, room := range rooms {
		pipe.SRem(ctx, roomMembersKey(room), connID)
		pipe.HDel(ctx, roomPresenceKey(room), connID)
	}
	pipe.Del(ctx, connAliveKey(connID), connMetaKey(connID), connRoomsKey(connID))
	pipe.SRem(ctx, nodeConnsKey(nodeID), connID)
	_, err = pipe.Exec(ctx)
	return err
}

func (s *RedisStore) ReconcileNode(ctx context.Context, nodeID string) error {
	connIDs, err := s.client.SMembers(ctx, nodeConnsKey(nodeID)).Result()
	if err != nil {
		return err
	}

	for _, connID := range connIDs {
		exists, err := s.client.Exists(ctx, connAliveKey(connID)).Result()
		if err != nil {
			return err
		}
		if exists == 0 {
			if err := s.CleanupConnection(ctx, nodeID, connID); err != nil {
				return err
			}
		}
	}

	return nil
}

func (s *RedisStore) SyncStats(ctx context.Context, nodeID string, snapshot stats.Snapshot) error {
	pipe := s.client.TxPipeline()
	pipe.SAdd(ctx, statsNodesKey(), nodeID)
	pipe.HSet(ctx, nodeStatsKey(nodeID), map[string]any{
		"active_connections":     snapshot.ActiveConnections,
		"active_rooms":           snapshot.ActiveRooms,
		"joins_total":            snapshot.JoinsTotal,
		"leaves_total":           snapshot.LeavesTotal,
		"events_total":           snapshot.EventsTotal,
		"presence_updates_total": snapshot.PresenceUpdatesTotal,
		"queue_overflows_total":  snapshot.QueueOverflowsTotal,
		"admin_publishes_total":  snapshot.AdminPublishesTotal,
	})
	_, err := pipe.Exec(ctx)
	return err
}

func (s *RedisStore) AggregateStats(ctx context.Context) (stats.Snapshot, error) {
	nodeIDs, err := s.client.SMembers(ctx, statsNodesKey()).Result()
	if err != nil && err != redis.Nil {
		return stats.Snapshot{}, err
	}

	total := stats.Snapshot{}
	for _, nodeID := range nodeIDs {
		values, err := s.client.HGetAll(ctx, nodeStatsKey(nodeID)).Result()
		if err != nil {
			return stats.Snapshot{}, err
		}

		snapshot := stats.Snapshot{
			ActiveConnections:    parseInt64(values["active_connections"]),
			ActiveRooms:          parseInt64(values["active_rooms"]),
			JoinsTotal:           parseInt64(values["joins_total"]),
			LeavesTotal:          parseInt64(values["leaves_total"]),
			EventsTotal:          parseInt64(values["events_total"]),
			PresenceUpdatesTotal: parseInt64(values["presence_updates_total"]),
			QueueOverflowsTotal:  parseInt64(values["queue_overflows_total"]),
			AdminPublishesTotal:  parseInt64(values["admin_publishes_total"]),
		}
		total.Merge(snapshot)
	}

	return total, nil
}

func (s *RedisStore) Close() error {
	return s.client.Close()
}

func (s *RedisStore) loadYJSSnapshotRecord(ctx context.Context, room string) (yjsSnapshotRecord, error) {
	raw, err := s.client.Get(ctx, roomYJSSnapshotV2Key(room)).Result()
	if err == nil {
		return decodeYJSSnapshotRecord(raw)
	}
	if err != redis.Nil {
		return yjsSnapshotRecord{}, err
	}

	legacy, err := s.client.Get(ctx, roomYJSSnapshotKey(room)).Result()
	if err == redis.Nil {
		return yjsSnapshotRecord{}, nil
	}
	if err != nil {
		return yjsSnapshotRecord{}, err
	}
	return yjsSnapshotRecord{Snapshot: []byte(legacy)}, nil
}

func (s *RedisStore) storeYJSSnapshotRecord(ctx context.Context, room string, record yjsSnapshotRecord) error {
	raw := encodeYJSSnapshotRecord(record)
	return s.client.Set(ctx, roomYJSSnapshotV2Key(room), raw, 0).Err()
}

func (s *RedisStore) loadRoomRecord(ctx context.Context, room string) (RoomRecord, error) {
	values, err := s.client.HGetAll(ctx, roomRecordKey(room)).Result()
	if err != nil {
		return RoomRecord{}, err
	}
	if len(values) == 0 {
		return RoomRecord{}, ErrRoomNotFound
	}
	return decodeRoomRecord(values)
}

func (s *RedisStore) loadThreadRecord(ctx context.Context, room string, threadID string) (ThreadRecord, error) {
	values, err := s.client.HGetAll(ctx, roomThreadKey(room, threadID)).Result()
	if err != nil {
		return ThreadRecord{}, err
	}
	if len(values) == 0 {
		return ThreadRecord{}, ErrThreadNotFound
	}
	thread, err := decodeThreadRecord(values)
	if err != nil {
		return ThreadRecord{}, err
	}
	rawComments, err := s.client.LRange(ctx, roomThreadCommentsKey(room, threadID), 0, -1).Result()
	if err != nil && err != redis.Nil {
		return ThreadRecord{}, err
	}
	thread.Comments = make([]CommentRecord, 0, len(rawComments))
	for _, raw := range rawComments {
		comment, err := decodeCommentRecord(raw)
		if err != nil {
			return ThreadRecord{}, err
		}
		thread.Comments = append(thread.Comments, comment)
	}
	return normalizeThreadRecord(thread), nil
}

func (s *RedisStore) loadInboxNotification(ctx context.Context, notificationID string) (InboxNotificationRecord, error) {
	values, err := s.client.HGetAll(ctx, inboxNotificationKey(notificationID)).Result()
	if err != nil {
		return InboxNotificationRecord{}, err
	}
	if len(values) == 0 {
		return InboxNotificationRecord{}, ErrInboxNotFound
	}
	return decodeInboxNotificationRecord(values)
}

func encodeRoomRecord(record RoomRecord) map[string]any {
	record = normalizeRoomRecord(record)
	usersAccesses, _ := json.Marshal(record.UsersAccesses)
	groupsAccesses, _ := json.Marshal(record.GroupsAccesses)
	defaultAccesses, _ := json.Marshal(record.DefaultAccesses)
	return map[string]any{
		"id":               record.ID,
		"metadata":         string(record.Metadata),
		"default_accesses": string(defaultAccesses),
		"users_accesses":   string(usersAccesses),
		"groups_accesses":  string(groupsAccesses),
		"created_at":       record.CreatedAt.UTC().Format(time.RFC3339Nano),
		"updated_at":       record.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func decodeRoomRecord(values map[string]string) (RoomRecord, error) {
	id := values["id"]
	metadata := values["metadata"]
	createdAt, err := time.Parse(time.RFC3339Nano, values["created_at"])
	if err != nil {
		return RoomRecord{}, err
	}
	updatedAt, err := time.Parse(time.RFC3339Nano, values["updated_at"])
	if err != nil {
		return RoomRecord{}, err
	}
	if metadata == "" {
		metadata = "{}"
	}
	record := RoomRecord{
		ID:        id,
		Metadata:  json.RawMessage(metadata),
		CreatedAt: createdAt.UTC(),
		UpdatedAt: updatedAt.UTC(),
	}
	if raw := values["default_accesses"]; raw != "" {
		if err := json.Unmarshal([]byte(raw), &record.DefaultAccesses); err != nil {
			return RoomRecord{}, err
		}
	}
	if raw := values["users_accesses"]; raw != "" {
		if err := json.Unmarshal([]byte(raw), &record.UsersAccesses); err != nil {
			return RoomRecord{}, err
		}
	}
	if raw := values["groups_accesses"]; raw != "" {
		if err := json.Unmarshal([]byte(raw), &record.GroupsAccesses); err != nil {
			return RoomRecord{}, err
		}
	}
	return normalizeRoomRecord(record), nil
}

func encodeThreadRecord(record ThreadRecord) map[string]any {
	record = normalizeThreadRecord(record)
	return map[string]any{
		"type":       record.Type,
		"id":         record.ID,
		"room_id":    record.RoomID,
		"resolved":   strconv.FormatBool(record.Resolved),
		"metadata":   string(record.Metadata),
		"created_at": record.CreatedAt.UTC().Format(time.RFC3339Nano),
		"updated_at": record.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func decodeThreadRecord(values map[string]string) (ThreadRecord, error) {
	createdAt, err := time.Parse(time.RFC3339Nano, values["created_at"])
	if err != nil {
		return ThreadRecord{}, err
	}
	updatedAt, err := time.Parse(time.RFC3339Nano, values["updated_at"])
	if err != nil {
		return ThreadRecord{}, err
	}
	resolved, err := strconv.ParseBool(defaultString(values["resolved"], "false"))
	if err != nil {
		return ThreadRecord{}, err
	}
	metadata := values["metadata"]
	if metadata == "" {
		metadata = "{}"
	}
	return normalizeThreadRecord(ThreadRecord{
		Type:      defaultString(values["type"], "thread"),
		ID:        values["id"],
		RoomID:    values["room_id"],
		Resolved:  resolved,
		Metadata:  json.RawMessage(metadata),
		CreatedAt: createdAt.UTC(),
		UpdatedAt: updatedAt.UTC(),
	}), nil
}

func encodeCommentRecord(record CommentRecord) (string, error) {
	record = normalizeCommentRecord(record)
	raw, err := json.Marshal(record)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func decodeCommentRecord(raw string) (CommentRecord, error) {
	var record CommentRecord
	if err := json.Unmarshal([]byte(raw), &record); err != nil {
		return CommentRecord{}, err
	}
	return normalizeCommentRecord(record), nil
}

func encodeInboxNotificationRecord(record InboxNotificationRecord) map[string]any {
	record = normalizeInboxNotificationRecord(record)
	readAt := ""
	if record.ReadAt != nil {
		readAt = record.ReadAt.UTC().Format(time.RFC3339Nano)
	}
	return map[string]any{
		"id":            record.ID,
		"user_id":       record.UserID,
		"kind":          record.Kind,
		"subject_id":    record.SubjectID,
		"thread_id":     record.ThreadID,
		"room_id":       record.RoomID,
		"read_at":       readAt,
		"notified_at":   record.NotifiedAt.UTC().Format(time.RFC3339Nano),
		"activity_data": string(record.ActivityData),
	}
}

func decodeInboxNotificationRecord(values map[string]string) (InboxNotificationRecord, error) {
	notifiedAt, err := time.Parse(time.RFC3339Nano, values["notified_at"])
	if err != nil {
		return InboxNotificationRecord{}, err
	}
	var readAt *time.Time
	if raw := values["read_at"]; raw != "" {
		parsed, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			return InboxNotificationRecord{}, err
		}
		parsed = parsed.UTC()
		readAt = &parsed
	}
	activityData := values["activity_data"]
	if activityData == "" {
		activityData = "{}"
	}
	return normalizeInboxNotificationRecord(InboxNotificationRecord{
		ID:           values["id"],
		UserID:       values["user_id"],
		Kind:         values["kind"],
		SubjectID:    values["subject_id"],
		ThreadID:     values["thread_id"],
		RoomID:       values["room_id"],
		ReadAt:       readAt,
		NotifiedAt:   notifiedAt.UTC(),
		ActivityData: json.RawMessage(activityData),
	}), nil
}

func encodeRoomSubscriptionSettings(settings RoomSubscriptionSettings) map[string]any {
	settings = normalizeRoomSubscriptionSettings(settings)
	return map[string]any{
		"room_id":       settings.RoomID,
		"user_id":       settings.UserID,
		"threads":       settings.Threads,
		"text_mentions": settings.TextMentions,
		"updated_at":    settings.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func decodeRoomSubscriptionSettings(values map[string]string) (RoomSubscriptionSettings, error) {
	updatedAt, err := time.Parse(time.RFC3339Nano, values["updated_at"])
	if err != nil {
		return RoomSubscriptionSettings{}, err
	}
	return normalizeRoomSubscriptionSettings(RoomSubscriptionSettings{
		RoomID:       values["room_id"],
		UserID:       values["user_id"],
		Threads:      values["threads"],
		TextMentions: values["text_mentions"],
		UpdatedAt:    updatedAt.UTC(),
	}), nil
}

func normalizeRoomRecord(record RoomRecord) RoomRecord {
	if len(record.Metadata) == 0 {
		record.Metadata = json.RawMessage(`{}`)
	}
	record.DefaultAccesses = cloneAccessList(record.DefaultAccesses)
	record.UsersAccesses = cloneAccessMap(record.UsersAccesses)
	record.GroupsAccesses = cloneAccessMap(record.GroupsAccesses)
	return record
}

func normalizeThreadRecord(record ThreadRecord) ThreadRecord {
	if record.Type == "" {
		record.Type = "thread"
	}
	if len(record.Metadata) == 0 {
		record.Metadata = json.RawMessage(`{}`)
	}
	record.Metadata = append(json.RawMessage(nil), record.Metadata...)
	record.Comments = cloneCommentList(record.Comments)
	return record
}

func normalizeCommentRecord(record CommentRecord) CommentRecord {
	if record.Type == "" {
		record.Type = "comment"
	}
	if len(record.Body) == 0 {
		record.Body = json.RawMessage(`{}`)
	}
	if len(record.Metadata) == 0 {
		record.Metadata = json.RawMessage(`{}`)
	}
	record.Body = append(json.RawMessage(nil), record.Body...)
	record.Metadata = append(json.RawMessage(nil), record.Metadata...)
	record.Mentions = normalizeStringList(record.Mentions)
	record.Reactions = normalizeCommentReactionList(record.Reactions)
	return record
}

func normalizeInboxNotificationRecord(record InboxNotificationRecord) InboxNotificationRecord {
	if len(record.ActivityData) == 0 {
		record.ActivityData = json.RawMessage(`{}`)
	}
	record.ActivityData = append(json.RawMessage(nil), record.ActivityData...)
	record.NotifiedAt = record.NotifiedAt.UTC()
	if record.ReadAt != nil {
		readAt := record.ReadAt.UTC()
		record.ReadAt = &readAt
	}
	return record
}

func normalizeRoomSubscriptionSettings(settings RoomSubscriptionSettings) RoomSubscriptionSettings {
	if settings.Threads == "" {
		settings.Threads = "all"
	}
	if settings.TextMentions == "" {
		settings.TextMentions = "mine"
	}
	settings.UpdatedAt = settings.UpdatedAt.UTC()
	return settings
}

func defaultRoomSubscriptionSettings(room string, userID string) RoomSubscriptionSettings {
	return normalizeRoomSubscriptionSettings(RoomSubscriptionSettings{
		RoomID:       room,
		UserID:       userID,
		Threads:      "all",
		TextMentions: "mine",
		UpdatedAt:    time.Time{},
	})
}

func (r RoomRecord) Allows(subject string, groupIDs []string, action string) bool {
	if permissions, ok := r.UsersAccesses[subject]; subject != "" && ok {
		return permissionsAllow(permissions, action)
	}

	groupPermissions := []string{}
	matchedGroup := false
	for _, groupID := range groupIDs {
		permissions, ok := r.GroupsAccesses[groupID]
		if !ok {
			continue
		}
		matchedGroup = true
		groupPermissions = append(groupPermissions, permissions...)
	}
	if matchedGroup {
		return permissionsAllow(groupPermissions, action)
	}

	return permissionsAllow(r.DefaultAccesses, action)
}

func permissionsAllow(permissions []string, action string) bool {
	for _, permission := range permissions {
		if permissionAllowsAction(permission, action) {
			return true
		}
	}
	return false
}

func permissionAllowsAction(permission string, action string) bool {
	switch action {
	case "join", "room:read":
		return permission == PermissionRoomRead || permission == PermissionRoomWrite
	case "publish", "room:write":
		return permission == PermissionRoomWrite
	case "presence":
		return permission == PermissionRoomPresenceWrite || permission == PermissionRoomWrite
	case "storage", "storage:read":
		return permission == PermissionStorageRead || permission == PermissionStorageWrite || permission == PermissionRoomWrite
	case "storage:write":
		return permission == PermissionStorageWrite || permission == PermissionRoomWrite
	case "comments", "comments:write":
		return permission == PermissionCommentsWrite || permission == PermissionRoomWrite
	case "comments:read":
		return permission == PermissionCommentsRead || permission == PermissionCommentsWrite || permission == PermissionRoomWrite
	case "feeds", "feeds:write":
		return permission == PermissionFeedsWrite || permission == PermissionRoomWrite
	case "feeds:read":
		return permission == PermissionFeedsRead || permission == PermissionFeedsWrite || permission == PermissionRoomWrite
	default:
		return false
	}
}

func cloneAccessMap(input map[string][]string) map[string][]string {
	out := make(map[string][]string, len(input))
	for id, permissions := range input {
		out[id] = cloneAccessList(permissions)
	}
	return out
}

func cloneAccessList(input []string) []string {
	if input == nil {
		return []string{}
	}
	return append([]string(nil), input...)
}

func cloneCommentList(input []CommentRecord) []CommentRecord {
	out := make([]CommentRecord, 0, len(input))
	for _, comment := range input {
		out = append(out, normalizeCommentRecord(comment))
	}
	return out
}

func normalizeStringList(input []string) []string {
	if len(input) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(input))
	out := make([]string, 0, len(input))
	for _, value := range input {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func cloneStringList(input []string) []string {
	if len(input) == 0 {
		return nil
	}
	return append([]string(nil), input...)
}

func normalizeCommentReactionList(input []CommentReaction) []CommentReaction {
	if len(input) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(input))
	out := make([]CommentReaction, 0, len(input))
	for _, reaction := range input {
		reaction.Emoji = strings.TrimSpace(reaction.Emoji)
		reaction.UserID = strings.TrimSpace(reaction.UserID)
		if reaction.Emoji == "" || reaction.UserID == "" {
			continue
		}
		key := reaction.Emoji + "\x00" + reaction.UserID
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, reaction)
	}
	return out
}

func cloneCommentReactionList(input []CommentReaction) []CommentReaction {
	if len(input) == 0 {
		return nil
	}
	return append([]CommentReaction(nil), input...)
}

func ApplyJSONPatch(document json.RawMessage, operations []JSONPatchOperation) (json.RawMessage, error) {
	var root any
	if err := decodeJSONValue(document, &root); err != nil {
		return nil, fmt.Errorf("%w: invalid storage document", ErrStoragePatch)
	}
	for _, operation := range operations {
		next, err := applyJSONPatchOperation(root, operation)
		if err != nil {
			return nil, err
		}
		root = next
	}
	raw, _ := json.Marshal(root)
	if !isJSONObject(raw) {
		return nil, fmt.Errorf("%w: storage root must remain a JSON object", ErrStoragePatch)
	}
	return raw, nil
}

func ValidateStorageDocument(document json.RawMessage) error {
	var root any
	if err := decodeJSONValue(document, &root); err != nil {
		return fmt.Errorf("%w: invalid storage document", ErrStoragePatch)
	}
	rootObject, ok := root.(map[string]any)
	if !ok {
		return fmt.Errorf("%w: storage root must be a JSON object", ErrStoragePatch)
	}
	return validateStorageValue(rootObject, true)
}

func validateStorageValue(value any, root bool) error {
	switch current := value.(type) {
	case map[string]any:
		typeName, data, typed, err := storageTypedNode(current)
		if err != nil {
			return err
		}
		if typed {
			if root && typeName != storageTypeLiveObject {
				return fmt.Errorf("%w: storage root typed node must be LiveObject", ErrStoragePatch)
			}
			return validateStorageTypedData(typeName, data)
		}
		for _, child := range current {
			if err := validateStorageValue(child, false); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range current {
			if err := validateStorageValue(child, false); err != nil {
				return err
			}
		}
	}
	return nil
}

func storageTypedNode(node map[string]any) (string, any, bool, error) {
	rawType, hasType := node[storageTypeKey]
	if !hasType {
		return "", nil, false, nil
	}
	typeName, ok := rawType.(string)
	if !ok {
		return "", nil, true, fmt.Errorf("%w: liveblocksType must be a string", ErrStoragePatch)
	}
	data, ok := node[storageDataKey]
	if !ok {
		return "", nil, true, fmt.Errorf("%w: typed storage node requires data", ErrStoragePatch)
	}
	switch typeName {
	case storageTypeLiveObject, storageTypeLiveList, storageTypeLiveMap:
		return typeName, data, true, nil
	default:
		return "", nil, true, fmt.Errorf("%w: unsupported typed storage node %q", ErrStoragePatch, typeName)
	}
}

func validateStorageTypedData(typeName string, data any) error {
	switch typeName {
	case storageTypeLiveObject, storageTypeLiveMap:
		children, ok := data.(map[string]any)
		if !ok {
			return fmt.Errorf("%w: %s data must be a JSON object", ErrStoragePatch, typeName)
		}
		for _, child := range children {
			if err := validateStorageValue(child, false); err != nil {
				return err
			}
		}
	case storageTypeLiveList:
		children, ok := data.([]any)
		if !ok {
			return fmt.Errorf("%w: LiveList data must be a JSON array", ErrStoragePatch)
		}
		for _, child := range children {
			if err := validateStorageValue(child, false); err != nil {
				return err
			}
		}
	}
	return nil
}

func applyJSONPatchOperation(root any, operation JSONPatchOperation) (any, error) {
	tokens, err := parseJSONPointer(operation.Path)
	if err != nil {
		return nil, err
	}

	switch operation.Op {
	case "add":
		value, err := operationValue(operation)
		if err != nil {
			return nil, err
		}
		return addJSONValue(root, tokens, value)
	case "remove":
		return removeJSONValue(root, tokens)
	case "replace":
		value, err := operationValue(operation)
		if err != nil {
			return nil, err
		}
		if _, err := getJSONValue(root, tokens); err != nil {
			return nil, err
		}
		return addJSONValue(root, tokens, value)
	case "test":
		value, err := operationValue(operation)
		if err != nil {
			return nil, err
		}
		current, err := getJSONValue(root, tokens)
		if err != nil {
			return nil, err
		}
		if !reflect.DeepEqual(current, value) {
			return nil, fmt.Errorf("%w: test failed at %s", ErrStoragePatch, operation.Path)
		}
		return root, nil
	case "copy":
		from, err := parseJSONPointer(operation.From)
		if err != nil {
			return nil, err
		}
		value, err := getJSONValue(root, from)
		if err != nil {
			return nil, err
		}
		return addJSONValue(root, tokens, cloneJSONValue(value))
	case "move":
		from, err := parseJSONPointer(operation.From)
		if err != nil {
			return nil, err
		}
		value, err := getJSONValue(root, from)
		if err != nil {
			return nil, err
		}
		withoutSource, _ := removeJSONValue(root, from)
		return addJSONValue(withoutSource, tokens, value)
	default:
		return nil, fmt.Errorf("%w: unsupported operation %q", ErrStoragePatch, operation.Op)
	}
}

func operationValue(operation JSONPatchOperation) (any, error) {
	if len(operation.Value) == 0 {
		return nil, fmt.Errorf("%w: value is required for %s", ErrStoragePatch, operation.Op)
	}
	var value any
	if err := decodeJSONValue(operation.Value, &value); err != nil {
		return nil, fmt.Errorf("%w: invalid value for %s", ErrStoragePatch, operation.Op)
	}
	return value, nil
}

func addJSONValue(root any, tokens []string, value any) (any, error) {
	if len(tokens) == 0 {
		return value, nil
	}

	switch current := root.(type) {
	case map[string]any:
		key := tokens[0]
		if len(tokens) == 1 {
			current[key] = value
			return current, nil
		}
		child, ok := current[key]
		if !ok {
			return nil, fmt.Errorf("%w: missing parent at %s", ErrStoragePatch, key)
		}
		updated, err := addJSONValue(child, tokens[1:], value)
		if err != nil {
			return nil, err
		}
		current[key] = updated
		return current, nil
	case []any:
		index, appendValue, err := parseArrayIndex(tokens[0], len(current), true)
		if err != nil {
			return nil, err
		}
		if len(tokens) == 1 {
			if appendValue {
				return append(current, value), nil
			}
			current = append(current[:index], append([]any{value}, current[index:]...)...)
			return current, nil
		}
		if appendValue || index >= len(current) {
			return nil, fmt.Errorf("%w: missing array parent at %s", ErrStoragePatch, tokens[0])
		}
		updated, err := addJSONValue(current[index], tokens[1:], value)
		if err != nil {
			return nil, err
		}
		current[index] = updated
		return current, nil
	default:
		return nil, fmt.Errorf("%w: target parent is not a container", ErrStoragePatch)
	}
}

func removeJSONValue(root any, tokens []string) (any, error) {
	if len(tokens) == 0 {
		return nil, fmt.Errorf("%w: removing storage root is not supported", ErrStoragePatch)
	}

	switch current := root.(type) {
	case map[string]any:
		key := tokens[0]
		if len(tokens) == 1 {
			if _, ok := current[key]; !ok {
				return nil, fmt.Errorf("%w: path does not exist", ErrStoragePatch)
			}
			delete(current, key)
			return current, nil
		}
		child, ok := current[key]
		if !ok {
			return nil, fmt.Errorf("%w: path does not exist", ErrStoragePatch)
		}
		updated, err := removeJSONValue(child, tokens[1:])
		if err != nil {
			return nil, err
		}
		current[key] = updated
		return current, nil
	case []any:
		index, _, err := parseArrayIndex(tokens[0], len(current), false)
		if err != nil {
			return nil, err
		}
		if len(tokens) == 1 {
			return append(current[:index], current[index+1:]...), nil
		}
		updated, err := removeJSONValue(current[index], tokens[1:])
		if err != nil {
			return nil, err
		}
		current[index] = updated
		return current, nil
	default:
		return nil, fmt.Errorf("%w: target parent is not a container", ErrStoragePatch)
	}
}

func getJSONValue(root any, tokens []string) (any, error) {
	if len(tokens) == 0 {
		return root, nil
	}

	switch current := root.(type) {
	case map[string]any:
		child, ok := current[tokens[0]]
		if !ok {
			return nil, fmt.Errorf("%w: path does not exist", ErrStoragePatch)
		}
		return getJSONValue(child, tokens[1:])
	case []any:
		index, _, err := parseArrayIndex(tokens[0], len(current), false)
		if err != nil {
			return nil, err
		}
		return getJSONValue(current[index], tokens[1:])
	default:
		return nil, fmt.Errorf("%w: target is not a container", ErrStoragePatch)
	}
}

func parseJSONPointer(pointer string) ([]string, error) {
	if pointer == "" {
		return nil, nil
	}
	if !strings.HasPrefix(pointer, "/") {
		return nil, fmt.Errorf("%w: path must be a JSON Pointer", ErrStoragePatch)
	}
	parts := strings.Split(pointer[1:], "/")
	for index, part := range parts {
		decoded, err := decodeJSONPointerToken(part)
		if err != nil {
			return nil, err
		}
		parts[index] = decoded
	}
	return parts, nil
}

func decodeJSONPointerToken(token string) (string, error) {
	for i := 0; i < len(token); i++ {
		if token[i] == '~' {
			if i+1 >= len(token) || (token[i+1] != '0' && token[i+1] != '1') {
				return "", fmt.Errorf("%w: invalid JSON Pointer escape", ErrStoragePatch)
			}
			i++
		}
	}
	token = strings.ReplaceAll(token, "~1", "/")
	token = strings.ReplaceAll(token, "~0", "~")
	return token, nil
}

func parseArrayIndex(token string, length int, allowAppend bool) (int, bool, error) {
	if token == "-" {
		if allowAppend {
			return length, true, nil
		}
		return 0, false, fmt.Errorf("%w: '-' is only valid for add", ErrStoragePatch)
	}
	index, err := strconv.Atoi(token)
	if err != nil || index < 0 {
		return 0, false, fmt.Errorf("%w: invalid array index", ErrStoragePatch)
	}
	if allowAppend {
		if index > length {
			return 0, false, fmt.Errorf("%w: array index out of range", ErrStoragePatch)
		}
		return index, false, nil
	}
	if index >= length {
		return 0, false, fmt.Errorf("%w: array index out of range", ErrStoragePatch)
	}
	return index, false, nil
}

func decodeJSONValue(raw json.RawMessage, dest *any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	return decoder.Decode(dest)
}

func cloneJSONValue(value any) any {
	raw, _ := json.Marshal(value)
	var cloned any
	_ = decodeJSONValue(raw, &cloned)
	return cloned
}

func compactJSON(raw json.RawMessage) (json.RawMessage, error) {
	var buffer bytes.Buffer
	if err := json.Compact(&buffer, raw); err != nil {
		return nil, err
	}
	return json.RawMessage(buffer.Bytes()), nil
}

func isJSONObject(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && trimmed[0] == '{'
}

func encodeYJSUpdateRecord(record yjsUpdateRecord) string {
	raw, _ := json.Marshal(record)
	return string(raw)
}

func decodeYJSUpdateRecord(raw string) (yjsUpdateRecord, error) {
	var record yjsUpdateRecord
	if err := json.Unmarshal([]byte(raw), &record); err != nil {
		return yjsUpdateRecord{}, err
	}
	if record.Seq <= 0 || len(record.Update) == 0 {
		return yjsUpdateRecord{}, errors.New("invalid yjs update record")
	}
	if !isDurableYJSEventKind(record.KindValue()) {
		return yjsUpdateRecord{}, errors.New("invalid yjs update kind")
	}
	return record, nil
}

func isDurableYJSEventKind(kind YJSEventKind) bool {
	return kind == YJSEventUpdate || kind == YJSEventSubdocUpdate
}

func encodeYJSSnapshotRecord(record yjsSnapshotRecord) string {
	raw, _ := json.Marshal(record)
	return string(raw)
}

func decodeYJSSnapshotRecord(raw string) (yjsSnapshotRecord, error) {
	var record yjsSnapshotRecord
	if err := json.Unmarshal([]byte(raw), &record); err != nil {
		return yjsSnapshotRecord{}, err
	}
	if record.CheckpointSeq < 0 || len(record.Snapshot) == 0 {
		return yjsSnapshotRecord{}, errors.New("invalid yjs snapshot record")
	}
	return record, nil
}

func connAliveKey(connID string) string {
	return fmt.Sprintf("conn:%s:alive", connID)
}

func connMetaKey(connID string) string {
	return fmt.Sprintf("conn:%s:meta", connID)
}

func connRoomsKey(connID string) string {
	return fmt.Sprintf("conn:%s:rooms", connID)
}

func roomMembersKey(room string) string {
	return fmt.Sprintf("room:%s:members", room)
}

func roomPresenceKey(room string) string {
	return fmt.Sprintf("room:%s:presence", room)
}

func roomRecordKey(room string) string {
	return fmt.Sprintf("room:%s:record", room)
}

func roomRecordScanPattern(prefix string) string {
	return fmt.Sprintf("room:%s*:record", prefix)
}

func roomStorageKey(room string) string {
	return fmt.Sprintf("room:%s:storage", room)
}

func roomEventLogKey(room string) string {
	return fmt.Sprintf("room:%s:events", room)
}

func roomEventSequenceKey(room string) string {
	return fmt.Sprintf("room:%s:events:seq", room)
}

func roomThreadsKey(room string) string {
	return fmt.Sprintf("room:%s:threads", room)
}

func roomThreadKey(room string, threadID string) string {
	return fmt.Sprintf("room:%s:thread:%s", room, threadID)
}

func roomThreadCommentsKey(room string, threadID string) string {
	return fmt.Sprintf("room:%s:thread:%s:comments", room, threadID)
}

func inboxNotificationKey(notificationID string) string {
	return fmt.Sprintf("inbox:%s", notificationID)
}

func userInboxKey(userID string) string {
	return fmt.Sprintf("user:%s:inbox", userID)
}

func userInboxUnreadKey(userID string) string {
	return fmt.Sprintf("user:%s:inbox:unread", userID)
}

func userNotificationSettingsKey(userID string) string {
	return fmt.Sprintf("user:%s:notification_settings", userID)
}

func userRoomSubscriptionSettingsKey(userID string) string {
	return fmt.Sprintf("user:%s:room_subscription_settings", userID)
}

func roomSubscriptionSettingsKey(room string, userID string) string {
	return fmt.Sprintf("room:%s:user:%s:subscription_settings", room, userID)
}

func roomFromRecordKey(key string) (string, bool) {
	const prefix = "room:"
	const suffix = ":record"
	if len(key) <= len(prefix)+len(suffix) || key[:len(prefix)] != prefix || key[len(key)-len(suffix):] != suffix {
		return "", false
	}
	return key[len(prefix) : len(key)-len(suffix)], true
}

func roomEphemeralPresenceKey(room string) string {
	return fmt.Sprintf("room:%s:presence:ephemeral", room)
}

func roomEphemeralPresenceAliveKey(room string, connID string) string {
	return fmt.Sprintf("room:%s:presence:ephemeral:%s:alive", room, connID)
}

func roomYJSSnapshotKey(room string) string {
	return fmt.Sprintf("room:%s:yjs:snapshot", room)
}

func roomYJSUpdatesKey(room string) string {
	return fmt.Sprintf("room:%s:yjs:updates", room)
}

func roomYJSSnapshotV2Key(room string) string {
	return fmt.Sprintf("room:%s:yjs:snapshot:v2", room)
}

func roomYJSUpdatesV2Key(room string) string {
	return fmt.Sprintf("room:%s:yjs:updates:v2", room)
}

func roomYJSSequenceKey(room string) string {
	return fmt.Sprintf("room:%s:yjs:seq", room)
}

func nodeConnsKey(nodeID string) string {
	return fmt.Sprintf("node:%s:conns", nodeID)
}

func nodeStatsKey(nodeID string) string {
	return fmt.Sprintf("stats:node:%s", nodeID)
}

func statsNodesKey() string {
	return "stats:nodes"
}

func parseInt64(value string) int64 {
	parsed, _ := strconv.ParseInt(value, 10, 64)
	return parsed
}

func defaultString(value string, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
