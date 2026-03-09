package cluster

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/openrtc/openrtc/server/internal/stats"
)

const (
	aliveTTL = 45 * time.Second
)

type PublishedEvent struct {
	Room                string          `json:"room"`
	Event               string          `json:"event"`
	Payload             json.RawMessage `json:"payload"`
	ExcludeSenderConnID string          `json:"exclude_sender_conn_id,omitempty"`
	TraceID             string          `json:"trace_id,omitempty"`
	OriginNode          string          `json:"origin_node"`
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

type Store interface {
	Healthy(ctx context.Context) error
	PublishEvent(ctx context.Context, event PublishedEvent) error
	Subscribe(ctx context.Context, handler func(PublishedEvent)) error
	TouchConnection(ctx context.Context, connID string, meta ConnectionMeta) error
	JoinRoom(ctx context.Context, connID string, room string) error
	LeaveRoom(ctx context.Context, connID string, room string) error
	SetPresence(ctx context.Context, connID string, room string, payload json.RawMessage) error
	ClearPresence(ctx context.Context, connID string, room string) error
	SnapshotRoom(ctx context.Context, room string) (Snapshot, error)
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

func (s *RedisStore) PublishEvent(ctx context.Context, event PublishedEvent) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}
	return s.client.Publish(ctx, s.channelPrefix+event.Room, payload).Err()
}

func (s *RedisStore) Subscribe(ctx context.Context, handler func(PublishedEvent)) error {
	pubsub := s.client.PSubscribe(ctx, s.channelPrefix+"*")
	if _, err := pubsub.Receive(ctx); err != nil {
		_ = pubsub.Close()
		return err
	}

	go func() {
		defer pubsub.Close()
		channel := pubsub.Channel()
		for {
			select {
			case <-ctx.Done():
				return
			case message, ok := <-channel:
				if !ok {
					return
				}
				var event PublishedEvent
				if err := json.Unmarshal([]byte(message.Payload), &event); err != nil {
					continue
				}
				handler(event)
			}
		}
	}()

	return nil
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

func (s *RedisStore) ClearPresence(ctx context.Context, connID string, room string) error {
	return s.client.HDel(ctx, roomPresenceKey(room), connID).Err()
}

func (s *RedisStore) SnapshotRoom(ctx context.Context, room string) (Snapshot, error) {
	pipe := s.client.TxPipeline()
	members := pipe.SMembers(ctx, roomMembersKey(room))
	presence := pipe.HGetAll(ctx, roomPresenceKey(room))
	_, err := pipe.Exec(ctx)
	if err != nil && err != redis.Nil {
		return Snapshot{}, err
	}

	rawPresence := make(map[string]json.RawMessage, len(presence.Val()))
	for connID, state := range presence.Val() {
		rawPresence[connID] = json.RawMessage(state)
	}

	return Snapshot{
		Members:  members.Val(),
		Presence: rawPresence,
	}, nil
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
