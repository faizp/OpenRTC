package cluster

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/openrtc/openrtc/server/internal/stats"
)

func TestRedisStoreHealthAndEphemeralPresenceExpiry(t *testing.T) {
	if _, err := NewRedisStore("redis://%", "room:"); err == nil {
		t.Fatalf("expected invalid redis URL to fail")
	}

	redisServer, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer redisServer.Close()

	store, err := NewRedisStore("redis://"+redisServer.Addr(), "room:")
	if err != nil {
		t.Fatalf("new redis store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	if err := store.Healthy(ctx); err != nil {
		t.Fatalf("expected healthy redis store: %v", err)
	}
	if err := store.SetEphemeralPresence(ctx, "agent-1", "tenant-a:room-1", json.RawMessage(`{"kind":"agent"}`), time.Minute); err != nil {
		t.Fatalf("set ephemeral presence: %v", err)
	}
	redisServer.FastForward(2 * time.Minute)
	snapshot, err := store.SnapshotRoom(ctx, "tenant-a:room-1")
	if err != nil {
		t.Fatalf("snapshot after ephemeral expiry: %v", err)
	}
	if len(snapshot.Members) != 0 || len(snapshot.Presence) != 0 {
		t.Fatalf("expected expired ephemeral presence to be pruned, got %+v", snapshot)
	}

	if err := store.TouchConnection(ctx, "conn-invalid-time", ConnectionMeta{
		NodeID:      "node-a",
		Subject:     "user-1",
		Tenant:      "tenant-a",
		ConnectedAt: time.Now(),
	}); err != nil {
		t.Fatalf("touch connection: %v", err)
	}
	if err := store.JoinRoom(ctx, "conn-invalid-time", "tenant-a:room-2"); err != nil {
		t.Fatalf("join room: %v", err)
	}
	if err := store.client.HSet(ctx, connMetaKey("conn-invalid-time"), "connected_at", "not-time").Err(); err != nil {
		t.Fatalf("seed invalid connected_at: %v", err)
	}
	users, err := store.ActiveUsers(ctx, "tenant-a:room-2")
	if err != nil {
		t.Fatalf("active users with invalid connected_at: %v", err)
	}
	if len(users) != 1 || !users[0].ConnectedAt.IsZero() {
		t.Fatalf("expected invalid connected_at to be ignored, got %+v", users)
	}
}

func TestRedisPubSubFanoutFiltersMalformedEvents(t *testing.T) {
	redisServer, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer redisServer.Close()

	store, err := NewRedisStore("redis://"+redisServer.Addr(), "room:")
	if err != nil {
		t.Fatalf("new redis store: %v", err)
	}
	defer store.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events := make(chan PublishedEvent, 1)
	if err := store.Subscribe(ctx, func(event PublishedEvent) {
		events <- event
	}); err != nil {
		t.Fatalf("subscribe events: %v", err)
	}
	presenceEvents := make(chan PresenceEvent, 1)
	if err := store.SubscribePresence(ctx, func(event PresenceEvent) {
		presenceEvents <- event
	}); err != nil {
		t.Fatalf("subscribe presence: %v", err)
	}
	yjsEvents := make(chan YJSEvent, 1)
	if err := store.SubscribeYJSEvents(ctx, func(event YJSEvent) {
		yjsEvents <- event
	}); err != nil {
		t.Fatalf("subscribe yjs: %v", err)
	}

	if err := store.client.Publish(ctx, "room:tenant-a:room-1", `not-json`).Err(); err != nil {
		t.Fatalf("publish malformed event: %v", err)
	}
	if err := store.client.Publish(ctx, "room:tenant-a:room-1", `{"room":"tenant-a:room-1"}`).Err(); err != nil {
		t.Fatalf("publish incomplete event: %v", err)
	}
	published, err := store.PublishEvent(ctx, PublishedEvent{
		Room:       "tenant-a:room-1",
		Event:      "doc.update",
		Payload:    json.RawMessage(`{"ok":true}`),
		OriginNode: "node-a",
	})
	if err != nil {
		t.Fatalf("publish valid event: %v", err)
	}
	if published.Sequence != 1 {
		t.Fatalf("expected first event sequence 1, got %d", published.Sequence)
	}
	event := receiveClusterTestValue(t, events, "published event")
	if event.Room != "tenant-a:room-1" || event.Event != "doc.update" || event.Sequence != 1 {
		t.Fatalf("unexpected published event: %+v", event)
	}
	logged, err := store.ListPublishedEvents(ctx, "tenant-a:room-1", 0, 10)
	if err != nil {
		t.Fatalf("list published events: %v", err)
	}
	if len(logged.Events) != 1 || logged.Events[0].Sequence != 1 || logged.Events[0].Event != "doc.update" {
		t.Fatalf("unexpected logged events: %+v", logged.Events)
	}
	second, err := store.PublishEvent(ctx, PublishedEvent{
		Room:       "tenant-a:room-1",
		Event:      "doc.second",
		Payload:    json.RawMessage(`{"ok":true}`),
		OriginNode: "node-a",
	})
	if err != nil {
		t.Fatalf("publish second event: %v", err)
	}
	if second.Sequence != 2 {
		t.Fatalf("expected second event sequence 2, got %d", second.Sequence)
	}
	if got := receiveClusterTestValue(t, events, "second published event"); got.Sequence != 2 || got.Event != "doc.second" {
		t.Fatalf("unexpected second published event: %+v", got)
	}
	logged, err = store.ListPublishedEvents(ctx, "tenant-a:room-1", 1, 10)
	if err != nil {
		t.Fatalf("list published events after sequence: %v", err)
	}
	if len(logged.Events) != 1 || logged.Events[0].Sequence != 2 || logged.Events[0].Event != "doc.second" {
		t.Fatalf("unexpected logged events after sequence: %+v", logged.Events)
	}

	if err := store.client.Publish(ctx, "room:presence:tenant-a:room-1", `not-json`).Err(); err != nil {
		t.Fatalf("publish malformed presence: %v", err)
	}
	if err := store.client.Publish(ctx, "room:presence:tenant-a:room-1", `{"room":"tenant-a:room-1"}`).Err(); err != nil {
		t.Fatalf("publish incomplete presence: %v", err)
	}
	if err := store.PublishPresence(ctx, PresenceEvent{
		Room:       "tenant-a:room-1",
		ConnID:     "conn-1",
		State:      json.RawMessage(`{"status":"online"}`),
		OriginNode: "node-a",
	}); err != nil {
		t.Fatalf("publish valid presence: %v", err)
	}
	presence := receiveClusterTestValue(t, presenceEvents, "presence event")
	if presence.Room != "tenant-a:room-1" || presence.ConnID != "conn-1" {
		t.Fatalf("unexpected presence event: %+v", presence)
	}

	if err := store.client.Publish(ctx, "room:yjs:tenant-a:doc-1", `not-json`).Err(); err != nil {
		t.Fatalf("publish malformed yjs: %v", err)
	}
	if err := store.client.Publish(ctx, "room:yjs:tenant-a:doc-1", `{"room":"tenant-a:doc-1"}`).Err(); err != nil {
		t.Fatalf("publish incomplete yjs: %v", err)
	}
	if err := store.PublishYJSEvent(ctx, YJSEvent{
		Room:       "tenant-a:doc-1",
		Kind:       YJSEventUpdate,
		Update:     []byte("update"),
		OriginNode: "node-a",
	}); err != nil {
		t.Fatalf("publish valid yjs: %v", err)
	}
	yjs := receiveClusterTestValue(t, yjsEvents, "yjs event")
	if yjs.Room != "tenant-a:doc-1" || string(yjs.Update) != "update" {
		t.Fatalf("unexpected yjs event: %+v", yjs)
	}
}

func TestRedisPubSubSubscribeErrorsWhenRedisUnavailable(t *testing.T) {
	redisServer, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	store, err := NewRedisStore("redis://"+redisServer.Addr(), "room:")
	if err != nil {
		t.Fatalf("new redis store: %v", err)
	}
	defer store.Close()
	redisServer.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := store.Subscribe(ctx, func(PublishedEvent) {}); err == nil {
		t.Fatalf("expected event subscribe failure")
	}
	if err := store.SubscribePresence(ctx, func(PresenceEvent) {}); err == nil {
		t.Fatalf("expected presence subscribe failure")
	}
	if err := store.SubscribeYJSEvents(ctx, func(YJSEvent) {}); err == nil {
		t.Fatalf("expected yjs subscribe failure")
	}
}

func TestRedisPublishAndYJSWriteErrors(t *testing.T) {
	redisServer, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	store, err := NewRedisStore("redis://"+redisServer.Addr(), "room:")
	if err != nil {
		t.Fatalf("new redis store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	if _, err := store.PublishEvent(ctx, PublishedEvent{
		Room:       "tenant-a:room-1",
		Event:      "doc.update",
		Payload:    json.RawMessage(`{`),
		OriginNode: "node-a",
	}); err == nil {
		t.Fatalf("expected invalid event payload to fail JSON marshaling")
	}
	if err := store.PublishPresence(ctx, PresenceEvent{
		Room:       "tenant-a:room-1",
		ConnID:     "conn-1",
		State:      json.RawMessage(`{`),
		OriginNode: "node-a",
	}); err == nil {
		t.Fatalf("expected invalid presence state to fail JSON marshaling")
	}

	redisServer.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := store.PublishEvent(ctx, PublishedEvent{
		Room:       "tenant-a:room-1",
		Event:      "doc.update",
		Payload:    json.RawMessage(`{"ok":true}`),
		OriginNode: "node-a",
	}); err == nil {
		t.Fatalf("expected publish event failure when redis is unavailable")
	}
	if err := store.PublishPresence(ctx, PresenceEvent{
		Room:       "tenant-a:room-1",
		ConnID:     "conn-1",
		State:      json.RawMessage(`{"ok":true}`),
		OriginNode: "node-a",
	}); err == nil {
		t.Fatalf("expected publish presence failure when redis is unavailable")
	}
	if err := store.PublishYJSEvent(ctx, YJSEvent{
		Room:       "tenant-a:doc-1",
		Kind:       YJSEventUpdate,
		Update:     []byte("update"),
		OriginNode: "node-a",
	}); err == nil {
		t.Fatalf("expected publish yjs failure when redis is unavailable")
	}
	if err := store.TouchConnection(ctx, "conn-1", ConnectionMeta{
		NodeID:      "node-a",
		Subject:     "user-1",
		Tenant:      "tenant-a",
		ConnectedAt: time.Now(),
	}); err == nil {
		t.Fatalf("expected touch connection failure when redis is unavailable")
	}
	if err := store.JoinRoom(ctx, "conn-1", "tenant-a:room-1"); err == nil {
		t.Fatalf("expected join room failure when redis is unavailable")
	}
	if err := store.LeaveRoom(ctx, "conn-1", "tenant-a:room-1"); err == nil {
		t.Fatalf("expected leave room failure when redis is unavailable")
	}
	if err := store.SetPresence(ctx, "conn-1", "tenant-a:room-1", json.RawMessage(`{"ok":true}`)); err == nil {
		t.Fatalf("expected set presence failure when redis is unavailable")
	}
	if err := store.SetEphemeralPresence(ctx, "agent-1", "tenant-a:room-1", json.RawMessage(`{"ok":true}`), time.Minute); err == nil {
		t.Fatalf("expected set ephemeral presence failure when redis is unavailable")
	}
	if err := store.ClearPresence(ctx, "conn-1", "tenant-a:room-1"); err == nil {
		t.Fatalf("expected clear presence failure when redis is unavailable")
	}
	if _, err := store.SnapshotRoom(ctx, "tenant-a:room-1"); err == nil {
		t.Fatalf("expected snapshot room failure when redis is unavailable")
	}
	if _, err := store.ActiveUsers(ctx, "tenant-a:room-1"); err == nil {
		t.Fatalf("expected active users failure when redis is unavailable")
	}
	if _, err := store.CreateRoom(ctx, RoomRecord{ID: "tenant-a:room-1"}); err == nil {
		t.Fatalf("expected create room failure when redis is unavailable")
	}
	if _, err := store.GetRoom(ctx, "tenant-a:room-1"); err == nil {
		t.Fatalf("expected get room failure when redis is unavailable")
	}
	if _, err := store.UpdateRoom(ctx, "tenant-a:room-1", RoomUpdate{Metadata: json.RawMessage(`{}`), MetadataSet: true}); err == nil {
		t.Fatalf("expected update room failure when redis is unavailable")
	}
	if err := store.DeleteRoom(ctx, "tenant-a:room-1"); err == nil {
		t.Fatalf("expected delete room failure when redis is unavailable")
	}
	if _, err := store.ListRooms(ctx, "tenant-a:", 0, 10); err == nil {
		t.Fatalf("expected list rooms failure when redis is unavailable")
	}
	if _, err := store.CreateThread(ctx, "tenant-a:room-1", ThreadRecord{ID: "thread-1"}); err == nil {
		t.Fatalf("expected create thread failure when redis is unavailable")
	}
	if _, err := store.ListThreads(ctx, "tenant-a:room-1"); err == nil {
		t.Fatalf("expected list threads failure when redis is unavailable")
	}
	if _, err := store.AddComment(ctx, "tenant-a:room-1", "thread-1", CommentRecord{ID: "comment-1", UserID: "user-1", Body: json.RawMessage(`{}`)}); err == nil {
		t.Fatalf("expected add comment failure when redis is unavailable")
	}
	if _, err := store.CreateInboxNotification(ctx, InboxNotificationRecord{ID: "in_1", UserID: "user-1", Kind: "thread"}); err == nil {
		t.Fatalf("expected create inbox notification failure when redis is unavailable")
	}
	if _, err := store.ListInboxNotifications(ctx, "user-1", InboxNotificationListFilter{Limit: 50}); err == nil {
		t.Fatalf("expected list inbox notifications failure when redis is unavailable")
	}
	if _, err := store.GetInboxNotification(ctx, "user-1", "in_1"); err == nil {
		t.Fatalf("expected get inbox notification failure when redis is unavailable")
	}
	if _, err := store.MarkInboxNotificationRead(ctx, "in_1"); err == nil {
		t.Fatalf("expected mark inbox notification read failure when redis is unavailable")
	}
	if err := store.DeleteInboxNotification(ctx, "user-1", "in_1"); err == nil {
		t.Fatalf("expected delete inbox notification failure when redis is unavailable")
	}
	if err := store.DeleteAllInboxNotifications(ctx, "user-1"); err == nil {
		t.Fatalf("expected delete all inbox notifications failure when redis is unavailable")
	}
	if _, err := store.GetNotificationSettings(ctx, "user-1"); err == nil {
		t.Fatalf("expected get notification settings failure when redis is unavailable")
	}
	if _, err := store.SetNotificationSettings(ctx, "user-1", json.RawMessage(`{}`)); err == nil {
		t.Fatalf("expected set notification settings failure when redis is unavailable")
	}
	if err := store.DeleteNotificationSettings(ctx, "user-1"); err == nil {
		t.Fatalf("expected delete notification settings failure when redis is unavailable")
	}
	if _, err := store.GetRoomSubscriptionSettings(ctx, "tenant-a:room-1", "user-1"); err == nil {
		t.Fatalf("expected get room subscription settings failure when redis is unavailable")
	}
	if _, err := store.SetRoomSubscriptionSettings(ctx, RoomSubscriptionSettings{RoomID: "tenant-a:room-1", UserID: "user-1"}); err == nil {
		t.Fatalf("expected set room subscription settings failure when redis is unavailable")
	}
	if err := store.DeleteRoomSubscriptionSettings(ctx, "tenant-a:room-1", "user-1"); err == nil {
		t.Fatalf("expected delete room subscription settings failure when redis is unavailable")
	}
	if _, err := store.ListRoomSubscriptionSettings(ctx, "user-1", 0, 50); err == nil {
		t.Fatalf("expected list room subscription settings failure when redis is unavailable")
	}
	if _, err := store.AppendYJSUpdate(ctx, "tenant-a:doc-1", YJSEventUpdate, []byte("update")); err == nil {
		t.Fatalf("expected append yjs update failure when redis is unavailable")
	}
	if err := store.StoreYJSSnapshot(ctx, "tenant-a:doc-1", []byte("snapshot")); err == nil {
		t.Fatalf("expected store yjs snapshot failure when redis is unavailable")
	}
	if err := store.CompactYJSDocument(ctx, "tenant-a:doc-1", 1, []byte("snapshot")); err == nil {
		t.Fatalf("expected compact yjs document failure when redis is unavailable")
	}
	if _, err := store.LoadYJSDocument(ctx, "tenant-a:doc-1"); err == nil {
		t.Fatalf("expected load yjs document failure when redis is unavailable")
	}
	if _, err := store.SetStorage(ctx, "tenant-a:doc-1", json.RawMessage(`{"ok":true}`)); err == nil {
		t.Fatalf("expected set storage failure when redis is unavailable")
	}
	if _, err := store.GetStorage(ctx, "tenant-a:doc-1"); err == nil {
		t.Fatalf("expected get storage failure when redis is unavailable")
	}
	if err := store.DeleteStorage(ctx, "tenant-a:doc-1"); err == nil {
		t.Fatalf("expected delete storage failure when redis is unavailable")
	}
	if err := store.CleanupConnection(ctx, "node-a", "conn-1"); err == nil {
		t.Fatalf("expected cleanup connection failure when redis is unavailable")
	}
	if err := store.ReconcileNode(ctx, "node-a"); err == nil {
		t.Fatalf("expected reconcile node failure when redis is unavailable")
	}
	if err := store.SyncStats(ctx, "node-a", stats.Snapshot{ActiveConnections: 1}); err == nil {
		t.Fatalf("expected sync stats failure when redis is unavailable")
	}
	if _, err := store.AggregateStats(ctx); err == nil {
		t.Fatalf("expected aggregate stats failure when redis is unavailable")
	}
}

func TestRedisCommandFailureBranches(t *testing.T) {
	redisServer, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer redisServer.Close()

	store, err := NewRedisStore("redis://"+redisServer.Addr(), "room:")
	if err != nil {
		t.Fatalf("new redis store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	expected := errors.New("hook failed")
	hook := &redisCommandFailureHook{}
	store.client.AddHook(hook)

	hook.processFailures = map[string]error{"exists": expected}
	if _, err := store.CreateRoom(ctx, RoomRecord{ID: "tenant-a:exists-room"}); !errors.Is(err, expected) {
		t.Fatalf("expected create room exists failure, got %v", err)
	}
	if _, err := store.CreateThread(ctx, "tenant-a:room-1", ThreadRecord{ID: "thread-exists"}); !errors.Is(err, expected) {
		t.Fatalf("expected create thread exists failure, got %v", err)
	}
	if _, err := store.CreateInboxNotification(ctx, InboxNotificationRecord{ID: "in_exists", UserID: "user-1", Kind: "thread"}); !errors.Is(err, expected) {
		t.Fatalf("expected inbox exists failure, got %v", err)
	}
	hook.clear()

	if _, err := store.CreateRoom(ctx, RoomRecord{ID: "tenant-a:room-1"}); err != nil {
		t.Fatalf("seed room: %v", err)
	}
	if _, err := store.CreateThread(ctx, "tenant-a:room-1", ThreadRecord{ID: "thread-1"}); err != nil {
		t.Fatalf("seed thread: %v", err)
	}
	if _, err := store.CreateInboxNotification(ctx, InboxNotificationRecord{ID: "in_1", UserID: "user-1", Kind: "thread"}); err != nil {
		t.Fatalf("seed inbox notification: %v", err)
	}
	if err := store.SyncStats(ctx, "node-a", stats.Snapshot{ActiveConnections: 1}); err != nil {
		t.Fatalf("seed stats: %v", err)
	}
	if err := store.SetEphemeralPresence(ctx, "conn-1", "tenant-a:room-1", json.RawMessage(`{"ok":true}`), time.Minute); err != nil {
		t.Fatalf("seed ephemeral presence: %v", err)
	}
	if _, err := store.SetStorage(ctx, "tenant-a:room-1", json.RawMessage(`{"title":"Draft"}`)); err != nil {
		t.Fatalf("seed storage: %v", err)
	}

	hook.processFailures = map[string]error{"hgetall": expected}
	if _, err := store.UpdateRoom(ctx, "tenant-a:room-1", RoomUpdate{MetadataSet: true, Metadata: json.RawMessage(`{}`)}); !errors.Is(err, expected) {
		t.Fatalf("expected update room hgetall failure, got %v", err)
	}
	if _, err := store.ListThreads(ctx, "tenant-a:room-1"); !errors.Is(err, expected) {
		t.Fatalf("expected list threads hgetall failure, got %v", err)
	}
	if _, err := store.ListInboxNotifications(ctx, "user-1", InboxNotificationListFilter{Limit: 50}); !errors.Is(err, expected) {
		t.Fatalf("expected list inbox hgetall failure, got %v", err)
	}
	if _, err := store.MarkInboxNotificationRead(ctx, "in_1"); !errors.Is(err, expected) {
		t.Fatalf("expected mark inbox hgetall failure, got %v", err)
	}
	if _, err := store.ActiveUsers(ctx, "tenant-a:room-1"); !errors.Is(err, expected) {
		t.Fatalf("expected active users hgetall failure, got %v", err)
	}
	if _, err := store.AggregateStats(ctx); !errors.Is(err, expected) {
		t.Fatalf("expected aggregate stats hgetall failure, got %v", err)
	}
	hook.clear()

	hook.processFailures = map[string]error{"get": expected}
	if _, err := store.ApplyStoragePatch(ctx, "tenant-a:room-1", []JSONPatchOperation{
		{Op: "replace", Path: "/title", Value: json.RawMessage(`"Published"`)},
	}, 1024); !errors.Is(err, expected) {
		t.Fatalf("expected storage patch get failure, got %v", err)
	}
	hook.clear()

	hook.pipelineFailures = map[string]error{"set": expected}
	if _, err := store.ApplyStoragePatch(ctx, "tenant-a:room-1", []JSONPatchOperation{
		{Op: "replace", Path: "/title", Value: json.RawMessage(`"Published"`)},
	}, 1024); !errors.Is(err, expected) {
		t.Fatalf("expected storage patch pipeline failure, got %v", err)
	}
	hook.clear()

	hook.pipelineFailures = map[string]error{"hset": expected}
	if _, err := store.UpdateRoom(ctx, "tenant-a:room-1", RoomUpdate{MetadataSet: true, Metadata: json.RawMessage(`{"status":"published"}`)}); !errors.Is(err, expected) {
		t.Fatalf("expected update room hset pipeline failure, got %v", err)
	}
	hook.clear()

	hook.processFailures = map[string]error{"lrange": expected}
	if _, err := store.LoadYJSDocument(ctx, "tenant-a:room-1"); !errors.Is(err, expected) {
		t.Fatalf("expected yjs legacy update failure, got %v", err)
	}
	if _, err := store.ListThreads(ctx, "tenant-a:room-1"); !errors.Is(err, expected) {
		t.Fatalf("expected thread comments lrange failure, got %v", err)
	}
	hook.clear()

	hook.processFailure = func(cmd redis.Cmder) error {
		if strings.ToLower(cmd.Name()) != "get" || len(cmd.Args()) < 2 {
			return nil
		}
		key, _ := cmd.Args()[1].(string)
		if key == roomYJSSnapshotKey("tenant-a:room-1") {
			return expected
		}
		return nil
	}
	if _, err := store.LoadYJSDocument(ctx, "tenant-a:room-1"); !errors.Is(err, expected) {
		t.Fatalf("expected yjs legacy snapshot get failure, got %v", err)
	}
	hook.clear()

	hook.processFailures = map[string]error{"zrangebyscore": expected}
	if _, err := store.LoadYJSDocument(ctx, "tenant-a:room-1"); !errors.Is(err, expected) {
		t.Fatalf("expected yjs sequenced update failure, got %v", err)
	}
	hook.clear()

	hook.processFailures = map[string]error{"zadd": expected}
	if _, err := store.AppendYJSUpdate(ctx, "tenant-a:room-1", YJSEventUpdate, []byte("update")); !errors.Is(err, expected) {
		t.Fatalf("expected yjs append zadd failure, got %v", err)
	}
	hook.clear()

	hook.processFailures = map[string]error{"exists": expected}
	if _, err := store.AddComment(ctx, "tenant-a:room-1", "thread-1", CommentRecord{ID: "comment-exists-fail", UserID: "user-1", Body: json.RawMessage(`{}`)}); !errors.Is(err, expected) {
		t.Fatalf("expected add comment exists failure, got %v", err)
	}
	if _, err := store.SnapshotRoom(ctx, "tenant-a:room-1"); !errors.Is(err, expected) {
		t.Fatalf("expected snapshot ephemeral exists failure, got %v", err)
	}
	hook.clear()

	if err := store.client.SAdd(ctx, nodeConnsKey("node-a"), "stale-conn").Err(); err != nil {
		t.Fatalf("seed stale connection: %v", err)
	}
	hook.pipelineFailures = map[string]error{"del": expected}
	if err := store.ReconcileNode(ctx, "node-a"); !errors.Is(err, expected) {
		t.Fatalf("expected reconcile cleanup failure, got %v", err)
	}
	hook.clear()

	hook.processFailures = map[string]error{"exists": expected}
	if err := store.ReconcileNode(ctx, "node-a"); !errors.Is(err, expected) {
		t.Fatalf("expected reconcile exists failure, got %v", err)
	}
	hook.clear()

	if _, err := store.CreateRoom(ctx, RoomRecord{ID: "tenant-a:vanishing"}); err != nil {
		t.Fatalf("seed vanishing room: %v", err)
	}
	deleteClient := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	defer deleteClient.Close()
	deletedRoom := false
	hook.processFailure = func(cmd redis.Cmder) error {
		if strings.ToLower(cmd.Name()) != "hgetall" || len(cmd.Args()) < 2 {
			return nil
		}
		key, _ := cmd.Args()[1].(string)
		if key == roomRecordKey("tenant-a:vanishing") && !deletedRoom {
			deletedRoom = true
			if err := deleteClient.Del(ctx, key).Err(); err != nil {
				return err
			}
		}
		return nil
	}
	rooms, err := store.ListRooms(ctx, "tenant-a:vanishing", 0, 10)
	if err != nil {
		t.Fatalf("vanishing room list should skip missing record: %v", err)
	}
	if len(rooms.Rooms) != 0 {
		t.Fatalf("expected vanished room to be skipped, got %+v", rooms.Rooms)
	}
	hook.clear()
}

func TestRedisYJSDocumentSequencedCompaction(t *testing.T) {
	redisServer, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer redisServer.Close()

	store, err := NewRedisStore("redis://"+redisServer.Addr(), "room:")
	if err != nil {
		t.Fatalf("new redis store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	seq1, err := store.AppendYJSUpdate(ctx, "tenant-a:doc-1", YJSEventUpdate, []byte("update-1"))
	if err != nil {
		t.Fatalf("append update 1: %v", err)
	}
	seq2, err := store.AppendYJSUpdate(ctx, "tenant-a:doc-1", YJSEventUpdate, []byte("update-2"))
	if err != nil {
		t.Fatalf("append update 2: %v", err)
	}
	if seq1 != 1 || seq2 != 2 {
		t.Fatalf("unexpected sequences: %d %d", seq1, seq2)
	}

	doc, err := store.LoadYJSDocument(ctx, "tenant-a:doc-1")
	if err != nil {
		t.Fatalf("load document: %v", err)
	}
	assertYJSUpdates(t, doc, []string{"update-1", "update-2"}, []int64{1, 2}, []YJSEventKind{YJSEventUpdate, YJSEventUpdate})

	subdocSeq, err := store.AppendYJSUpdate(ctx, "tenant-a:doc-1", YJSEventSubdocUpdate, []byte("subdoc-update"))
	if err != nil {
		t.Fatalf("append subdoc update: %v", err)
	}
	if subdocSeq != 3 {
		t.Fatalf("unexpected subdoc sequence: %d", subdocSeq)
	}
	doc, err = store.LoadYJSDocument(ctx, "tenant-a:doc-1")
	if err != nil {
		t.Fatalf("load after subdoc update: %v", err)
	}
	if len(doc.UpdateKinds) != 3 || doc.UpdateKinds[2] != YJSEventSubdocUpdate || string(doc.Updates[2]) != "subdoc-update" {
		t.Fatalf("expected subdoc update kind to round trip, got %+v", doc)
	}

	if err := store.CompactYJSDocument(ctx, "tenant-a:doc-1", seq1, []byte("snapshot-through-1")); err != nil {
		t.Fatalf("compact document: %v", err)
	}
	doc, err = store.LoadYJSDocument(ctx, "tenant-a:doc-1")
	if err != nil {
		t.Fatalf("load compacted document: %v", err)
	}
	if string(doc.Snapshot) != "snapshot-through-1" || doc.SnapshotCheckpoint != seq1 {
		t.Fatalf("unexpected compacted snapshot: checkpoint=%d snapshot=%q", doc.SnapshotCheckpoint, string(doc.Snapshot))
	}
	if doc.SnapshotHash != YJSSnapshotHash([]byte("snapshot-through-1")) {
		t.Fatalf("unexpected compacted snapshot hash: %q", doc.SnapshotHash)
	}
	assertYJSUpdates(t, doc, []string{"update-2", "subdoc-update"}, []int64{2, 3}, []YJSEventKind{YJSEventUpdate, YJSEventSubdocUpdate})

	if err := store.StoreYJSSnapshot(ctx, "tenant-a:doc-1", []byte("client-snapshot")); err != nil {
		t.Fatalf("store client snapshot: %v", err)
	}
	doc, err = store.LoadYJSDocument(ctx, "tenant-a:doc-1")
	if err != nil {
		t.Fatalf("load after client snapshot: %v", err)
	}
	if string(doc.Snapshot) != "client-snapshot" || doc.SnapshotCheckpoint != seq1 {
		t.Fatalf("client snapshot should preserve checkpoint, got checkpoint=%d snapshot=%q", doc.SnapshotCheckpoint, string(doc.Snapshot))
	}
	if doc.SnapshotHash != YJSSnapshotHash([]byte("client-snapshot")) {
		t.Fatalf("unexpected client snapshot hash: %q", doc.SnapshotHash)
	}
	assertYJSUpdates(t, doc, []string{"update-2", "subdoc-update"}, []int64{2, 3}, []YJSEventKind{YJSEventUpdate, YJSEventSubdocUpdate})

	seq3, err := store.AppendYJSUpdate(ctx, "tenant-a:doc-1", YJSEventUpdate, []byte("update-3"))
	if err != nil {
		t.Fatalf("append update 3: %v", err)
	}
	if seq3 != 4 {
		t.Fatalf("unexpected update-3 sequence: %d", seq3)
	}
	if err := store.CompactYJSDocument(ctx, "tenant-a:doc-1", seq3, []byte("snapshot-through-3")); err != nil {
		t.Fatalf("compact through 3: %v", err)
	}
	doc, err = store.LoadYJSDocument(ctx, "tenant-a:doc-1")
	if err != nil {
		t.Fatalf("load final compacted document: %v", err)
	}
	if string(doc.Snapshot) != "snapshot-through-3" || doc.SnapshotCheckpoint != seq3 {
		t.Fatalf("unexpected final snapshot: checkpoint=%d snapshot=%q", doc.SnapshotCheckpoint, string(doc.Snapshot))
	}
	if doc.SnapshotHash != YJSSnapshotHash([]byte("snapshot-through-3")) {
		t.Fatalf("unexpected final snapshot hash: %q", doc.SnapshotHash)
	}
	assertYJSUpdates(t, doc, nil, nil, nil)
}

func TestRedisYJSDocumentLoadsLegacyUpdates(t *testing.T) {
	redisServer, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer redisServer.Close()

	store, err := NewRedisStore("redis://"+redisServer.Addr(), "room:")
	if err != nil {
		t.Fatalf("new redis store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	if err := store.client.Set(ctx, roomYJSSnapshotKey("tenant-a:legacy"), "legacy-snapshot", 0).Err(); err != nil {
		t.Fatalf("seed legacy snapshot: %v", err)
	}
	if err := store.client.RPush(ctx, roomYJSUpdatesKey("tenant-a:legacy"), "legacy-update").Err(); err != nil {
		t.Fatalf("seed legacy update: %v", err)
	}

	doc, err := store.LoadYJSDocument(ctx, "tenant-a:legacy")
	if err != nil {
		t.Fatalf("load legacy document: %v", err)
	}
	if string(doc.Snapshot) != "legacy-snapshot" || doc.SnapshotCheckpoint != 0 {
		t.Fatalf("unexpected legacy snapshot: checkpoint=%d snapshot=%q", doc.SnapshotCheckpoint, string(doc.Snapshot))
	}
	if doc.SnapshotHash != YJSSnapshotHash([]byte("legacy-snapshot")) {
		t.Fatalf("unexpected legacy snapshot hash: %q", doc.SnapshotHash)
	}
	assertYJSUpdates(t, doc, []string{"legacy-update"}, []int64{0}, []YJSEventKind{YJSEventUpdate})
}

func TestRedisYJSDocumentSkipsInvalidSequencedRecords(t *testing.T) {
	redisServer, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer redisServer.Close()

	store, err := NewRedisStore("redis://"+redisServer.Addr(), "room:")
	if err != nil {
		t.Fatalf("new redis store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	if err := store.CompactYJSDocument(ctx, "tenant-a:doc-1", 5, []byte("checkpoint-5")); err != nil {
		t.Fatalf("compact initial checkpoint: %v", err)
	}
	oldRecord := encodeYJSUpdateRecord(yjsUpdateRecord{Seq: 4, Update: []byte("old")})
	currentRecord := encodeYJSUpdateRecord(yjsUpdateRecord{Seq: 6, Update: []byte("current")})
	if err := store.client.ZAdd(ctx, roomYJSUpdatesV2Key("tenant-a:doc-1"),
		redis.Z{Score: 4, Member: oldRecord},
		redis.Z{Score: 5.5, Member: `{`},
		redis.Z{Score: 6, Member: currentRecord},
	).Err(); err != nil {
		t.Fatalf("seed sequenced records: %v", err)
	}
	doc, err := store.LoadYJSDocument(ctx, "tenant-a:doc-1")
	if err != nil {
		t.Fatalf("load document: %v", err)
	}
	if string(doc.Snapshot) != "checkpoint-5" || doc.SnapshotCheckpoint != 5 {
		t.Fatalf("unexpected checkpoint snapshot: %+v", doc)
	}
	assertYJSUpdates(t, doc, []string{"current"}, []int64{6}, []YJSEventKind{YJSEventUpdate})

	if err := store.client.Set(ctx, roomYJSSnapshotV2Key("tenant-a:broken-doc"), `{`, 0).Err(); err != nil {
		t.Fatalf("seed broken snapshot: %v", err)
	}
	if _, err := store.LoadYJSDocument(ctx, "tenant-a:broken-doc"); err == nil {
		t.Fatalf("expected corrupt checkpoint snapshot to fail")
	}
	badHashRecord := encodeYJSSnapshotRecord(yjsSnapshotRecord{CheckpointSeq: 1, Snapshot: []byte("snapshot")})
	badHashRecord = strings.Replace(badHashRecord, YJSSnapshotHash([]byte("snapshot")), "00000000", 1)
	if err := store.client.Set(ctx, roomYJSSnapshotV2Key("tenant-a:bad-hash"), badHashRecord, 0).Err(); err != nil {
		t.Fatalf("seed bad hash snapshot: %v", err)
	}
	if _, err := store.LoadYJSDocument(ctx, "tenant-a:bad-hash"); err == nil {
		t.Fatalf("expected bad hash snapshot to fail")
	}
}

func TestRedisYJSDocumentRejectsInvalidCompaction(t *testing.T) {
	redisServer, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer redisServer.Close()

	store, err := NewRedisStore("redis://"+redisServer.Addr(), "room:")
	if err != nil {
		t.Fatalf("new redis store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	if err := store.CompactYJSDocument(ctx, "tenant-a:doc-1", -1, []byte("snapshot")); err == nil {
		t.Fatalf("expected negative checkpoint rejection")
	}
	if err := store.CompactYJSDocument(ctx, "tenant-a:doc-1", 0, nil); err == nil {
		t.Fatalf("expected empty snapshot rejection")
	}
	if _, err := store.AppendYJSUpdate(ctx, "tenant-a:doc-1", YJSEventUpdate, nil); err == nil {
		t.Fatalf("expected empty yjs update rejection")
	}
	if err := store.StoreYJSSnapshot(ctx, "tenant-a:doc-1", nil); err == nil {
		t.Fatalf("expected empty yjs snapshot rejection")
	}
	if err := store.StoreYJSSnapshot(ctx, "tenant-a:doc-1", []byte("snapshot")); err != nil {
		t.Fatalf("store initial snapshot: %v", err)
	}
	doc, err := store.LoadYJSDocument(ctx, "tenant-a:doc-1")
	if err != nil {
		t.Fatalf("load document: %v", err)
	}
	if string(doc.Snapshot) != "snapshot" || doc.SnapshotCheckpoint != 0 {
		t.Fatalf("unexpected stored snapshot: %+v", doc)
	}
}

func TestRedisRoomRecordLifecycle(t *testing.T) {
	redisServer, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer redisServer.Close()

	store, err := NewRedisStore("redis://"+redisServer.Addr(), "room:")
	if err != nil {
		t.Fatalf("new redis store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	created, err := store.CreateRoom(ctx, RoomRecord{
		ID:              "tenant-a:room-1",
		Metadata:        json.RawMessage(`{"name":"Room 1"}`),
		DefaultAccesses: []string{PermissionRoomRead, PermissionRoomPresenceWrite},
		UsersAccesses: map[string][]string{
			"user-editor": {PermissionRoomWrite},
		},
		GroupsAccesses: map[string][]string{
			"viewers": {PermissionRoomRead},
		},
	})
	if err != nil {
		t.Fatalf("create room: %v", err)
	}
	if created.ID != "tenant-a:room-1" || string(created.Metadata) != `{"name":"Room 1"}` {
		t.Fatalf("unexpected created room: %+v", created)
	}
	if created.CreatedAt.IsZero() || created.UpdatedAt.IsZero() {
		t.Fatalf("expected timestamps: %+v", created)
	}
	if !created.Allows("user-editor", nil, "publish") {
		t.Fatalf("expected user editor grant to allow publishing")
	}
	if created.Allows("user-viewer", nil, "publish") {
		t.Fatalf("expected default read grant to deny publishing")
	}
	if !created.Allows("user-viewer", nil, "presence") {
		t.Fatalf("expected default presence grant to allow presence")
	}
	if !created.Allows("user-viewer", []string{"viewers"}, "join") {
		t.Fatalf("expected group read grant to allow join")
	}
	if created.Allows("user-viewer", nil, "comments") {
		t.Fatalf("expected default grants to deny comments")
	}
	created.UsersAccesses["commenter"] = []string{PermissionCommentsWrite}
	if !created.Allows("commenter", nil, "comments") {
		t.Fatalf("expected comments grant to allow comments")
	}
	created.UsersAccesses["storage-reader"] = []string{PermissionStorageRead}
	if !created.Allows("storage-reader", nil, "storage:read") {
		t.Fatalf("expected storage read grant to allow storage reads")
	}
	if created.Allows("storage-reader", nil, "storage:write") {
		t.Fatalf("expected storage read grant to deny storage writes")
	}
	created.UsersAccesses["storage-writer"] = []string{PermissionStorageWrite}
	if !created.Allows("storage-writer", nil, "storage:read") || !created.Allows("storage-writer", nil, "storage:write") {
		t.Fatalf("expected storage write grant to allow storage read/write")
	}
	created.UsersAccesses["comment-reader"] = []string{PermissionCommentsRead}
	if !created.Allows("comment-reader", nil, "comments:read") {
		t.Fatalf("expected comments read grant to allow comment reads")
	}
	if created.Allows("comment-reader", nil, "comments:write") {
		t.Fatalf("expected comments read grant to deny comment writes")
	}
	created.UsersAccesses["feed-reader"] = []string{PermissionFeedsRead}
	if !created.Allows("feed-reader", nil, "feeds:read") {
		t.Fatalf("expected feeds read grant to allow feed reads")
	}
	if created.Allows("feed-reader", nil, "feeds:write") {
		t.Fatalf("expected feeds read grant to deny feed writes")
	}

	if _, err := store.CreateRoom(ctx, RoomRecord{ID: "tenant-a:room-1"}); !errors.Is(err, ErrRoomAlreadyExists) {
		t.Fatalf("expected room conflict, got %v", err)
	}
	if _, err := store.UpdateRoom(ctx, "tenant-a:missing", RoomUpdate{Metadata: json.RawMessage(`{}`), MetadataSet: true}); !errors.Is(err, ErrRoomNotFound) {
		t.Fatalf("expected missing room update failure, got %v", err)
	}

	got, err := store.GetRoom(ctx, "tenant-a:room-1")
	if err != nil {
		t.Fatalf("get room: %v", err)
	}
	if got.ID != created.ID || string(got.Metadata) != string(created.Metadata) || !got.CreatedAt.Equal(created.CreatedAt) {
		t.Fatalf("unexpected room: %+v", got)
	}
	if !got.Allows("user-editor", nil, "publish") {
		t.Fatalf("expected persisted user grant to allow publishing")
	}

	updated, err := store.UpdateRoom(ctx, "tenant-a:room-1", RoomUpdate{
		Metadata:           json.RawMessage(`{"name":"Room One","pinned":true}`),
		MetadataSet:        true,
		DefaultAccesses:    []string{},
		DefaultAccessesSet: true,
		UsersAccesses: map[string][]string{
			"user-owner": {PermissionRoomWrite},
		},
		UsersAccessesSet: true,
		GroupsAccesses: map[string][]string{
			"editors": {PermissionRoomWrite},
		},
		GroupsAccessesSet: true,
	})
	if err != nil {
		t.Fatalf("update room: %v", err)
	}
	if string(updated.Metadata) != `{"name":"Room One","pinned":true}` {
		t.Fatalf("unexpected updated metadata: %s", updated.Metadata)
	}
	if !updated.CreatedAt.Equal(created.CreatedAt) || updated.UpdatedAt.Before(created.UpdatedAt) {
		t.Fatalf("unexpected updated timestamps: created=%s updated=%s", updated.CreatedAt, updated.UpdatedAt)
	}
	if updated.Allows("user-viewer", nil, "join") {
		t.Fatalf("expected updated private default access to deny join")
	}
	if !updated.Allows("user-viewer", []string{"editors"}, "publish") {
		t.Fatalf("expected updated group write grant to allow publish")
	}
	if !updated.Allows("user-owner", nil, "publish") {
		t.Fatalf("expected updated user write grant to allow publish")
	}
	if updated.Allows("user-viewer", []string{"missing-group"}, "join") {
		t.Fatalf("expected missing group grant to fall back to private defaults")
	}

	if _, err := store.CreateRoom(ctx, RoomRecord{ID: "tenant-a:room-2"}); err != nil {
		t.Fatalf("create room 2: %v", err)
	}
	if _, err := store.CreateRoom(ctx, RoomRecord{ID: "tenant-b:room-1"}); err != nil {
		t.Fatalf("create tenant b room: %v", err)
	}
	defaultLimitList, err := store.ListRooms(ctx, "tenant-a:", 0, 0)
	if err != nil {
		t.Fatalf("list rooms with default limit: %v", err)
	}
	if len(defaultLimitList.Rooms) < 2 {
		t.Fatalf("expected default limit list to return tenant rooms, got %+v", defaultLimitList)
	}
	list, err := store.ListRooms(ctx, "tenant-a:", 0, 100)
	if err != nil {
		t.Fatalf("list rooms: %v", err)
	}
	if len(list.Rooms) != 2 || list.Rooms[0].ID != "tenant-a:room-1" || list.Rooms[1].ID != "tenant-a:room-2" {
		t.Fatalf("unexpected room list: %+v", list.Rooms)
	}
	if err := store.client.Set(ctx, "room::record", "malformed", 0).Err(); err != nil {
		t.Fatalf("seed malformed room record key: %v", err)
	}
	if _, err := store.ListRooms(ctx, "", 0, 100); err != nil {
		t.Fatalf("malformed room record key should be ignored: %v", err)
	}
	if err := store.client.Del(ctx, "room::record").Err(); err != nil {
		t.Fatalf("delete malformed room record key: %v", err)
	}
	if err := store.client.HSet(ctx, roomRecordKey("tenant-a:broken"), map[string]any{
		"id":         "tenant-a:broken",
		"metadata":   "{}",
		"created_at": "not-time",
		"updated_at": time.Now().UTC().Format(time.RFC3339Nano),
	}).Err(); err != nil {
		t.Fatalf("seed broken room record: %v", err)
	}
	if _, err := store.ListRooms(ctx, "tenant-a:", 0, 100); err == nil {
		t.Fatalf("expected broken room record to fail listing")
	}
	if err := store.client.Del(ctx, roomRecordKey("tenant-a:broken")).Err(); err != nil {
		t.Fatalf("delete broken room record: %v", err)
	}
	if err := store.client.HSet(ctx, roomRecordKey("tenant-a:room-2"), "default_accesses", `[`).Err(); err != nil {
		t.Fatalf("corrupt room record: %v", err)
	}
	if _, err := store.UpdateRoom(ctx, "tenant-a:room-2", RoomUpdate{Metadata: json.RawMessage(`{"bad":true}`), MetadataSet: true}); err == nil {
		t.Fatalf("expected corrupt room update to fail")
	}
	if err := store.client.Del(ctx, roomRecordKey("tenant-a:room-2")).Err(); err != nil {
		t.Fatalf("delete corrupt room record: %v", err)
	}

	if err := store.DeleteRoom(ctx, "tenant-a:room-1"); err != nil {
		t.Fatalf("delete room: %v", err)
	}
	if _, err := store.GetRoom(ctx, "tenant-a:room-1"); !errors.Is(err, ErrRoomNotFound) {
		t.Fatalf("expected room not found, got %v", err)
	}
	if err := store.DeleteRoom(ctx, "tenant-a:room-1"); !errors.Is(err, ErrRoomNotFound) {
		t.Fatalf("expected missing room delete failure, got %v", err)
	}
}

func TestRedisThreadLifecycle(t *testing.T) {
	redisServer, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer redisServer.Close()

	store, err := NewRedisStore("redis://"+redisServer.Addr(), "room:")
	if err != nil {
		t.Fatalf("new redis store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	created, err := store.CreateThread(ctx, "tenant-a:room-1", ThreadRecord{
		ID:       "thread-1",
		Metadata: json.RawMessage(`{"x":1}`),
		Comments: []CommentRecord{
			{
				ID:       "comment-1",
				UserID:   "user-1",
				Body:     json.RawMessage(`{"content":[{"type":"paragraph"}]}`),
				Metadata: json.RawMessage(`{"source":"test"}`),
				Mentions: []string{"user-2", "user-2"},
				Reactions: []CommentReaction{
					{Emoji: "+1", UserID: "user-2"},
					{Emoji: "+1", UserID: "user-2"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("create thread: %v", err)
	}
	if created.Type != "thread" || created.RoomID != "tenant-a:room-1" || string(created.Metadata) != `{"x":1}` {
		t.Fatalf("unexpected created thread: %+v", created)
	}
	if len(created.Comments) != 1 || created.Comments[0].ThreadID != "thread-1" || created.Comments[0].RoomID != "tenant-a:room-1" {
		t.Fatalf("unexpected created thread comments: %+v", created.Comments)
	}
	if len(created.Comments[0].Mentions) != 1 || created.Comments[0].Mentions[0] != "user-2" || len(created.Comments[0].Reactions) != 1 {
		t.Fatalf("expected normalized initial mentions/reactions, got %+v", created.Comments[0])
	}
	if _, err := store.CreateThread(ctx, "tenant-a:room-1", ThreadRecord{ID: "thread-1"}); !errors.Is(err, ErrThreadAlreadyExists) {
		t.Fatalf("expected thread conflict, got %v", err)
	}
	if _, err := store.CreateThread(ctx, "tenant-a:room-1", ThreadRecord{
		ID: "thread-invalid-comment",
		Comments: []CommentRecord{{
			ID:     "comment-invalid",
			UserID: "user-1",
			Body:   json.RawMessage(`{`),
		}},
	}); err == nil {
		t.Fatalf("expected invalid initial thread comment to fail")
	}

	updated, err := store.AddComment(ctx, "tenant-a:room-1", "thread-1", CommentRecord{
		ID:     "comment-2",
		UserID: "user-2",
		Body:   json.RawMessage(`{"content":[{"type":"paragraph","text":"second"}]}`),
	})
	if err != nil {
		t.Fatalf("add comment: %v", err)
	}
	if len(updated.Comments) != 2 || updated.Comments[1].ID != "comment-2" || string(updated.Comments[1].Metadata) != `{}` {
		t.Fatalf("unexpected updated thread comments: %+v", updated.Comments)
	}
	patched, err := store.UpdateComment(ctx, "tenant-a:room-1", "thread-1", "comment-1", CommentUpdate{
		Body:        json.RawMessage(`{"content":[{"type":"paragraph","text":"edited"}]}`),
		BodySet:     true,
		Metadata:    json.RawMessage(`{"status":"resolved"}`),
		MetadataSet: true,
		Mentions:    []string{"user-3", "user-3"},
		MentionsSet: true,
		Reactions: []CommentReaction{
			{Emoji: "+1", UserID: "user-3"},
			{Emoji: "+1", UserID: "user-3"},
		},
		ReactionsSet: true,
	})
	if err != nil {
		t.Fatalf("update comment: %v", err)
	}
	if len(patched.Comments) != 2 {
		t.Fatalf("unexpected patched thread comments: %+v", patched.Comments)
	}
	comment := patched.Comments[0]
	if string(comment.Body) != `{"content":[{"type":"paragraph","text":"edited"}]}` || string(comment.Metadata) != `{"status":"resolved"}` || comment.EditedAt == nil {
		t.Fatalf("unexpected patched comment body/metadata/edit time: %+v", comment)
	}
	if len(comment.Mentions) != 1 || comment.Mentions[0] != "user-3" || len(comment.Reactions) != 1 || comment.Reactions[0].UserID != "user-3" {
		t.Fatalf("expected normalized patched mentions/reactions, got %+v", comment)
	}
	cleared, err := store.UpdateComment(ctx, "tenant-a:room-1", "thread-1", "comment-1", CommentUpdate{
		MentionsSet:  true,
		ReactionsSet: true,
	})
	if err != nil {
		t.Fatalf("clear comment mentions/reactions: %v", err)
	}
	if len(cleared.Comments[0].Mentions) != 0 || len(cleared.Comments[0].Reactions) != 0 {
		t.Fatalf("expected cleared mentions/reactions, got %+v", cleared.Comments[0])
	}
	if _, err := store.AddComment(ctx, "tenant-a:room-1", "missing-thread", CommentRecord{ID: "comment-3", UserID: "user-3", Body: json.RawMessage(`{}`)}); !errors.Is(err, ErrThreadNotFound) {
		t.Fatalf("expected missing thread, got %v", err)
	}
	if _, err := store.AddComment(ctx, "tenant-a:room-1", "thread-1", CommentRecord{ID: "comment-invalid", UserID: "user-3", Body: json.RawMessage(`{`)}); err == nil {
		t.Fatalf("expected invalid added comment to fail")
	}
	if _, err := store.UpdateComment(ctx, "tenant-a:room-1", "missing-thread", "comment-1", CommentUpdate{MetadataSet: true, Metadata: json.RawMessage(`{}`)}); !errors.Is(err, ErrThreadNotFound) {
		t.Fatalf("expected missing update thread, got %v", err)
	}
	if _, err := store.UpdateComment(ctx, "tenant-a:room-1", "thread-1", "missing-comment", CommentUpdate{MetadataSet: true, Metadata: json.RawMessage(`{}`)}); !errors.Is(err, ErrCommentNotFound) {
		t.Fatalf("expected missing update comment, got %v", err)
	}
	if _, err := store.UpdateComment(ctx, "tenant-a:room-1", "thread-1", "comment-1", CommentUpdate{BodySet: true, Body: json.RawMessage(`{`)}); err == nil {
		t.Fatalf("expected invalid updated comment to fail")
	}

	if _, err := store.CreateThread(ctx, "tenant-a:room-1", ThreadRecord{ID: "thread-0"}); err != nil {
		t.Fatalf("create second thread: %v", err)
	}
	if err := store.client.HSet(ctx, roomThreadKey("tenant-a:room-1", "thread-0"), "created_at", "not-time").Err(); err != nil {
		t.Fatalf("corrupt thread record: %v", err)
	}
	if _, err := store.ListThreads(ctx, "tenant-a:room-1"); err == nil {
		t.Fatalf("expected corrupt thread record to fail listing")
	}
	earlyTime := time.Date(2026, 5, 23, 11, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	lateTime := time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	if err := store.client.HSet(ctx, roomThreadKey("tenant-a:room-1", "thread-0"), "created_at", earlyTime).Err(); err != nil {
		t.Fatalf("restore thread-0 time: %v", err)
	}
	if err := store.client.HSet(ctx, roomThreadKey("tenant-a:room-1", "thread-1"), "created_at", lateTime).Err(); err != nil {
		t.Fatalf("seed thread-1 later time: %v", err)
	}
	threads, err := store.ListThreads(ctx, "tenant-a:room-1")
	if err != nil {
		t.Fatalf("list ordered threads: %v", err)
	}
	if len(threads) != 2 || threads[0].ID != "thread-0" || threads[1].ID != "thread-1" {
		t.Fatalf("expected threads to sort by creation time, got %+v", threads)
	}
	tieTime := time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	if err := store.client.HSet(ctx, roomThreadKey("tenant-a:room-1", "thread-1"), "created_at", tieTime).Err(); err != nil {
		t.Fatalf("seed thread-1 tie time: %v", err)
	}
	if err := store.client.HSet(ctx, roomThreadKey("tenant-a:room-1", "thread-0"), "created_at", tieTime).Err(); err != nil {
		t.Fatalf("seed thread-0 tie time: %v", err)
	}
	if err := store.client.SAdd(ctx, roomThreadsKey("tenant-a:room-1"), "missing-thread").Err(); err != nil {
		t.Fatalf("seed missing thread id: %v", err)
	}
	threads, err = store.ListThreads(ctx, "tenant-a:room-1")
	if err != nil {
		t.Fatalf("list threads: %v", err)
	}
	if len(threads) != 2 {
		t.Fatalf("unexpected thread count: %+v", threads)
	}
	if threads[0].ID != "thread-0" || threads[1].ID != "thread-1" {
		t.Fatalf("expected equal timestamp threads to sort by id, got %+v", threads)
	}
	threadIDs := map[string]bool{threads[0].ID: true, threads[1].ID: true}
	if !threadIDs["thread-1"] || !threadIDs["thread-0"] {
		t.Fatalf("unexpected threads: %+v", threads)
	}

	if err := store.client.RPush(ctx, roomThreadCommentsKey("tenant-a:room-1", "thread-1"), `{`).Err(); err != nil {
		t.Fatalf("seed broken comment: %v", err)
	}
	if _, err := store.ListThreads(ctx, "tenant-a:room-1"); err == nil {
		t.Fatalf("expected malformed thread comment to fail listing")
	}
}

func TestRedisInboxNotificationLifecycle(t *testing.T) {
	redisServer, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer redisServer.Close()

	store, err := NewRedisStore("redis://"+redisServer.Addr(), "room:")
	if err != nil {
		t.Fatalf("new redis store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	created, err := store.CreateInboxNotification(ctx, InboxNotificationRecord{
		ID:           "in_1",
		UserID:       "user-1",
		Kind:         "$custom",
		SubjectID:    "subject-1",
		RoomID:       "tenant-a:room-1",
		ActivityData: json.RawMessage(`{"count":1}`),
	})
	if err != nil {
		t.Fatalf("create inbox notification: %v", err)
	}
	if created.ID != "in_1" || created.UserID != "user-1" || created.ReadAt != nil {
		t.Fatalf("unexpected created notification: %+v", created)
	}
	if _, err := store.CreateInboxNotification(ctx, InboxNotificationRecord{ID: "in_1", UserID: "user-1", Kind: "thread"}); !errors.Is(err, ErrInboxAlreadyExists) {
		t.Fatalf("expected notification conflict, got %v", err)
	}
	if _, err := store.CreateInboxNotification(ctx, InboxNotificationRecord{ID: "in_2", UserID: "user-1", Kind: "thread"}); err != nil {
		t.Fatalf("create second inbox notification: %v", err)
	}

	list, err := store.ListInboxNotifications(ctx, "user-1", InboxNotificationListFilter{UnreadOnly: true, Limit: 1})
	if err != nil {
		t.Fatalf("list inbox notifications: %v", err)
	}
	if len(list.Data) != 1 || list.NextCursor == 0 {
		t.Fatalf("unexpected paginated inbox list: %+v", list)
	}
	get, err := store.GetInboxNotification(ctx, "user-1", "in_1")
	if err != nil {
		t.Fatalf("get inbox notification: %v", err)
	}
	if get.SubjectID != "subject-1" || string(get.ActivityData) != `{"count":1}` {
		t.Fatalf("unexpected notification: %+v", get)
	}
	if _, err := store.GetInboxNotification(ctx, "user-2", "in_1"); !errors.Is(err, ErrInboxNotFound) {
		t.Fatalf("expected user mismatch to hide notification, got %v", err)
	}
	if _, err := store.MarkInboxNotificationRead(ctx, "missing-notification"); !errors.Is(err, ErrInboxNotFound) {
		t.Fatalf("expected missing notification read failure, got %v", err)
	}
	if err := store.client.HSet(ctx, inboxNotificationKey("in_2"), "notified_at", "not-time").Err(); err != nil {
		t.Fatalf("corrupt inbox notification: %v", err)
	}
	if _, err := store.MarkInboxNotificationRead(ctx, "in_2"); err == nil {
		t.Fatalf("expected corrupt notification read to fail")
	}
	if err := store.client.HSet(ctx, inboxNotificationKey("in_2"), "notified_at", created.NotifiedAt.Format(time.RFC3339Nano)).Err(); err != nil {
		t.Fatalf("restore inbox notification: %v", err)
	}
	read, err := store.MarkInboxNotificationRead(ctx, "in_1")
	if err != nil {
		t.Fatalf("mark read: %v", err)
	}
	if read.ReadAt == nil {
		t.Fatalf("expected read timestamp: %+v", read)
	}
	readAgain, err := store.MarkInboxNotificationRead(ctx, "in_1")
	if err != nil {
		t.Fatalf("mark already read: %v", err)
	}
	if readAgain.ReadAt == nil {
		t.Fatalf("expected read timestamp to remain set: %+v", readAgain)
	}
	unread, err := store.ListInboxNotifications(ctx, "user-1", InboxNotificationListFilter{UnreadOnly: true, Limit: 50})
	if err != nil {
		t.Fatalf("list unread notifications: %v", err)
	}
	if len(unread.Data) != 1 || unread.Data[0].ID != "in_2" {
		t.Fatalf("unexpected unread list: %+v", unread)
	}
	if err := store.client.ZAdd(ctx, userInboxKey("user-1"), redis.Z{Score: 1, Member: "missing-notification"}).Err(); err != nil {
		t.Fatalf("seed stale inbox id: %v", err)
	}
	defaultLimitList, err := store.ListInboxNotifications(ctx, "user-1", InboxNotificationListFilter{})
	if err != nil {
		t.Fatalf("list inbox notifications with default limit: %v", err)
	}
	if len(defaultLimitList.Data) < 1 {
		t.Fatalf("expected default limit list to skip stale id and return existing notifications: %+v", defaultLimitList)
	}
	if err := store.DeleteInboxNotification(ctx, "user-1", "in_1"); err != nil {
		t.Fatalf("delete inbox notification: %v", err)
	}
	if _, err := store.GetInboxNotification(ctx, "user-1", "in_1"); !errors.Is(err, ErrInboxNotFound) {
		t.Fatalf("expected deleted notification to be missing, got %v", err)
	}
	if err := store.DeleteAllInboxNotifications(ctx, "user-1"); err != nil {
		t.Fatalf("delete all inbox notifications: %v", err)
	}
	list, err = store.ListInboxNotifications(ctx, "user-1", InboxNotificationListFilter{Limit: 50})
	if err != nil {
		t.Fatalf("list after delete all: %v", err)
	}
	if len(list.Data) != 0 {
		t.Fatalf("expected empty inbox after delete all: %+v", list)
	}

	settings, err := store.GetNotificationSettings(ctx, "user-1")
	if err != nil {
		t.Fatalf("get default notification settings: %v", err)
	}
	if string(settings) != `{}` {
		t.Fatalf("unexpected default settings: %s", settings)
	}
	if _, err := store.SetNotificationSettings(ctx, "user-1", json.RawMessage(`{"email":{"thread":true}}`)); err != nil {
		t.Fatalf("set notification settings: %v", err)
	}
	if _, err := store.SetNotificationSettings(ctx, "user-1", json.RawMessage(`{`)); err == nil {
		t.Fatalf("expected invalid notification settings JSON to fail")
	}
	settings, err = store.GetNotificationSettings(ctx, "user-1")
	if err != nil || string(settings) != `{"email":{"thread":true}}` {
		t.Fatalf("unexpected stored settings: %s err=%v", settings, err)
	}
	if err := store.DeleteNotificationSettings(ctx, "user-1"); err != nil {
		t.Fatalf("delete notification settings: %v", err)
	}

	defaultRoomSettings, err := store.GetRoomSubscriptionSettings(ctx, "tenant-a:room-1", "user-1")
	if err != nil {
		t.Fatalf("get default room subscription settings: %v", err)
	}
	if defaultRoomSettings.Threads != "all" || defaultRoomSettings.TextMentions != "mine" {
		t.Fatalf("unexpected default room settings: %+v", defaultRoomSettings)
	}
	storedRoomSettings, err := store.SetRoomSubscriptionSettings(ctx, RoomSubscriptionSettings{
		RoomID:       "tenant-a:room-1",
		UserID:       "user-1",
		Threads:      "none",
		TextMentions: "none",
	})
	if err != nil {
		t.Fatalf("set room subscription settings: %v", err)
	}
	if storedRoomSettings.Threads != "none" || storedRoomSettings.TextMentions != "none" {
		t.Fatalf("unexpected stored room settings: %+v", storedRoomSettings)
	}
	if _, err := store.SetRoomSubscriptionSettings(ctx, RoomSubscriptionSettings{
		RoomID:       "tenant-a:room-2",
		UserID:       "user-1",
		Threads:      "replies_and_mentions",
		TextMentions: "mine",
	}); err != nil {
		t.Fatalf("set second room subscription settings: %v", err)
	}
	roomSettingsList, err := store.ListRoomSubscriptionSettings(ctx, "user-1", 0, 1)
	if err != nil {
		t.Fatalf("list room subscription settings: %v", err)
	}
	if len(roomSettingsList.Data) != 1 || roomSettingsList.NextCursor == 0 {
		t.Fatalf("unexpected room subscription settings list: %+v", roomSettingsList)
	}
	defaultRoomSettingsList, err := store.ListRoomSubscriptionSettings(ctx, "user-1", 0, 0)
	if err != nil {
		t.Fatalf("list room subscription settings with default limit: %v", err)
	}
	if len(defaultRoomSettingsList.Data) != 2 {
		t.Fatalf("unexpected default room subscription settings list: %+v", defaultRoomSettingsList)
	}
	if err := store.client.HSet(ctx, roomSubscriptionSettingsKey("tenant-a:broken-room", "user-1"), map[string]any{
		"room_id":       "tenant-a:broken-room",
		"user_id":       "user-1",
		"threads":       "all",
		"text_mentions": "mine",
		"updated_at":    "not-time",
	}).Err(); err != nil {
		t.Fatalf("seed broken room subscription settings: %v", err)
	}
	if err := store.client.ZAdd(ctx, userRoomSubscriptionSettingsKey("user-1"), redis.Z{Score: 1, Member: "tenant-a:broken-room"}).Err(); err != nil {
		t.Fatalf("index broken room subscription settings: %v", err)
	}
	if _, err := store.ListRoomSubscriptionSettings(ctx, "user-1", 0, 50); err == nil {
		t.Fatalf("expected broken room subscription settings to fail listing")
	}
	if err := store.client.Del(ctx, roomSubscriptionSettingsKey("tenant-a:broken-room", "user-1")).Err(); err != nil {
		t.Fatalf("delete broken room subscription settings: %v", err)
	}
	if err := store.client.ZRem(ctx, userRoomSubscriptionSettingsKey("user-1"), "tenant-a:broken-room").Err(); err != nil {
		t.Fatalf("unindex broken room subscription settings: %v", err)
	}
	if err := store.DeleteRoomSubscriptionSettings(ctx, "tenant-a:room-1", "user-1"); err != nil {
		t.Fatalf("delete room subscription settings: %v", err)
	}
	if err := store.DeleteRoomSubscriptionSettings(ctx, "tenant-a:room-2", "user-1"); err != nil {
		t.Fatalf("delete second room subscription settings: %v", err)
	}
	roomSettingsList, err = store.ListRoomSubscriptionSettings(ctx, "user-1", 0, 50)
	if err != nil {
		t.Fatalf("list room subscription settings after delete: %v", err)
	}
	if len(roomSettingsList.Data) != 0 {
		t.Fatalf("expected empty room subscription settings list: %+v", roomSettingsList)
	}
}

func TestThreadRecordHelpersRejectInvalidData(t *testing.T) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	base := map[string]string{
		"type":       "thread",
		"id":         "thread-1",
		"room_id":    "tenant-a:room-1",
		"resolved":   "false",
		"metadata":   "",
		"created_at": now,
		"updated_at": now,
	}
	thread, err := decodeThreadRecord(base)
	if err != nil {
		t.Fatalf("decode thread: %v", err)
	}
	if string(thread.Metadata) != `{}` || thread.Type != "thread" {
		t.Fatalf("unexpected decoded thread: %+v", thread)
	}
	for _, tc := range []struct {
		name  string
		field string
		value string
	}{
		{name: "created at", field: "created_at", value: "not-time"},
		{name: "updated at", field: "updated_at", value: "not-time"},
		{name: "resolved", field: "resolved", value: "not-bool"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			values := make(map[string]string, len(base))
			for key, value := range base {
				values[key] = value
			}
			values[tc.field] = tc.value
			if _, err := decodeThreadRecord(values); err == nil {
				t.Fatalf("expected invalid %s to fail", tc.name)
			}
		})
	}

	if _, err := encodeCommentRecord(CommentRecord{ID: "comment-1", Body: json.RawMessage(`{`)}); err == nil {
		t.Fatalf("expected invalid raw comment body to fail encoding")
	}
	comment, err := decodeCommentRecord(`{"id":"comment-1","body":{},"metadata":{}}`)
	if err != nil {
		t.Fatalf("decode comment: %v", err)
	}
	if comment.Type != "comment" || string(comment.Body) != `{}` || string(comment.Metadata) != `{}` {
		t.Fatalf("unexpected decoded comment: %+v", comment)
	}
	normalized := normalizeCommentRecord(CommentRecord{ID: "comment-1"})
	if normalized.Type != "comment" || string(normalized.Body) != `{}` || string(normalized.Metadata) != `{}` {
		t.Fatalf("unexpected normalized comment: %+v", normalized)
	}
}

func TestNotificationRecordHelpersRejectInvalidData(t *testing.T) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	base := map[string]string{
		"id":            "in_1",
		"user_id":       "user-1",
		"kind":          "thread",
		"notified_at":   now,
		"activity_data": "",
	}
	notification, err := decodeInboxNotificationRecord(base)
	if err != nil {
		t.Fatalf("decode inbox notification: %v", err)
	}
	if string(notification.ActivityData) != `{}` {
		t.Fatalf("unexpected default activity data: %+v", notification)
	}
	readAt := time.Now().UTC()
	encoded := encodeInboxNotificationRecord(InboxNotificationRecord{
		ID:         "in_1",
		UserID:     "user-1",
		Kind:       "thread",
		NotifiedAt: time.Now().UTC(),
		ReadAt:     &readAt,
	})
	if encoded["read_at"] == "" {
		t.Fatalf("expected read_at to be encoded: %+v", encoded)
	}
	if _, err := decodeInboxNotificationRecord(map[string]string{"notified_at": "not-time"}); err == nil {
		t.Fatalf("expected bad notified_at to fail")
	}
	badReadAt := make(map[string]string, len(base))
	for key, value := range base {
		badReadAt[key] = value
	}
	badReadAt["read_at"] = "not-time"
	if _, err := decodeInboxNotificationRecord(badReadAt); err == nil {
		t.Fatalf("expected bad read_at to fail")
	}
	settings := defaultRoomSubscriptionSettings("tenant-a:room-1", "user-1")
	if settings.Threads != "all" || settings.TextMentions != "mine" {
		t.Fatalf("unexpected default subscription settings: %+v", settings)
	}
	encodedSettings := encodeRoomSubscriptionSettings(RoomSubscriptionSettings{
		RoomID:       "tenant-a:room-1",
		UserID:       "user-1",
		Threads:      "none",
		TextMentions: "none",
		UpdatedAt:    time.Now().UTC(),
	})
	decodedSettings, err := decodeRoomSubscriptionSettings(map[string]string{
		"room_id":       encodedSettings["room_id"].(string),
		"user_id":       encodedSettings["user_id"].(string),
		"threads":       encodedSettings["threads"].(string),
		"text_mentions": encodedSettings["text_mentions"].(string),
		"updated_at":    encodedSettings["updated_at"].(string),
	})
	if err != nil {
		t.Fatalf("decode room subscription settings: %v", err)
	}
	if decodedSettings.Threads != "none" || decodedSettings.TextMentions != "none" {
		t.Fatalf("unexpected decoded subscription settings: %+v", decodedSettings)
	}
	if _, err := decodeRoomSubscriptionSettings(map[string]string{"updated_at": "not-time"}); err == nil {
		t.Fatalf("expected bad subscription timestamp to fail")
	}
}

func TestRoomRecordDecodeRejectsInvalidFields(t *testing.T) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	base := map[string]string{
		"id":               "tenant-a:room-1",
		"metadata":         "",
		"default_accesses": `["room:read"]`,
		"users_accesses":   `{"user-1":["room:write"]}`,
		"groups_accesses":  `{"team-1":["room:presence:write"]}`,
		"created_at":       now,
		"updated_at":       now,
	}
	record, err := decodeRoomRecord(base)
	if err != nil {
		t.Fatalf("decode room record: %v", err)
	}
	if string(record.Metadata) != `{}` || record.ID != "tenant-a:room-1" {
		t.Fatalf("unexpected decoded room record: %+v", record)
	}

	tests := []struct {
		name  string
		field string
		value string
	}{
		{name: "created at", field: "created_at", value: "not-time"},
		{name: "updated at", field: "updated_at", value: "not-time"},
		{name: "default accesses", field: "default_accesses", value: `[`},
		{name: "user accesses", field: "users_accesses", value: `[`},
		{name: "group accesses", field: "groups_accesses", value: `[`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			values := make(map[string]string, len(base))
			for key, value := range base {
				values[key] = value
			}
			values[tc.field] = tc.value
			if _, err := decodeRoomRecord(values); err == nil {
				t.Fatalf("expected invalid %s to fail", tc.name)
			}
		})
	}
}

func TestRoomFromRecordKeyRejectsInvalidKeys(t *testing.T) {
	if room, ok := roomFromRecordKey("room:tenant-a:room-1:record"); !ok || room != "tenant-a:room-1" {
		t.Fatalf("expected valid record key, got room=%q ok=%v", room, ok)
	}
	for _, key := range []string{
		"",
		"room::record",
		"tenant-a:room-1:record",
		"room:tenant-a:room-1",
	} {
		if room, ok := roomFromRecordKey(key); ok || room != "" {
			t.Fatalf("expected invalid record key %q to fail, got room=%q ok=%v", key, room, ok)
		}
	}
}

func TestRedisConnectionCleanupAndReconcile(t *testing.T) {
	redisServer, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer redisServer.Close()

	store, err := NewRedisStore("redis://"+redisServer.Addr(), "room:")
	if err != nil {
		t.Fatalf("new redis store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	if err := store.TouchConnection(ctx, "conn-live", ConnectionMeta{
		NodeID:      "node-a",
		Subject:     "user-live",
		Tenant:      "tenant-a",
		ConnectedAt: time.Now(),
	}); err != nil {
		t.Fatalf("touch live connection: %v", err)
	}
	if err := store.TouchConnection(ctx, "conn-stale", ConnectionMeta{
		NodeID:      "node-a",
		Subject:     "user-stale",
		Tenant:      "tenant-a",
		ConnectedAt: time.Now(),
	}); err != nil {
		t.Fatalf("touch stale connection: %v", err)
	}
	for _, connID := range []string{"conn-live", "conn-stale"} {
		if err := store.JoinRoom(ctx, connID, "tenant-a:room-1"); err != nil {
			t.Fatalf("join room %s: %v", connID, err)
		}
		if err := store.SetPresence(ctx, connID, "tenant-a:room-1", json.RawMessage(`{"online":true}`)); err != nil {
			t.Fatalf("set presence %s: %v", connID, err)
		}
	}
	if err := store.ClearPresence(ctx, "conn-live", "tenant-a:room-1"); err != nil {
		t.Fatalf("clear presence: %v", err)
	}
	if err := store.SetPresence(ctx, "conn-live", "tenant-a:room-1", json.RawMessage(`{"online":true}`)); err != nil {
		t.Fatalf("restore presence: %v", err)
	}

	if err := store.client.Del(ctx, connAliveKey("conn-stale")).Err(); err != nil {
		t.Fatalf("expire stale connection: %v", err)
	}
	if err := store.ReconcileNode(ctx, "node-a"); err != nil {
		t.Fatalf("reconcile node: %v", err)
	}

	snapshot, err := store.SnapshotRoom(ctx, "tenant-a:room-1")
	if err != nil {
		t.Fatalf("snapshot after reconcile: %v", err)
	}
	if len(snapshot.Members) != 1 || snapshot.Members[0] != "conn-live" {
		t.Fatalf("expected only live member after reconcile, got %+v", snapshot.Members)
	}
	if _, ok := snapshot.Presence["conn-stale"]; ok {
		t.Fatalf("expected stale presence to be removed: %+v", snapshot.Presence)
	}
	exists, err := store.client.Exists(ctx, connMetaKey("conn-stale"), connRoomsKey("conn-stale")).Result()
	if err != nil {
		t.Fatalf("check stale keys: %v", err)
	}
	if exists != 0 {
		t.Fatalf("expected stale connection keys to be deleted")
	}

	if err := store.CleanupConnection(ctx, "node-a", "conn-live"); err != nil {
		t.Fatalf("cleanup live connection: %v", err)
	}
	snapshot, err = store.SnapshotRoom(ctx, "tenant-a:room-1")
	if err != nil {
		t.Fatalf("snapshot after cleanup: %v", err)
	}
	if len(snapshot.Members) != 0 || len(snapshot.Presence) != 0 {
		t.Fatalf("expected room to be empty after cleanup, got %+v", snapshot)
	}
}

func TestRedisAggregateStatsParsesInvalidValuesAsZero(t *testing.T) {
	redisServer, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer redisServer.Close()

	store, err := NewRedisStore("redis://"+redisServer.Addr(), "room:")
	if err != nil {
		t.Fatalf("new redis store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	if err := store.SyncStats(ctx, "node-a", stats.Snapshot{
		ActiveConnections:    2,
		ActiveRooms:          1,
		JoinsTotal:           3,
		LeavesTotal:          4,
		EventsTotal:          5,
		PresenceUpdatesTotal: 6,
		QueueOverflowsTotal:  7,
		AdminPublishesTotal:  8,
	}); err != nil {
		t.Fatalf("sync stats: %v", err)
	}
	if err := store.client.SAdd(ctx, statsNodesKey(), "node-b").Err(); err != nil {
		t.Fatalf("seed node-b stats member: %v", err)
	}
	if err := store.client.HSet(ctx, nodeStatsKey("node-b"), map[string]any{
		"active_connections":     "not-int",
		"active_rooms":           "9",
		"joins_total":            "",
		"leaves_total":           "1",
		"events_total":           "bad",
		"presence_updates_total": "2",
		"queue_overflows_total":  "bad",
		"admin_publishes_total":  "3",
	}).Err(); err != nil {
		t.Fatalf("seed node-b stats: %v", err)
	}
	aggregate, err := store.AggregateStats(ctx)
	if err != nil {
		t.Fatalf("aggregate stats: %v", err)
	}
	if aggregate.ActiveConnections != 2 ||
		aggregate.ActiveRooms != 10 ||
		aggregate.JoinsTotal != 3 ||
		aggregate.LeavesTotal != 5 ||
		aggregate.EventsTotal != 5 ||
		aggregate.PresenceUpdatesTotal != 8 ||
		aggregate.QueueOverflowsTotal != 7 ||
		aggregate.AdminPublishesTotal != 11 {
		t.Fatalf("unexpected aggregate stats: %+v", aggregate)
	}
}

func TestRedisActiveUsers(t *testing.T) {
	redisServer, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer redisServer.Close()

	store, err := NewRedisStore("redis://"+redisServer.Addr(), "room:")
	if err != nil {
		t.Fatalf("new redis store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	connectedAt := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	if err := store.TouchConnection(ctx, "conn-b", ConnectionMeta{
		NodeID:      "node-a",
		Subject:     "user-b",
		Tenant:      "tenant-a",
		ConnectedAt: connectedAt,
	}); err != nil {
		t.Fatalf("touch conn-b: %v", err)
	}
	if err := store.TouchConnection(ctx, "conn-a", ConnectionMeta{
		NodeID:      "node-a",
		Subject:     "user-a",
		Tenant:      "tenant-a",
		ConnectedAt: connectedAt,
	}); err != nil {
		t.Fatalf("touch conn-a: %v", err)
	}
	for _, connID := range []string{"conn-b", "conn-a"} {
		if err := store.JoinRoom(ctx, connID, "tenant-a:room-1"); err != nil {
			t.Fatalf("join room %s: %v", connID, err)
		}
	}
	if err := store.SetPresence(ctx, "conn-a", "tenant-a:room-1", json.RawMessage(`{"cursor":{"x":1}}`)); err != nil {
		t.Fatalf("set presence: %v", err)
	}
	if err := store.SetEphemeralPresence(ctx, "agent-1", "tenant-a:room-1", json.RawMessage(`{"kind":"agent"}`), time.Minute); err != nil {
		t.Fatalf("set ephemeral presence: %v", err)
	}

	users, err := store.ActiveUsers(ctx, "tenant-a:room-1")
	if err != nil {
		t.Fatalf("active users: %v", err)
	}
	if len(users) != 3 {
		t.Fatalf("expected three active users, got %+v", users)
	}
	if users[0].ConnectionID != "agent-1" || users[0].ID != "agent-1" || string(users[0].Presence) != `{"kind":"agent"}` {
		t.Fatalf("unexpected ephemeral active user: %+v", users[0])
	}
	if users[1].ConnectionID != "conn-a" || users[1].ID != "user-a" || users[1].Tenant != "tenant-a" || users[1].NodeID != "node-a" {
		t.Fatalf("unexpected conn-a active user: %+v", users[1])
	}
	if string(users[1].Presence) != `{"cursor":{"x":1}}` {
		t.Fatalf("unexpected conn-a presence: %s", users[1].Presence)
	}
	if !users[1].ConnectedAt.Equal(connectedAt) {
		t.Fatalf("unexpected connected_at: %s", users[1].ConnectedAt)
	}
	if users[2].ConnectionID != "conn-b" || users[2].ID != "user-b" {
		t.Fatalf("unexpected conn-b active user: %+v", users[2])
	}
}

func TestRedisStorageLifecycleAndPatch(t *testing.T) {
	redisServer, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer redisServer.Close()

	store, err := NewRedisStore("redis://"+redisServer.Addr(), "room:")
	if err != nil {
		t.Fatalf("new redis store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	if _, err := store.GetStorage(ctx, "tenant-a:doc-1"); !errors.Is(err, ErrStorageNotFound) {
		t.Fatalf("expected storage not found, got %v", err)
	}
	if _, err := store.SetStorage(ctx, "tenant-a:doc-1", json.RawMessage(`{"broken":`)); err == nil {
		t.Fatalf("expected invalid storage JSON to fail")
	}
	if _, err := store.SetStorage(ctx, "tenant-a:doc-1", json.RawMessage(`[]`)); !errors.Is(err, ErrStoragePatch) {
		t.Fatalf("expected non-object storage rejection, got %v", err)
	}
	if _, err := store.ApplyStoragePatch(ctx, "tenant-a:missing", []JSONPatchOperation{
		{Op: "add", Path: "/title", Value: json.RawMessage(`"Draft"`)},
	}, 1024); !errors.Is(err, ErrStorageNotFound) {
		t.Fatalf("expected missing storage patch failure, got %v", err)
	}

	stored, err := store.SetStorage(ctx, "tenant-a:doc-1", json.RawMessage(`{"layers":["base"],"meta":{"title":"Draft"}}`))
	if err != nil {
		t.Fatalf("set storage: %v", err)
	}
	if string(stored) != `{"layers":["base"],"meta":{"title":"Draft"}}` {
		t.Fatalf("unexpected stored storage: %s", stored)
	}
	snapshot, sequence, err := store.GetStorageWithSequence(ctx, "tenant-a:doc-1")
	if err != nil {
		t.Fatalf("get storage with sequence: %v", err)
	}
	if string(snapshot) != string(stored) || sequence != 1 {
		t.Fatalf("unexpected sequenced storage snapshot: document=%s sequence=%d", snapshot, sequence)
	}

	if _, _, err := store.SetStorageWithOptions(ctx, "tenant-a:doc-1", json.RawMessage(`{"layers":["stale"],"meta":{"title":"Stale"}}`), StorageWriteOptions{
		ExpectedSequence:    0,
		ExpectedSequenceSet: true,
	}); !errors.Is(err, ErrStorageConflict) {
		t.Fatalf("expected stale storage set conflict, got %v", err)
	}

	patched, sequence, err := store.ApplyStoragePatchWithOptions(ctx, "tenant-a:doc-1", []JSONPatchOperation{
		{Op: "test", Path: "/meta/title", Value: json.RawMessage(`"Draft"`)},
		{Op: "add", Path: "/layers/-", Value: json.RawMessage(`"foreground"`)},
		{Op: "replace", Path: "/meta/title", Value: json.RawMessage(`"Published"`)},
		{Op: "copy", From: "/meta/title", Path: "/meta/copy"},
		{Op: "move", From: "/meta/copy", Path: "/meta/slug"},
	}, StorageWriteOptions{
		MaxBytes:            1024,
		ExpectedSequence:    1,
		ExpectedSequenceSet: true,
	})
	if err != nil {
		t.Fatalf("apply storage patch: %v", err)
	}
	if sequence != 2 {
		t.Fatalf("expected storage sequence 2, got %d", sequence)
	}
	if string(patched) != `{"layers":["base","foreground"],"meta":{"slug":"Published","title":"Published"}}` {
		t.Fatalf("unexpected patched storage: %s", patched)
	}
	snapshot, sequence, err = store.GetStorageWithSequence(ctx, "tenant-a:doc-1")
	if err != nil {
		t.Fatalf("get patched storage with sequence: %v", err)
	}
	if string(snapshot) != string(patched) || sequence != 2 {
		t.Fatalf("unexpected patched sequenced storage snapshot: document=%s sequence=%d", snapshot, sequence)
	}

	if _, _, err := store.ApplyStoragePatchWithOptions(ctx, "tenant-a:doc-1", []JSONPatchOperation{
		{Op: "replace", Path: "/meta/title", Value: json.RawMessage(`"Stale"`)},
	}, StorageWriteOptions{
		MaxBytes:            1024,
		ExpectedSequence:    1,
		ExpectedSequenceSet: true,
	}); !errors.Is(err, ErrStorageConflict) {
		t.Fatalf("expected stale storage patch conflict, got %v", err)
	}

	if _, err := store.ApplyStoragePatch(ctx, "tenant-a:doc-1", []JSONPatchOperation{
		{Op: "remove", Path: "/missing"},
	}, 1024); !errors.Is(err, ErrStoragePatch) {
		t.Fatalf("expected patch failure, got %v", err)
	}
	current, err := store.GetStorage(ctx, "tenant-a:doc-1")
	if err != nil {
		t.Fatalf("get storage after failed patch: %v", err)
	}
	if string(current) != string(patched) {
		t.Fatalf("failed patch should not mutate storage: %s", current)
	}

	if _, err := store.ApplyStoragePatch(ctx, "tenant-a:doc-1", []JSONPatchOperation{
		{Op: "add", Path: "/oversized", Value: json.RawMessage(`"0123456789"`)},
	}, 10); !errors.Is(err, ErrStoragePatch) {
		t.Fatalf("expected oversized patch failure, got %v", err)
	}

	if err := store.DeleteStorage(ctx, "tenant-a:doc-1"); err != nil {
		t.Fatalf("delete storage: %v", err)
	}
	if err := store.DeleteStorage(ctx, "tenant-a:doc-1"); !errors.Is(err, ErrStorageNotFound) {
		t.Fatalf("expected missing storage delete failure, got %v", err)
	}
}

func TestRedisStorageValidatesTypedLiveblocksStorage(t *testing.T) {
	redisServer, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer redisServer.Close()

	store, err := NewRedisStore("redis://"+redisServer.Addr(), "room:")
	if err != nil {
		t.Fatalf("new redis store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	valid := json.RawMessage(`{
		"liveblocksType":"LiveObject",
		"data":{
			"title":"Draft",
			"items":{"liveblocksType":"LiveList","data":["a","b"]},
			"props":{"liveblocksType":"LiveMap","data":{"visible":true}},
			"child":{"liveblocksType":"LiveObject","data":{"x":1}}
		}
	}`)
	stored, err := store.SetStorage(ctx, "tenant-a:typed-doc", valid)
	if err != nil {
		t.Fatalf("set typed storage: %v", err)
	}
	if string(stored) != `{"liveblocksType":"LiveObject","data":{"title":"Draft","items":{"liveblocksType":"LiveList","data":["a","b"]},"props":{"liveblocksType":"LiveMap","data":{"visible":true}},"child":{"liveblocksType":"LiveObject","data":{"x":1}}}}` {
		t.Fatalf("unexpected typed storage: %s", stored)
	}

	if err := ValidateStorageDocument(json.RawMessage(`{`)); !errors.Is(err, ErrStoragePatch) {
		t.Fatalf("expected malformed storage document validation failure, got %v", err)
	}

	invalidDocuments := []json.RawMessage{
		json.RawMessage(`[]`),
		json.RawMessage(`{"liveblocksType":"LiveList","data":[]}`),
		json.RawMessage(`{"liveblocksType":"LiveObject","data":[]}`),
		json.RawMessage(`{"child":{"liveblocksType":"LiveObject","data":[]}}`),
		json.RawMessage(`{"items":[{"liveblocksType":"LiveMap","data":[]}]}`),
		json.RawMessage(`{"liveblocksType":"LiveObject","data":{"items":{"liveblocksType":"LiveList","data":{}}}}`),
		json.RawMessage(`{"liveblocksType":"LiveObject","data":{"items":{"liveblocksType":"LiveList","data":[{"liveblocksType":"LiveMap","data":[]}]}}}`),
		json.RawMessage(`{"liveblocksType":"LiveObject","data":{"bad":{"liveblocksType":"LiveSet","data":[]}}}`),
		json.RawMessage(`{"liveblocksType":1,"data":{}}`),
		json.RawMessage(`{"liveblocksType":"LiveObject"}`),
	}
	for _, document := range invalidDocuments {
		if _, err := store.SetStorage(ctx, "tenant-a:typed-doc-invalid", document); !errors.Is(err, ErrStoragePatch) {
			t.Fatalf("expected typed storage validation failure for %s, got %v", document, err)
		}
	}

	if _, err := store.ApplyStoragePatch(ctx, "tenant-a:typed-doc", []JSONPatchOperation{
		{Op: "replace", Path: "/data/items/data", Value: json.RawMessage(`{}`)},
	}, 2048); !errors.Is(err, ErrStoragePatch) {
		t.Fatalf("expected typed patch validation failure, got %v", err)
	}
	current, err := store.GetStorage(ctx, "tenant-a:typed-doc")
	if err != nil {
		t.Fatalf("get storage after invalid typed patch: %v", err)
	}
	if string(current) != string(stored) {
		t.Fatalf("invalid typed patch should not mutate storage: %s", current)
	}

	patched, err := store.ApplyStoragePatch(ctx, "tenant-a:typed-doc", []JSONPatchOperation{
		{Op: "add", Path: "/data/items/data/-", Value: json.RawMessage(`"c"`)},
	}, 2048)
	if err != nil {
		t.Fatalf("apply valid typed patch: %v", err)
	}
	if string(patched) != `{"data":{"child":{"data":{"x":1},"liveblocksType":"LiveObject"},"items":{"data":["a","b","c"],"liveblocksType":"LiveList"},"props":{"data":{"visible":true},"liveblocksType":"LiveMap"},"title":"Draft"},"liveblocksType":"LiveObject"}` {
		t.Fatalf("unexpected patched typed storage: %s", patched)
	}
}

func TestApplyJSONPatchPointerAndArraySemantics(t *testing.T) {
	result, err := ApplyJSONPatch(json.RawMessage(`{"a/b":{"~key":[1,2]}}`), []JSONPatchOperation{
		{Op: "add", Path: "/a~1b/~0key/1", Value: json.RawMessage(`1.5`)},
		{Op: "remove", Path: "/a~1b/~0key/0"},
	})
	if err != nil {
		t.Fatalf("apply escaped patch: %v", err)
	}
	if string(result) != `{"a/b":{"~key":[1.5,2]}}` {
		t.Fatalf("unexpected escaped result: %s", result)
	}

	result, err = ApplyJSONPatch(json.RawMessage(`{"items":[{"title":"Draft","tags":["a","b"]}]}`), []JSONPatchOperation{
		{Op: "add", Path: "/items/0/subtitle", Value: json.RawMessage(`"Sub"`)},
		{Op: "remove", Path: "/items/0/tags/0"},
	})
	if err != nil {
		t.Fatalf("apply nested array patch: %v", err)
	}
	if string(result) != `{"items":[{"subtitle":"Sub","tags":["b"],"title":"Draft"}]}` {
		t.Fatalf("unexpected nested result: %s", result)
	}

	if _, err := ApplyJSONPatch(json.RawMessage(`{"items":[]}`), []JSONPatchOperation{
		{Op: "add", Path: "items/0", Value: json.RawMessage(`true`)},
	}); !errors.Is(err, ErrStoragePatch) {
		t.Fatalf("expected invalid pointer failure, got %v", err)
	}
	if _, err := ApplyJSONPatch(json.RawMessage(`{"items":[]}`), []JSONPatchOperation{
		{Op: "add", Path: "/items/~2", Value: json.RawMessage(`true`)},
	}); !errors.Is(err, ErrStoragePatch) {
		t.Fatalf("expected invalid pointer escape failure, got %v", err)
	}
	if _, err := ApplyJSONPatch(json.RawMessage(`{"items":[]}`), []JSONPatchOperation{
		{Op: "replace", Path: "", Value: json.RawMessage(`[]`)},
	}); !errors.Is(err, ErrStoragePatch) {
		t.Fatalf("expected non-object root failure, got %v", err)
	}
}

func TestApplyJSONPatchRejectsInvalidOperations(t *testing.T) {
	if result, err := ApplyJSONPatch(json.RawMessage(`{"title":"Draft"}`), []JSONPatchOperation{
		{Op: "replace", Path: "", Value: json.RawMessage(`{"title":"Published"}`)},
	}); err != nil || string(result) != `{"title":"Published"}` {
		t.Fatalf("expected root object replacement, got %s %v", result, err)
	}

	tests := []struct {
		name       string
		document   json.RawMessage
		operations []JSONPatchOperation
	}{
		{
			name:     "invalid document",
			document: json.RawMessage(`{"title":`),
			operations: []JSONPatchOperation{
				{Op: "test", Path: "", Value: json.RawMessage(`{}`)},
			},
		},
		{
			name:     "add missing operation value",
			document: json.RawMessage(`{"title":"Draft"}`),
			operations: []JSONPatchOperation{
				{Op: "add", Path: "/title"},
			},
		},
		{
			name:     "replace missing operation value",
			document: json.RawMessage(`{"title":"Draft"}`),
			operations: []JSONPatchOperation{
				{Op: "replace", Path: "/title"},
			},
		},
		{
			name:     "test missing operation value",
			document: json.RawMessage(`{"title":"Draft"}`),
			operations: []JSONPatchOperation{
				{Op: "test", Path: "/title"},
			},
		},
		{
			name:     "invalid operation value",
			document: json.RawMessage(`{"title":"Draft"}`),
			operations: []JSONPatchOperation{
				{Op: "test", Path: "/title", Value: json.RawMessage(`"Draft`)},
			},
		},
		{
			name:     "unsupported operation",
			document: json.RawMessage(`{"title":"Draft"}`),
			operations: []JSONPatchOperation{
				{Op: "increment", Path: "/counter", Value: json.RawMessage(`1`)},
			},
		},
		{
			name:     "test mismatch",
			document: json.RawMessage(`{"title":"Draft"}`),
			operations: []JSONPatchOperation{
				{Op: "test", Path: "/title", Value: json.RawMessage(`"Published"`)},
			},
		},
		{
			name:     "replace missing path",
			document: json.RawMessage(`{"title":"Draft"}`),
			operations: []JSONPatchOperation{
				{Op: "replace", Path: "/missing", Value: json.RawMessage(`true`)},
			},
		},
		{
			name:     "copy missing source",
			document: json.RawMessage(`{"title":"Draft"}`),
			operations: []JSONPatchOperation{
				{Op: "copy", From: "/missing", Path: "/copy"},
			},
		},
		{
			name:     "copy invalid source pointer",
			document: json.RawMessage(`{"title":"Draft"}`),
			operations: []JSONPatchOperation{
				{Op: "copy", From: "missing", Path: "/copy"},
			},
		},
		{
			name:     "move missing source",
			document: json.RawMessage(`{"title":"Draft"}`),
			operations: []JSONPatchOperation{
				{Op: "move", From: "/missing", Path: "/copy"},
			},
		},
		{
			name:     "move invalid source pointer",
			document: json.RawMessage(`{"title":"Draft"}`),
			operations: []JSONPatchOperation{
				{Op: "move", From: "missing", Path: "/copy"},
			},
		},
		{
			name:     "remove root",
			document: json.RawMessage(`{"title":"Draft"}`),
			operations: []JSONPatchOperation{
				{Op: "remove", Path: ""},
			},
		},
		{
			name:     "add missing object parent",
			document: json.RawMessage(`{"title":"Draft"}`),
			operations: []JSONPatchOperation{
				{Op: "add", Path: "/meta/title", Value: json.RawMessage(`"Draft"`)},
			},
		},
		{
			name:     "add into scalar",
			document: json.RawMessage(`{"title":"Draft"}`),
			operations: []JSONPatchOperation{
				{Op: "add", Path: "/title/value", Value: json.RawMessage(`true`)},
			},
		},
		{
			name:     "add array index out of range",
			document: json.RawMessage(`{"items":[]}`),
			operations: []JSONPatchOperation{
				{Op: "add", Path: "/items/1", Value: json.RawMessage(`true`)},
			},
		},
		{
			name:     "add through array append token",
			document: json.RawMessage(`{"items":[]}`),
			operations: []JSONPatchOperation{
				{Op: "add", Path: "/items/-/title", Value: json.RawMessage(`"Draft"`)},
			},
		},
		{
			name:     "add through scalar array element",
			document: json.RawMessage(`{"items":[true]}`),
			operations: []JSONPatchOperation{
				{Op: "add", Path: "/items/0/title", Value: json.RawMessage(`"Draft"`)},
			},
		},
		{
			name:     "remove array append token",
			document: json.RawMessage(`{"items":[true]}`),
			operations: []JSONPatchOperation{
				{Op: "remove", Path: "/items/-"},
			},
		},
		{
			name:     "remove negative array index",
			document: json.RawMessage(`{"items":[true]}`),
			operations: []JSONPatchOperation{
				{Op: "remove", Path: "/items/-1"},
			},
		},
		{
			name:     "remove array index out of range",
			document: json.RawMessage(`{"items":[true]}`),
			operations: []JSONPatchOperation{
				{Op: "remove", Path: "/items/1"},
			},
		},
		{
			name:     "remove through scalar",
			document: json.RawMessage(`{"items":[true]}`),
			operations: []JSONPatchOperation{
				{Op: "remove", Path: "/items/0/value"},
			},
		},
		{
			name:     "remove missing nested object path",
			document: json.RawMessage(`{"items":[{}]}`),
			operations: []JSONPatchOperation{
				{Op: "remove", Path: "/items/0/missing/value"},
			},
		},
		{
			name:     "get through scalar",
			document: json.RawMessage(`{"flag":true}`),
			operations: []JSONPatchOperation{
				{Op: "test", Path: "/flag/value", Value: json.RawMessage(`true`)},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ApplyJSONPatch(tc.document, tc.operations); !errors.Is(err, ErrStoragePatch) {
				t.Fatalf("expected storage patch failure, got %v", err)
			}
		})
	}
}

func TestJSONValueAndYJSRecordHelpers(t *testing.T) {
	root := map[string]any{
		"items": []any{
			map[string]any{"title": "Draft"},
		},
	}
	if value, err := getJSONValue(root, nil); err != nil || value == nil {
		t.Fatalf("expected root value, got value=%v err=%v", value, err)
	}
	if value, err := getJSONValue(root, []string{"items", "0", "title"}); err != nil || value != "Draft" {
		t.Fatalf("expected nested title, got value=%v err=%v", value, err)
	}
	if _, err := getJSONValue(root, []string{"missing"}); !errors.Is(err, ErrStoragePatch) {
		t.Fatalf("expected missing object path failure, got %v", err)
	}
	if _, err := getJSONValue(root, []string{"items", "1"}); !errors.Is(err, ErrStoragePatch) {
		t.Fatalf("expected array index failure, got %v", err)
	}
	if _, err := getJSONValue(root, []string{"items", "0", "title", "value"}); !errors.Is(err, ErrStoragePatch) {
		t.Fatalf("expected scalar traversal failure, got %v", err)
	}

	if compacted, err := compactJSON(json.RawMessage(` { "ok" : true } `)); err != nil || string(compacted) != `{"ok":true}` {
		t.Fatalf("unexpected compact JSON result: %s %v", compacted, err)
	}
	if _, err := compactJSON(json.RawMessage(`{"ok":`)); err == nil {
		t.Fatalf("expected invalid JSON compaction to fail")
	}

	updateRecord, err := decodeYJSUpdateRecord(`{"seq":7,"update":"dXBkYXRl"}`)
	if err != nil {
		t.Fatalf("decode update record: %v", err)
	}
	if updateRecord.Seq != 7 || updateRecord.KindValue() != YJSEventUpdate || string(updateRecord.Update) != "update" {
		t.Fatalf("unexpected update record: %+v", updateRecord)
	}
	subdocRecord, err := decodeYJSUpdateRecord(`{"seq":8,"kind":5,"update":"c3ViZG9j"}`)
	if err != nil {
		t.Fatalf("decode subdoc update record: %v", err)
	}
	if subdocRecord.Seq != 8 || subdocRecord.KindValue() != YJSEventSubdocUpdate || string(subdocRecord.Update) != "subdoc" {
		t.Fatalf("unexpected subdoc update record: %+v", subdocRecord)
	}
	for _, raw := range []string{
		`{`,
		`{"seq":0,"update":"dXBkYXRl"}`,
		`{"seq":1,"update":""}`,
		`{"seq":1,"kind":4,"update":"ZGlmZg=="}`,
		`{"seq":1,"kind":6,"update":"c3RhdGUtdmVjdG9y"}`,
	} {
		if _, err := decodeYJSUpdateRecord(raw); err == nil {
			t.Fatalf("expected invalid update record %q to fail", raw)
		}
	}

	snapshotRecord, err := decodeYJSSnapshotRecord(`{"checkpoint_seq":7,"snapshot":"c25hcA=="}`)
	if err != nil {
		t.Fatalf("decode snapshot record: %v", err)
	}
	if snapshotRecord.CheckpointSeq != 7 || string(snapshotRecord.Snapshot) != "snap" {
		t.Fatalf("unexpected snapshot record: %+v", snapshotRecord)
	}
	if snapshotRecord.Hash != YJSSnapshotHash([]byte("snap")) {
		t.Fatalf("expected missing snapshot hash to be filled, got %q", snapshotRecord.Hash)
	}
	for _, raw := range []string{
		`{`,
		`{"checkpoint_seq":-1,"snapshot":"c25hcA=="}`,
		`{"checkpoint_seq":0,"snapshot":""}`,
		`{"checkpoint_seq":0,"snapshot":"c25hcA==","hash":"00000000"}`,
	} {
		if _, err := decodeYJSSnapshotRecord(raw); err == nil {
			t.Fatalf("expected invalid snapshot record %q to fail", raw)
		}
	}
}

func assertYJSUpdates(t *testing.T, doc YJSDocument, wantUpdates []string, wantSeqs []int64, wantKinds []YJSEventKind) {
	t.Helper()
	if len(doc.Updates) != len(wantUpdates) || len(doc.UpdateSequences) != len(wantSeqs) || len(doc.UpdateKinds) != len(wantKinds) {
		t.Fatalf("unexpected update counts: updates=%d seqs=%d kinds=%d", len(doc.Updates), len(doc.UpdateSequences), len(doc.UpdateKinds))
	}
	for i, want := range wantUpdates {
		if string(doc.Updates[i]) != want {
			t.Fatalf("update %d: expected %q, got %q", i, want, string(doc.Updates[i]))
		}
	}
	for i, want := range wantSeqs {
		if doc.UpdateSequences[i] != want {
			t.Fatalf("sequence %d: expected %d, got %d", i, want, doc.UpdateSequences[i])
		}
	}
	for i, want := range wantKinds {
		if doc.UpdateKinds[i] != want {
			t.Fatalf("kind %d: expected %d, got %d", i, want, doc.UpdateKinds[i])
		}
	}
}

func receiveClusterTestValue[T any](t *testing.T, ch <-chan T, label string) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(time.Second):
		var zero T
		t.Fatalf("timed out waiting for %s", label)
		return zero
	}
}

type redisCommandFailureHook struct {
	processFailures  map[string]error
	pipelineFailures map[string]error
	processFailure   func(redis.Cmder) error
}

func (h *redisCommandFailureHook) clear() {
	h.processFailures = nil
	h.pipelineFailures = nil
	h.processFailure = nil
}

func (h *redisCommandFailureHook) DialHook(next redis.DialHook) redis.DialHook {
	return next
}

func (h *redisCommandFailureHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		if err := h.failureFor(cmd, h.processFailures); err != nil {
			return err
		}
		return next(ctx, cmd)
	}
}

func (h *redisCommandFailureHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		for _, cmd := range cmds {
			if err := h.failureFor(cmd, h.pipelineFailures); err != nil {
				return err
			}
		}
		return next(ctx, cmds)
	}
}

func (h *redisCommandFailureHook) failureFor(cmd redis.Cmder, failures map[string]error) error {
	if len(failures) == 0 {
		if h.processFailure != nil {
			return h.processFailure(cmd)
		}
		return nil
	}
	if err := failures[strings.ToLower(cmd.Name())]; err != nil {
		return err
	}
	if h.processFailure != nil {
		return h.processFailure(cmd)
	}
	return nil
}
