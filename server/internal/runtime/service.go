package runtimeapp

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/openrtc/openrtc/server/internal/auth"
	"github.com/openrtc/openrtc/server/internal/cluster"
	"github.com/openrtc/openrtc/server/internal/config"
	openrtcerr "github.com/openrtc/openrtc/server/internal/errors"
	"github.com/openrtc/openrtc/server/internal/observability"
	"github.com/openrtc/openrtc/server/internal/protocol"
	"github.com/openrtc/openrtc/server/internal/roomengine"
	"github.com/openrtc/openrtc/server/internal/stats"
)

var (
	heartbeatInterval = 15 * time.Second
	reconcileInterval = 30 * time.Second
	newClusterStore   = func(url string, channelPrefix string) (cluster.Store, error) {
		return cluster.NewRedisStore(url, channelPrefix)
	}
	randomRead            = rand.Read
	clientMessageHandlers = map[protocol.MessageType]func(*Service, *clientConn, protocol.Message) error{
		protocol.TypeJoin:         (*Service).handleJoin,
		protocol.TypeLeave:        (*Service).handleLeave,
		protocol.TypeEmit:         (*Service).handleEmit,
		protocol.TypePresenceSet:  (*Service).handlePresence,
		protocol.TypeStorageGet:   (*Service).handleStorageGet,
		protocol.TypeStorageSet:   (*Service).handleStorageSet,
		protocol.TypeStoragePatch: (*Service).handleStoragePatch,
	}
	sendInitialYJSDocument = sendYJSDocument
)

const (
	defaultJoinLimit            = 100
	eventCatchupMaxEvents       = 1000
	maxStoragePatchOperations   = 100
	storageClusterEvent         = cluster.EventStorageUpdate
	notificationInboxCreated    = roomengine.NotificationInboxCreated
	notificationInboxRead       = roomengine.NotificationInboxRead
	notificationInboxDeleted    = roomengine.NotificationInboxDeleted
	notificationInboxDeletedAll = roomengine.NotificationInboxDeletedAll
	yjsPathPrefix               = "/yjs/"
	yjsFrameUpdate              = byte(cluster.YJSEventUpdate)
	yjsFrameSnapshot            = byte(cluster.YJSEventSnapshot)
	yjsFrameStateVector         = byte(cluster.YJSEventStateVectorRequest)
	yjsFrameStateVectorDiff     = byte(cluster.YJSEventStateVectorDiff)
	yjsFrameSubdocUpdate        = byte(cluster.YJSEventSubdocUpdate)
	yjsFrameSubdocStateVector   = byte(cluster.YJSEventSubdocStateVector)
	yjsFrameSubdocDiff          = byte(cluster.YJSEventSubdocDiff)
	writeWait                   = 5 * time.Second
	readWait                    = 30 * time.Second
)

var joinReplayExcludedEventNames = []string{
	storageClusterEvent,
	notificationInboxCreated,
	notificationInboxRead,
	notificationInboxDeleted,
	notificationInboxDeletedAll,
}

type Service struct {
	cfg      config.RuntimeConfig
	logger   *log.Logger
	verifier *auth.Verifier
	store    cluster.Store
	metrics  *observability.RuntimeMetrics

	ctx    context.Context
	cancel context.CancelFunc

	mu       sync.RWMutex
	conns    map[string]*clientConn
	rooms    *roomengine.Engine
	yjsConns map[string]*yjsConn
	stats    stats.Snapshot
}

type clientConn struct {
	id      string
	ws      *websocket.Conn
	service *Service
	claims  *auth.Claims
	send    chan outboundMessage
	done    chan struct{}

	writeMu sync.Mutex
	closeMu sync.Mutex
	closed  bool

	limiter *emitLimiter
}

type emitLimiter struct {
	limit  int
	window int64
	count  int
	mu     sync.Mutex
}

type yjsConn struct {
	id      string
	ws      *websocket.Conn
	service *Service
	claims  *auth.Claims
	room    string
	send    chan []byte
	done    chan struct{}
	limiter *emitLimiter

	writeMu sync.Mutex
	closeMu sync.Mutex
	closed  bool
}

type outboundMessage struct {
	T       string      `json:"t"`
	ID      string      `json:"id,omitempty"`
	Room    string      `json:"room,omitempty"`
	Event   string      `json:"event,omitempty"`
	Payload interface{} `json:"payload,omitempty"`
	Meta    interface{} `json:"meta,omitempty"`
}

type notificationDeltaPayload struct {
	Type           string                           `json:"type"`
	UserID         string                           `json:"userId"`
	NotificationID string                           `json:"notificationId,omitempty"`
	Notification   *cluster.InboxNotificationRecord `json:"notification,omitempty"`
}

type DevConnectionsSnapshot struct {
	NodeID          string                     `json:"node_id"`
	Connections     []DevConnectionSnapshot    `json:"connections"`
	YJSConnections  []DevYJSConnectionSnapshot `json:"yjs_connections"`
	ActiveSockets   int                        `json:"active_sockets"`
	ActiveRoomCount int                        `json:"active_room_count"`
}

type DevConnectionSnapshot = roomengine.ConnectionSnapshot

type DevYJSConnectionSnapshot = roomengine.YJSConnectionSnapshot

type DevStorageSnapshot struct {
	NodeID      string          `json:"node_id"`
	Room        string          `json:"room"`
	Found       bool            `json:"found"`
	StoreBacked bool            `json:"store_backed"`
	Document    json.RawMessage `json:"document,omitempty"`
}

type DevYJSDocumentSnapshot struct {
	NodeID             string   `json:"node_id"`
	Room               string   `json:"room"`
	Found              bool     `json:"found"`
	StoreBacked        bool     `json:"store_backed"`
	SnapshotFound      bool     `json:"snapshot_found"`
	SnapshotBytes      int      `json:"snapshot_bytes"`
	SnapshotHash       string   `json:"snapshot_hash,omitempty"`
	SnapshotCheckpoint int64    `json:"snapshot_checkpoint"`
	UpdateCount        int      `json:"update_count"`
	UpdateBytes        int      `json:"update_bytes"`
	UpdateSequences    []int64  `json:"update_sequences,omitempty"`
	UpdateKinds        []string `json:"update_kinds,omitempty"`
}

func NewService(cfg config.RuntimeConfig, logger *log.Logger) (*Service, error) {
	ctx, cancel := context.WithCancel(context.Background())
	service := &Service{
		cfg:      cfg,
		logger:   logger,
		verifier: auth.NewVerifier(cfg.Auth.Issuer, cfg.Auth.Audience, cfg.Auth.JWKSURL),
		metrics:  observability.NewRuntimeMetrics(),
		ctx:      ctx,
		cancel:   cancel,
		conns:    make(map[string]*clientConn),
		rooms:    roomengine.New(),
		yjsConns: make(map[string]*yjsConn),
	}

	if cfg.Redis != nil {
		store, err := newClusterStore(cfg.Redis.URL, cfg.Redis.ChannelPrefix)
		if err != nil {
			cancel()
			return nil, err
		}
		service.store = store
		if err := store.Subscribe(ctx, service.handleClusterEvent); err != nil {
			cancel()
			_ = store.Close()
			return nil, err
		}
		if err := store.SubscribePresence(ctx, service.handleClusterPresence); err != nil {
			cancel()
			_ = store.Close()
			return nil, err
		}
		if err := store.SubscribeYJSEvents(ctx, service.handleClusterYJSEvent); err != nil {
			cancel()
			_ = store.Close()
			return nil, err
		}
		go service.reconcileLoop()
	}

	return service, nil
}

func (s *Service) DevStorageSnapshot(room string) DevStorageSnapshot {
	snapshot := DevStorageSnapshot{
		NodeID:      s.cfg.NodeID,
		Room:        room,
		StoreBacked: s.store != nil,
	}
	document, err := s.roomEngine().GetStorage(room)
	if err != nil {
		return snapshot
	}
	snapshot.Found = true
	snapshot.Document = document
	return snapshot
}

func (s *Service) DevYJSDocumentSnapshot(room string) DevYJSDocumentSnapshot {
	document := s.roomEngine().LoadYJSDocument(room)
	snapshot := summarizeYJSDocument(s.cfg.NodeID, room, s.store != nil, document)
	return snapshot
}

func (s *Service) DevConnectionsSnapshot() DevConnectionsSnapshot {
	snapshot := s.roomEngine().ConnectionsSnapshot()

	return DevConnectionsSnapshot{
		NodeID:          s.cfg.NodeID,
		Connections:     snapshot.Connections,
		YJSConnections:  snapshot.YJSConnections,
		ActiveSockets:   len(snapshot.Connections) + len(snapshot.YJSConnections),
		ActiveRoomCount: snapshot.ActiveRoomCount,
	}
}

func summarizeYJSDocument(nodeID string, room string, storeBacked bool, document cluster.YJSDocument) DevYJSDocumentSnapshot {
	snapshot := DevYJSDocumentSnapshot{
		NodeID:             nodeID,
		Room:               room,
		StoreBacked:        storeBacked,
		SnapshotFound:      len(document.Snapshot) > 0,
		SnapshotBytes:      len(document.Snapshot),
		SnapshotHash:       document.SnapshotHash,
		SnapshotCheckpoint: document.SnapshotCheckpoint,
		UpdateCount:        len(document.Updates),
		UpdateSequences:    append([]int64(nil), document.UpdateSequences...),
	}
	if snapshot.SnapshotFound || snapshot.UpdateCount > 0 || snapshot.SnapshotCheckpoint > 0 {
		snapshot.Found = true
	}
	if snapshot.UpdateCount > 0 {
		snapshot.UpdateKinds = make([]string, 0, snapshot.UpdateCount)
		for index, update := range document.Updates {
			snapshot.UpdateBytes += len(update)
			kind := cluster.YJSEventUpdate
			if index < len(document.UpdateKinds) {
				kind = document.UpdateKinds[index]
			}
			snapshot.UpdateKinds = append(snapshot.UpdateKinds, yjsEventKindLabel(kind))
		}
	}
	return snapshot
}

func yjsEventKindLabel(kind cluster.YJSEventKind) string {
	switch kind {
	case cluster.YJSEventUpdate:
		return "update"
	case cluster.YJSEventSnapshot:
		return "snapshot"
	case cluster.YJSEventStateVectorRequest:
		return "state-vector"
	case cluster.YJSEventStateVectorDiff:
		return "state-vector-diff"
	case cluster.YJSEventSubdocUpdate:
		return "subdoc-update"
	case cluster.YJSEventSubdocStateVector:
		return "subdoc-state-vector"
	case cluster.YJSEventSubdocDiff:
		return "subdoc-state-vector-diff"
	default:
		return fmt.Sprintf("unknown-%d", kind)
	}
}

func (s *Service) Close() error {
	s.cancel()
	s.closeActiveSockets()
	if s.store != nil {
		return s.store.Close()
	}
	return nil
}

func (s *Service) closeActiveSockets() {
	s.mu.RLock()
	conns := make([]*clientConn, 0, len(s.conns))
	for _, conn := range s.conns {
		conns = append(conns, conn)
	}
	yjsConns := make([]*yjsConn, 0, len(s.yjsConns))
	for _, conn := range s.yjsConns {
		yjsConns = append(yjsConns, conn)
	}
	s.mu.RUnlock()

	for _, conn := range conns {
		conn.close(websocket.CloseGoingAway, "runtime closing")
	}
	for _, conn := range yjsConns {
		conn.close(websocket.CloseGoingAway, "runtime closing")
	}
}

func (s *Service) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(s.cfg.Server.WSPath, s.handleWS)
	mux.HandleFunc(yjsPathPrefix, s.handleYJS)
	mux.HandleFunc("/healthz", s.handleHealth)
	mux.HandleFunc("/readyz", s.handleReady)
	mux.Handle("/metrics", s.metrics.Handler())
	return mux
}

func (s *Service) handleWS(w http.ResponseWriter, r *http.Request) {
	token := tokenFromRequest(r)
	if token == "" {
		http.Error(w, "missing bearer token", http.StatusUnauthorized)
		return
	}

	claims, err := s.verifier.Verify(r.Context(), token)
	if err != nil {
		http.Error(w, "invalid bearer token", http.StatusUnauthorized)
		return
	}

	upgrader := websocket.Upgrader{
		CheckOrigin: s.checkOrigin,
	}
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	conn := &clientConn{
		id:      newConnID(),
		ws:      ws,
		service: s,
		claims:  claims,
		send:    make(chan outboundMessage, s.cfg.Limits.OutboundQueueDepth),
		done:    make(chan struct{}),
		limiter: &emitLimiter{limit: s.cfg.Limits.EmitsPerSecond},
	}

	s.registerConn(conn)
	defer s.unregisterConn(conn)

	if err := conn.enqueue(outboundMessage{
		T: "HELLO",
		Payload: map[string]any{
			"conn_id": conn.id,
			"server": map[string]any{
				"name":    config.ServerName,
				"node_id": s.cfg.NodeID,
			},
		},
	}); err != nil {
		return
	}

	go conn.writeLoop()
	go s.heartbeatLoop(conn)

	ws.SetReadLimit(int64(s.cfg.Limits.EnvelopeMaxBytes))
	_ = ws.SetReadDeadline(time.Now().Add(readWait))
	ws.SetPongHandler(func(string) error {
		return ws.SetReadDeadline(time.Now().Add(readWait))
	})

	for {
		_, payload, err := ws.ReadMessage()
		if err != nil {
			return
		}
		if err := s.handleClientMessage(conn, payload); err != nil {
			return
		}
	}
}

func (s *Service) handleYJS(w http.ResponseWriter, r *http.Request) {
	room, err := roomFromYJSPath(r.URL.Path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	token := tokenFromRequest(r)
	if token == "" {
		http.Error(w, "missing bearer token", http.StatusUnauthorized)
		return
	}

	claims, err := s.verifier.Verify(r.Context(), token)
	if err != nil {
		http.Error(w, "invalid bearer token", http.StatusUnauthorized)
		return
	}
	if !s.allowsRoomAction(r.Context(), claims, "join", room) {
		http.Error(w, "room join is not permitted", http.StatusForbidden)
		return
	}

	upgrader := websocket.Upgrader{
		CheckOrigin: s.checkOrigin,
	}
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	conn := &yjsConn{
		id:      newConnID(),
		ws:      ws,
		service: s,
		claims:  claims,
		room:    room,
		send:    make(chan []byte, s.cfg.Limits.OutboundQueueDepth),
		done:    make(chan struct{}),
		limiter: &emitLimiter{limit: s.cfg.Limits.EmitsPerSecond},
	}

	s.registerYJSConn(conn)
	defer s.unregisterYJSConn(conn)

	go conn.writeLoop()
	go s.yjsHeartbeatLoop(conn)

	document, err := s.loadYJSDocument(room)
	if err != nil {
		conn.close(openrtcerr.DescriptorFor(openrtcerr.CodeInternal).WSCloseCode, openrtcerr.WSCloseReason(openrtcerr.CodeInternal))
		return
	}
	if err := sendInitialYJSDocument(conn, document); err != nil {
		return
	}

	ws.SetReadLimit(int64(s.cfg.Limits.YJSMaxBytes + 1))
	_ = ws.SetReadDeadline(time.Now().Add(readWait))
	ws.SetPongHandler(func(string) error {
		return ws.SetReadDeadline(time.Now().Add(readWait))
	})

	for {
		messageType, payload, err := ws.ReadMessage()
		if err != nil {
			return
		}
		if messageType != websocket.BinaryMessage || len(payload) < 2 {
			conn.close(openrtcerr.DescriptorFor(openrtcerr.CodeBadRequest).WSCloseCode, openrtcerr.WSCloseReason(openrtcerr.CodeBadRequest))
			return
		}
		if !conn.limiter.Allow() {
			conn.close(openrtcerr.DescriptorFor(openrtcerr.CodeRateLimited).WSCloseCode, openrtcerr.WSCloseReason(openrtcerr.CodeRateLimited))
			return
		}

		kind := cluster.YJSEventKind(payload[0])
		plan, ok := roomengine.NewYJSEventPlan(room, kind, payload[1:], roomengine.YJSEventOptions{
			OriginNode:   s.cfg.NodeID,
			OriginConnID: conn.id,
		})
		if !ok {
			conn.close(openrtcerr.DescriptorFor(openrtcerr.CodeBadRequest).WSCloseCode, openrtcerr.WSCloseReason(openrtcerr.CodeBadRequest))
			return
		}
		if plan.RequiresPublish {
			if !s.allowsRoomAction(r.Context(), claims, "publish", room) {
				conn.close(openrtcerr.DescriptorFor(openrtcerr.CodeRoomForbidden).WSCloseCode, openrtcerr.WSCloseReason(openrtcerr.CodeRoomForbidden))
				return
			}
		}
		if plan.Durable {
			storedEvent, err := s.storeYJSEvent(plan.Event)
			if err != nil {
				conn.close(openrtcerr.DescriptorFor(openrtcerr.CodeInternal).WSCloseCode, openrtcerr.WSCloseReason(openrtcerr.CodeInternal))
				return
			}
			plan = plan.WithEvent(storedEvent)
		}
		effects := s.roomEngine().YJSMutationEffects(plan)
		if err := s.broadcastYJSFanout(effects.Fanout); err != nil {
			return
		}
		if s.store != nil && effects.PublishCluster {
			if err := s.store.PublishYJSEvent(s.ctx, effects.Event); err != nil {
				conn.close(openrtcerr.DescriptorFor(openrtcerr.CodeInternal).WSCloseCode, openrtcerr.WSCloseReason(openrtcerr.CodeInternal))
				return
			}
		}
	}
}

func (s *Service) handleClientMessage(conn *clientConn, payload []byte) error {
	message, err := protocol.ParseClientMessage(payload, protocol.ParseOptions{
		MaxEnvelopeBytes: s.cfg.Limits.EnvelopeMaxBytes,
		MaxPayloadBytes:  s.cfg.Limits.PayloadMaxBytes,
		TenantPrefix:     s.tenantPrefix(conn.claims),
	})
	if err != nil {
		parseErr := err.(*protocol.ParseError)
		return conn.enqueue(outboundMessage{
			T:  "ERROR",
			ID: message.ID,
			Payload: openrtcerr.APIError{
				Code:    parseErr.Code,
				Message: parseErr.Message,
			},
		})
	}

	return clientMessageHandlers[message.Type](s, conn, message)
}

func sendYJSDocument(conn *yjsConn, document cluster.YJSDocument) error {
	for _, frame := range roomengine.NewYJSDocumentFrames(document) {
		if err := conn.enqueueFrame(byte(frame.Kind), frame.Update); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) handleJoin(conn *clientConn, message protocol.Message) error {
	if !s.allowsRoomAction(s.ctx, conn.claims, "join", message.Room) {
		return conn.enqueue(outboundMessage{
			T:  "ERROR",
			ID: message.ID,
			Payload: openrtcerr.APIError{
				Code:      openrtcerr.CodeRoomForbidden,
				Message:   "room join is not permitted",
				RequestID: message.ID,
			},
		})
	}

	joinResult, err := s.roomEngine().Join(conn.id, message.Room, s.cfg.Limits.RoomsPerConnection)
	if errors.Is(err, roomengine.ErrRoomLimitExceeded) {
		return conn.enqueue(outboundMessage{
			T:  "ERROR",
			ID: message.ID,
			Payload: openrtcerr.APIError{
				Code:      openrtcerr.CodeBadRequest,
				Message:   "maximum rooms per connection exceeded",
				RequestID: message.ID,
			},
		})
	}
	if err != nil {
		return err
	}

	joinPlan := roomengine.NewJoinPlan(joinResult, joinPlanOptions(conn.id, message.JoinMeta))

	if !joinPlan.AlreadyJoined() {
		s.mu.Lock()
		s.stats.JoinsTotal++
		s.syncStatsLocked()
		s.mu.Unlock()

		s.metrics.JoinsTotal.Inc()
	}
	if err := s.applyRoomMembershipMutation(joinPlan.MembershipMutation()); err != nil {
		return err
	}

	roomSnapshot, err := s.snapshotJoinedRoom(message.Room, joinPlan)
	if err != nil {
		return err
	}
	replayEvents, err := s.replayablePublishedEvents(message.Room, joinPlan)
	if err != nil {
		return err
	}

	if err := conn.enqueue(outboundMessage{
		T:       "JOINED",
		ID:      message.ID,
		Room:    message.Room,
		Payload: roomSnapshot,
	}); err != nil {
		return err
	}
	return s.sendReplayEvents(conn, replayEvents)
}

func (s *Service) handleLeave(conn *clientConn, message protocol.Message) error {
	plan := s.roomEngine().NewLeavePlan(conn.id, message.Room, roomengine.PresenceEventOptions{OriginNode: s.cfg.NodeID})
	if err := s.applyRoomMembershipMutation(plan.MembershipMutation); err != nil {
		return err
	}
	if plan.Left {
		fanout := plan.PresenceFanout
		if fanout != nil {
			if err := s.publishPresenceFanout(*fanout); err != nil {
				return err
			}
			s.roomEngine().ApplyLeavePlan(plan)
			if err := s.broadcastPresenceFanout(*fanout); err != nil {
				return err
			}
		} else {
			s.roomEngine().ApplyLeavePlan(plan)
		}
		s.recordLeave()
	}

	return conn.enqueue(outboundMessage{T: "LEFT", ID: message.ID, Room: message.Room})
}

func (s *Service) applyRoomMembershipMutation(mutation *roomengine.MembershipMutation) error {
	if mutation == nil || s.store == nil {
		return nil
	}
	switch mutation.Kind {
	case roomengine.MembershipMutationJoin:
		return s.store.JoinRoom(s.ctx, mutation.ConnID, mutation.Room)
	case roomengine.MembershipMutationLeave:
		return s.store.LeaveRoom(s.ctx, mutation.ConnID, mutation.Room)
	default:
		return errors.New("invalid room membership mutation kind")
	}
}

func (s *Service) handleEmit(conn *clientConn, message protocol.Message) error {
	if !s.allowsRoomAction(s.ctx, conn.claims, "publish", message.Room) {
		return conn.enqueue(outboundMessage{
			T:  "ERROR",
			ID: message.ID,
			Payload: openrtcerr.APIError{
				Code:      openrtcerr.CodeRoomForbidden,
				Message:   "room publish is not permitted",
				RequestID: message.ID,
			},
		})
	}
	if !conn.limiter.Allow() {
		return conn.enqueue(outboundMessage{
			T:  "ERROR",
			ID: message.ID,
			Payload: openrtcerr.APIError{
				Code:      openrtcerr.CodeRateLimited,
				Message:   "emit rate limit exceeded",
				RequestID: message.ID,
			},
		})
	}

	traceID := ""
	if message.EmitMeta != nil {
		traceID = message.EmitMeta.TraceID
	}

	event := roomengine.NewEvent(message.Room, message.Event, message.Payload, roomengine.EventOptions{
		TraceID:    traceID,
		OriginNode: s.cfg.NodeID,
	})
	if s.store != nil {
		published, err := s.store.PublishEvent(s.ctx, event)
		if err != nil {
			return err
		}
		event = published
	}
	if err := s.broadcastEvent(event, true); err != nil {
		return err
	}
	return nil
}

func (s *Service) handlePresence(conn *clientConn, message protocol.Message) error {
	if !s.allowsRoomAction(s.ctx, conn.claims, "presence", message.Room) {
		return conn.enqueue(outboundMessage{
			T:  "ERROR",
			ID: message.ID,
			Payload: openrtcerr.APIError{
				Code:      openrtcerr.CodeRoomForbidden,
				Message:   "room presence is not permitted",
				RequestID: message.ID,
			},
		})
	}

	plan := s.roomEngine().NewPresenceSetPlan(conn.id, message.Room, message.Payload, roomengine.PresenceEventOptions{OriginNode: s.cfg.NodeID})
	if s.store != nil {
		if err := s.store.SetPresence(s.ctx, conn.id, message.Room, message.Payload); err != nil {
			return err
		}
	}

	if err := s.publishPresenceFanout(plan.Fanout); err != nil {
		return err
	}
	fanout := s.roomEngine().ApplyPresenceSetPlan(plan)
	if err := s.broadcastPresenceFanout(fanout); err != nil {
		return err
	}
	s.recordPresenceUpdate()
	return nil
}

func (s *Service) handleStorageGet(conn *clientConn, message protocol.Message) error {
	if !s.allowsRoomAction(s.ctx, conn.claims, "storage:read", message.Room) {
		return conn.enqueue(runtimeErrorMessage(message.ID, openrtcerr.CodeRoomForbidden, "room storage is not permitted"))
	}

	document, err := s.getStorage(message.Room)
	if err != nil {
		return conn.enqueue(storageErrorMessage(message.ID, err))
	}
	return conn.enqueue(outboundMessage{
		T:    "STORAGE_SNAPSHOT",
		ID:   message.ID,
		Room: message.Room,
		Payload: map[string]any{
			"document": document,
		},
	})
}

func (s *Service) handleStorageSet(conn *clientConn, message protocol.Message) error {
	if !s.allowsRoomAction(s.ctx, conn.claims, "storage:write", message.Room) {
		return conn.enqueue(runtimeErrorMessage(message.ID, openrtcerr.CodeRoomForbidden, "room storage is not permitted"))
	}

	plan, err := s.setStorageMutationPlan(message.Room, message.Payload, roomengine.StorageMutationOptions{
		MaxBytes:            s.cfg.Limits.PayloadMaxBytes,
		OpID:                storageOpID(message),
		OriginConnID:        conn.id,
		ExpectedSequence:    storageExpectedSequence(message),
		ExpectedSequenceSet: storageExpectedSequenceSet(message),
	}, roomengine.StorageEventOptions{
		OriginNode:          s.cfg.NodeID,
		ExcludeSenderConnID: conn.id,
	})
	if err != nil {
		return conn.enqueue(storageErrorMessage(message.ID, err))
	}
	plan, err = s.publishStoragePlan(plan)
	if err != nil {
		return conn.enqueue(storageErrorMessage(message.ID, err))
	}
	if err := s.broadcastStorageFanout(plan.Fanout); err != nil {
		return err
	}
	return conn.enqueue(storageAckMessage(message, plan.Mutation.Kind, plan.Mutation.Document, plan.Fanout.Sequence))
}

func (s *Service) handleStoragePatch(conn *clientConn, message protocol.Message) error {
	if !s.allowsRoomAction(s.ctx, conn.claims, "storage:write", message.Room) {
		return conn.enqueue(runtimeErrorMessage(message.ID, openrtcerr.CodeRoomForbidden, "room storage is not permitted"))
	}

	operations, parseErr := decodeStoragePatchPayload(message.Payload)
	if parseErr != nil {
		return conn.enqueue(runtimeErrorMessage(message.ID, parseErr.Code, parseErr.Message))
	}
	plan, err := s.applyStoragePatchMutationPlan(message.Room, operations, roomengine.StorageMutationOptions{
		MaxBytes:            s.cfg.Limits.PayloadMaxBytes,
		OpID:                storageOpID(message),
		OriginConnID:        conn.id,
		ExpectedSequence:    storageExpectedSequence(message),
		ExpectedSequenceSet: storageExpectedSequenceSet(message),
	}, roomengine.StorageEventOptions{
		OriginNode:          s.cfg.NodeID,
		ExcludeSenderConnID: conn.id,
	})
	if err != nil {
		return conn.enqueue(storageErrorMessage(message.ID, err))
	}
	plan, err = s.publishStoragePlan(plan)
	if err != nil {
		return conn.enqueue(storageErrorMessage(message.ID, err))
	}
	if err := s.broadcastStorageFanout(plan.Fanout); err != nil {
		return err
	}
	return conn.enqueue(storageAckMessage(message, plan.Mutation.Kind, plan.Mutation.Document, plan.Fanout.Sequence))
}

func (s *Service) replayablePublishedEvents(room string, plan roomengine.JoinPlan) ([]cluster.PublishedEvent, error) {
	afterSequence, maxEvents, ok := plan.ReplayLogRequest()
	if s.store == nil || !ok {
		return nil, nil
	}
	list, err := s.store.ListPublishedEvents(s.ctx, room, afterSequence, maxEvents)
	if err != nil {
		return nil, err
	}
	return plan.ReplayEvents(list.Events), nil
}

func (s *Service) sendReplayEvents(conn *clientConn, events []cluster.PublishedEvent) error {
	for _, event := range events {
		if err := conn.enqueue(eventOutboundMessage(event)); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) handleClusterEvent(event cluster.PublishedEvent) {
	plan := roomengine.NewClusterEventPlan(event, s.cfg.NodeID)
	switch plan.Kind {
	case roomengine.ClusterEventSkip:
		return
	case roomengine.ClusterEventNotification:
		_ = s.broadcastNotificationDelta(plan.Event)
		return
	case roomengine.ClusterEventStorage:
		storagePlan, err := s.roomEngine().RecordStorageEvent(plan.Event, s.cfg.Limits.PayloadMaxBytes)
		if err != nil {
			return
		}
		_ = s.broadcastStorageFanout(storagePlan.Fanout)
		return
	default:
		_ = s.broadcastEvent(plan.Event, false)
	}
}

func (s *Service) handleClusterPresence(event cluster.PresenceEvent) {
	plan := roomengine.NewClusterPresencePlan(event, s.cfg.NodeID)
	if !plan.Deliver {
		return
	}
	_ = s.broadcastPresenceFanout(s.roomEngine().PresenceFanout(plan.Event))
}

func (s *Service) broadcastEvent(event cluster.PublishedEvent, countMetric bool) error {
	return s.broadcastEventFanout(s.roomEngine().EventFanout(event), countMetric)
}

func (s *Service) broadcastEventFanout(fanout roomengine.EventFanout, countMetric bool) error {
	event := fanout.Event
	targetIDs := fanout.TargetConnIDs
	s.mu.RLock()
	targets := make([]*clientConn, 0, len(targetIDs))
	for _, connID := range targetIDs {
		if member := s.conns[connID]; member != nil {
			targets = append(targets, member)
		}
	}
	s.mu.RUnlock()

	for _, target := range targets {
		if err := target.enqueue(eventOutboundMessage(event)); err != nil {
			return err
		}
	}

	if countMetric {
		s.mu.Lock()
		s.stats.EventsTotal++
		s.syncStatsLocked()
		s.mu.Unlock()
		s.metrics.EventsTotal.Inc()
	}

	return nil
}

func (s *Service) broadcastNotificationDelta(event cluster.PublishedEvent) error {
	var payload notificationDeltaPayload
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return nil
	}
	if payload.UserID == "" {
		return nil
	}
	if payload.NotificationID == "" && payload.Notification != nil {
		payload.NotificationID = payload.Notification.ID
	}

	return s.broadcastNotificationFanout(s.roomEngine().NotificationFanout(event, payload.UserID), payload)
}

func (s *Service) broadcastNotificationFanout(fanout roomengine.NotificationFanout, payload notificationDeltaPayload) error {
	s.mu.RLock()
	targets := make([]*clientConn, 0, len(fanout.TargetConnIDs))
	for _, connID := range fanout.TargetConnIDs {
		if conn := s.conns[connID]; conn != nil {
			targets = append(targets, conn)
		}
	}
	s.mu.RUnlock()

	for _, target := range targets {
		if err := target.enqueue(outboundMessage{
			T:       "NOTIFICATION",
			Event:   fanout.Event.Event,
			Payload: payload,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) broadcastPresenceEvent(event cluster.PresenceEvent) error {
	return s.broadcastPresenceFanout(s.roomEngine().PresenceFanout(event))
}

func (s *Service) broadcastPresenceFanout(fanout roomengine.PresenceFanout) error {
	event := fanout.Event
	targetIDs := fanout.TargetConnIDs
	s.mu.RLock()
	targets := make([]*clientConn, 0, len(targetIDs))
	for _, connID := range targetIDs {
		if member := s.conns[connID]; member != nil {
			targets = append(targets, member)
		}
	}
	s.mu.RUnlock()

	payload := map[string]any{
		"conn_id": event.ConnID,
	}
	if event.Offline {
		payload["offline"] = true
	} else {
		payload["state"] = event.State
	}

	for _, target := range targets {
		if err := target.enqueue(outboundMessage{
			T:       "PRESENCE",
			Room:    event.Room,
			Payload: payload,
		}); err != nil {
			return err
		}
	}

	return nil
}

func (s *Service) publishPresenceFanout(fanout roomengine.PresenceFanout) error {
	if s.store == nil {
		return nil
	}
	return s.store.PublishPresence(s.ctx, fanout.Event)
}

func eventOutboundMessage(event cluster.PublishedEvent) outboundMessage {
	meta := map[string]any{}
	if event.TraceID != "" {
		meta["trace_id"] = event.TraceID
	}
	if event.Sequence > 0 {
		meta["seq"] = event.Sequence
	}
	message := outboundMessage{
		T:       "EVENT",
		Room:    event.Room,
		Event:   event.Event,
		Payload: event.Payload,
	}
	if len(meta) > 0 {
		message.Meta = meta
	}
	return message
}

func (s *Service) broadcastStorageFanout(fanout roomengine.StorageFanout) error {
	targetIDs := fanout.TargetConnIDs
	s.mu.RLock()
	targets := make([]*clientConn, 0, len(targetIDs))
	for _, connID := range targetIDs {
		if member := s.conns[connID]; member != nil {
			targets = append(targets, member)
		}
	}
	s.mu.RUnlock()

	for _, target := range targets {
		if err := target.enqueue(storageUpdateOutboundMessage(fanout)); err != nil {
			return err
		}
	}
	return nil
}

func storageUpdateOutboundMessage(fanout roomengine.StorageFanout) outboundMessage {
	message := outboundMessage{
		T:       "STORAGE_UPDATE",
		Room:    fanout.Room,
		Payload: fanout.Update,
	}
	if fanout.Sequence > 0 {
		message.Meta = map[string]any{"seq": fanout.Sequence}
	}
	return message
}

func (s *Service) handleClusterYJSEvent(event cluster.YJSEvent) {
	plan := roomengine.NewClusterYJSEventPlan(event, s.cfg.NodeID)
	if !plan.Deliver {
		return
	}
	_ = s.broadcastYJSEvent(plan.Event)
}

func (s *Service) loadYJSDocument(room string) (cluster.YJSDocument, error) {
	if s.store != nil {
		return s.store.LoadYJSDocument(s.ctx, room)
	}

	return s.roomEngine().LoadYJSDocument(room), nil
}

func (s *Service) storeYJSEvent(event cluster.YJSEvent) (cluster.YJSEvent, error) {
	plan, err := roomengine.NewYJSPersistencePlan(event)
	if err != nil {
		return cluster.YJSEvent{}, err
	}
	if s.store != nil {
		if plan.Mode == roomengine.YJSPersistenceStoreSnapshot {
			return plan.Event, s.store.StoreYJSSnapshot(s.ctx, plan.Event.Room, plan.Event.Update)
		}
		sequence, err := s.store.AppendYJSUpdate(s.ctx, plan.Event.Room, plan.Event.Kind, plan.Event.Update)
		event = plan.WithSequence(sequence)
		return event, err
	}

	return s.roomEngine().StoreYJSPersistence(plan), nil
}

func (s *Service) broadcastYJSEvent(event cluster.YJSEvent) error {
	return s.broadcastYJSFanout(s.roomEngine().YJSFanout(event))
}

func (s *Service) broadcastYJSFanout(fanout roomengine.YJSFanout) error {
	event := fanout.Event
	targetIDs := fanout.TargetConnIDs
	s.mu.RLock()
	targets := make([]*yjsConn, 0, len(targetIDs))
	for _, connID := range targetIDs {
		if member := s.yjsConns[connID]; member != nil {
			targets = append(targets, member)
		}
	}
	s.mu.RUnlock()

	for _, target := range targets {
		if err := target.enqueueFrame(byte(event.Kind), event.Update); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) snapshotRoom(room string, joinMeta *protocol.JoinMeta) (roomengine.SnapshotPage, error) {
	if s.store != nil {
		return s.snapshotStoreRoom(room, joinMeta)
	}

	return roomengine.PageSnapshot(s.roomEngine().Snapshot(room), snapshotPageOptions(joinMeta)), nil
}

func (s *Service) snapshotJoinedRoom(room string, plan roomengine.JoinPlan) (roomengine.SnapshotPage, error) {
	if s.store != nil {
		return s.snapshotStoreRoomWithPlan(room, plan)
	}

	return plan.LocalSnapshotPage(), nil
}

func (s *Service) snapshotStoreRoom(room string, joinMeta *protocol.JoinMeta) (roomengine.SnapshotPage, error) {
	return s.snapshotStoreRoomWithPlan(room, roomengine.NewJoinPlan(roomengine.JoinResult{}, roomengine.JoinPlanOptions{
		SnapshotPage: snapshotPageOptions(joinMeta),
	}))
}

func (s *Service) snapshotStoreRoomWithPlan(room string, plan roomengine.JoinPlan) (roomengine.SnapshotPage, error) {
	snapshot, err := s.store.SnapshotRoom(s.ctx, room)
	if err != nil {
		return roomengine.SnapshotPage{}, err
	}
	return plan.SnapshotPage(roomengine.Snapshot{
		Members:  snapshot.Members,
		Presence: snapshot.Presence,
	}), nil
}

func snapshotPageOptions(joinMeta *protocol.JoinMeta) roomengine.SnapshotPageOptions {
	limit := defaultJoinLimit
	cursor := ""
	if joinMeta != nil {
		if joinMeta.Limit > 0 {
			limit = joinMeta.Limit
		}
		cursor = joinMeta.Cursor
	}

	return roomengine.SnapshotPageOptions{
		Limit:  limit,
		Cursor: cursor,
	}
}

func joinPlanOptions(connID string, joinMeta *protocol.JoinMeta) roomengine.JoinPlanOptions {
	var afterSequence uint64
	if joinMeta != nil {
		afterSequence = joinMeta.AfterSequence
	}
	return roomengine.JoinPlanOptions{
		SnapshotPage: snapshotPageOptions(joinMeta),
		Replay: roomengine.JoinReplayOptions{
			AfterSequence:      afterSequence,
			MaxEvents:          eventCatchupMaxEvents,
			ExcludeConnID:      connID,
			ExcludedEventNames: joinReplayExcludedEventNames,
		},
	}
}

func (s *Service) getStorage(room string) (json.RawMessage, error) {
	if s.store != nil {
		return s.store.GetStorage(s.ctx, room)
	}
	return s.roomEngine().GetStorage(room)
}

func (s *Service) setStorageMutationPlan(room string, document json.RawMessage, mutationOptions roomengine.StorageMutationOptions, eventOptions roomengine.StorageEventOptions) (roomengine.StorageMutationPlan, error) {
	if s.store != nil {
		if sequenced, ok := s.store.(cluster.SequencedStorageWriter); ok {
			stored, sequence, err := sequenced.SetStorageWithOptions(s.ctx, room, document, storageWriteOptions(mutationOptions))
			if err != nil {
				return roomengine.StorageMutationPlan{}, err
			}
			mutationOptions.Sequence = sequence
			return s.roomEngine().RecordStorageMutationPlan(room, roomengine.StorageMutationSet, stored, nil, mutationOptions, eventOptions)
		}
		if mutationOptions.ExpectedSequenceSet {
			return roomengine.StorageMutationPlan{}, cluster.ErrStorageConflict
		}
		stored, err := s.store.SetStorage(s.ctx, room, document)
		if err != nil {
			return roomengine.StorageMutationPlan{}, err
		}
		return s.roomEngine().RecordStorageMutationPlan(room, roomengine.StorageMutationSet, stored, nil, mutationOptions, eventOptions)
	}
	return s.roomEngine().SetStorageMutationPlan(room, document, mutationOptions, eventOptions)
}

func (s *Service) applyStoragePatchMutationPlan(room string, operations []cluster.JSONPatchOperation, mutationOptions roomengine.StorageMutationOptions, eventOptions roomengine.StorageEventOptions) (roomengine.StorageMutationPlan, error) {
	if s.store != nil {
		if sequenced, ok := s.store.(cluster.SequencedStorageWriter); ok {
			patched, sequence, err := sequenced.ApplyStoragePatchWithOptions(s.ctx, room, operations, storageWriteOptions(mutationOptions))
			if err != nil {
				return roomengine.StorageMutationPlan{}, err
			}
			mutationOptions.Sequence = sequence
			return s.roomEngine().RecordStorageMutationPlan(room, roomengine.StorageMutationPatch, patched, operations, mutationOptions, eventOptions)
		}
		if mutationOptions.ExpectedSequenceSet {
			return roomengine.StorageMutationPlan{}, cluster.ErrStorageConflict
		}
		patched, err := s.store.ApplyStoragePatch(s.ctx, room, operations, mutationOptions.MaxBytes)
		if err != nil {
			return roomengine.StorageMutationPlan{}, err
		}
		return s.roomEngine().RecordStorageMutationPlan(room, roomengine.StorageMutationPatch, patched, operations, mutationOptions, eventOptions)
	}
	return s.roomEngine().ApplyStoragePatchMutationPlan(room, operations, mutationOptions, eventOptions)
}

func (s *Service) publishStoragePlan(plan roomengine.StorageMutationPlan) (roomengine.StorageMutationPlan, error) {
	if s.store == nil {
		return plan, nil
	}
	event, err := s.store.PublishEvent(s.ctx, plan.Event)
	if err != nil {
		return roomengine.StorageMutationPlan{}, err
	}
	return plan.WithEvent(event), nil
}

func (s *Service) registerConn(conn *clientConn) {
	touch := s.roomEngine().RegisterSession(sessionInfoFromConn(conn))
	s.mu.Lock()
	s.conns[conn.id] = conn
	s.syncStatsLocked()
	s.mu.Unlock()
	s.metrics.ActiveConnections.Inc()

	s.applyConnectionTouch(touch)
}

func (s *Service) registerYJSConn(conn *yjsConn) {
	s.mu.Lock()
	s.yjsConns[conn.id] = conn
	s.mu.Unlock()
	s.roomEngine().RegisterYJSSession(yjsSessionInfoFromConn(conn))
}

func (s *Service) unregisterYJSConn(conn *yjsConn) {
	s.mu.Lock()
	delete(s.yjsConns, conn.id)
	s.mu.Unlock()
	s.roomEngine().UnregisterYJSConn(conn.id, conn.room)
	conn.close(websocket.CloseNormalClosure, "closing")
}

func (s *Service) unregisterConn(conn *clientConn) {
	disconnectPlan := s.roomEngine().NewDisconnectPlan(conn.id, roomengine.PresenceEventOptions{OriginNode: s.cfg.NodeID})
	disconnectResult := s.roomEngine().ApplyDisconnectPlan(disconnectPlan)
	s.mu.Lock()
	delete(s.conns, conn.id)
	s.syncStatsLocked()
	s.mu.Unlock()
	s.metrics.ActiveConnections.Dec()
	conn.close(websocket.CloseNormalClosure, "closing")

	s.applyConnectionCleanup(disconnectResult.Cleanup)
	for _, fanout := range disconnectResult.PresenceFanouts {
		if err := s.publishPresenceFanout(fanout); err != nil {
			continue
		}
		_ = s.broadcastPresenceFanout(fanout)
	}
}

func (s *Service) applyConnectionCleanup(cleanup *roomengine.ConnectionCleanup) {
	if s.store == nil || cleanup == nil {
		return
	}
	_ = s.store.CleanupConnection(s.ctx, s.cfg.NodeID, cleanup.ConnID)
}

func (s *Service) applyConnectionTouch(touch *roomengine.ConnectionTouch) {
	if s.store == nil || touch == nil {
		return
	}
	_ = s.store.TouchConnection(s.ctx, touch.ConnID, cluster.ConnectionMeta{
		NodeID:      s.cfg.NodeID,
		Subject:     touch.Subject,
		Tenant:      touch.Tenant,
		ConnectedAt: time.Now(),
	})
}

func sessionInfoFromConn(conn *clientConn) roomengine.SessionInfo {
	info := roomengine.SessionInfo{
		ConnID: conn.id,
	}
	if conn.claims != nil {
		info.Subject = conn.claims.Subject
		info.Tenant = conn.claims.Tenant
	}
	return info
}

func yjsSessionInfoFromConn(conn *yjsConn) roomengine.YJSSessionInfo {
	info := roomengine.YJSSessionInfo{
		ConnID: conn.id,
		Room:   conn.room,
	}
	if conn.claims != nil {
		info.Subject = conn.claims.Subject
		info.Tenant = conn.claims.Tenant
	}
	return info
}

func (s *Service) yjsHeartbeatLoop(conn *yjsConn) {
	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-conn.done:
			return
		case <-ticker.C:
			conn.writeMu.Lock()
			_ = conn.ws.WriteControl(websocket.PingMessage, []byte("ping"), time.Now().Add(writeWait))
			conn.writeMu.Unlock()
		}
	}
}

func (s *Service) heartbeatLoop(conn *clientConn) {
	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-conn.done:
			return
		case <-ticker.C:
			conn.writeMu.Lock()
			_ = conn.ws.WriteControl(websocket.PingMessage, []byte("ping"), time.Now().Add(writeWait))
			conn.writeMu.Unlock()

			s.applyConnectionTouch(s.roomEngine().TouchSession(conn.id))
		}
	}
}

func (s *Service) reconcileLoop() {
	ticker := time.NewTicker(reconcileInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			if s.store != nil {
				_ = s.store.ReconcileNode(s.ctx, s.cfg.NodeID)
			}
		}
	}
}

func (s *Service) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func (s *Service) handleReady(w http.ResponseWriter, _ *http.Request) {
	if s.store != nil {
		if err := s.store.Healthy(s.ctx); err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ready"))
}

func (s *Service) recordLeave() {
	s.mu.Lock()
	s.stats.LeavesTotal++
	s.syncStatsLocked()
	s.mu.Unlock()
	s.metrics.LeavesTotal.Inc()
}

func (s *Service) recordPresenceUpdate() {
	s.mu.Lock()
	s.stats.PresenceUpdatesTotal++
	s.syncStatsLocked()
	s.mu.Unlock()
	s.metrics.PresenceUpdatesTotal.Inc()
}

func (s *Service) syncStatsLocked() {
	snapshot := stats.Snapshot{
		ActiveConnections:    int64(len(s.conns)),
		ActiveRooms:          int64(s.roomEngine().ActiveRoomCount()),
		JoinsTotal:           s.stats.JoinsTotal,
		LeavesTotal:          s.stats.LeavesTotal,
		EventsTotal:          s.stats.EventsTotal,
		PresenceUpdatesTotal: s.stats.PresenceUpdatesTotal,
		QueueOverflowsTotal:  s.stats.QueueOverflowsTotal,
	}
	s.metrics.ActiveConnections.Set(float64(snapshot.ActiveConnections))
	s.metrics.ActiveRooms.Set(float64(snapshot.ActiveRooms))
	if s.store != nil {
		_ = s.store.SyncStats(s.ctx, s.cfg.NodeID, snapshot)
	}
}

func (s *Service) roomEngine() *roomengine.Engine {
	if s.rooms == nil {
		s.rooms = roomengine.New()
	}
	return s.rooms
}

func (s *Service) tenantPrefix(claims *auth.Claims) string {
	if !s.cfg.Tenant.EnforcePrefix || claims.Tenant == "" {
		return ""
	}
	return claims.Tenant + s.cfg.Tenant.Separator
}

func (s *Service) allowsRoomAction(ctx context.Context, claims *auth.Claims, action string, room string) bool {
	if claims.Allows(action, room, s.cfg.Tenant.EnforcePrefix, s.cfg.Tenant.Separator) {
		return true
	}
	if s.cfg.Tenant.EnforcePrefix {
		if claims.Tenant == "" || !strings.HasPrefix(room, claims.Tenant+s.cfg.Tenant.Separator) {
			return false
		}
	}
	if s.store == nil {
		return false
	}
	record, err := s.store.GetRoom(ctx, room)
	if err != nil {
		return false
	}
	return record.Allows(claims.Subject, claims.RoomGroupIDs(), action)
}

func decodeStoragePatchPayload(payload json.RawMessage) ([]cluster.JSONPatchOperation, *protocol.ParseError) {
	decoder := json.NewDecoder(bytes.NewReader(payload))
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

func storageAckMessage(message protocol.Message, kind string, document json.RawMessage, sequence uint64) outboundMessage {
	outbound := outboundMessage{
		T:    "STORAGE_ACK",
		ID:   message.ID,
		Room: message.Room,
		Payload: map[string]any{
			"kind":     kind,
			"op_id":    storageOpID(message),
			"document": document,
		},
	}
	if sequence > 0 {
		outbound.Meta = map[string]any{"seq": sequence}
	}
	return outbound
}

func storageOpID(message protocol.Message) string {
	if message.StorageMeta == nil {
		return ""
	}
	return message.StorageMeta.OpID
}

func storageExpectedSequence(message protocol.Message) uint64 {
	if message.StorageMeta == nil {
		return 0
	}
	return message.StorageMeta.ExpectedSequence
}

func storageExpectedSequenceSet(message protocol.Message) bool {
	return message.StorageMeta != nil && message.StorageMeta.ExpectedSequenceSet
}

func storageWriteOptions(options roomengine.StorageMutationOptions) cluster.StorageWriteOptions {
	return cluster.StorageWriteOptions{
		MaxBytes:            options.MaxBytes,
		ExpectedSequence:    options.ExpectedSequence,
		ExpectedSequenceSet: options.ExpectedSequenceSet,
	}
}

func runtimeErrorMessage(requestID string, code openrtcerr.Code, message string) outboundMessage {
	return outboundMessage{
		T:  "ERROR",
		ID: requestID,
		Payload: openrtcerr.APIError{
			Code:      code,
			Message:   message,
			RequestID: requestID,
		},
	}
}

func storageErrorMessage(requestID string, err error) outboundMessage {
	switch {
	case errors.Is(err, cluster.ErrStorageNotFound):
		return runtimeErrorMessage(requestID, openrtcerr.CodeStorageNotFound, "storage document not found")
	case errors.Is(err, cluster.ErrStorageConflict):
		return runtimeErrorMessage(requestID, openrtcerr.CodeStorageConflict, err.Error())
	case errors.Is(err, cluster.ErrStoragePatch):
		return runtimeErrorMessage(requestID, openrtcerr.CodePatchFailed, err.Error())
	default:
		return runtimeErrorMessage(requestID, openrtcerr.CodeInternal, err.Error())
	}
}

func tokenFromRequest(r *http.Request) string {
	if authHeader := r.Header.Get("Authorization"); authHeader != "" {
		const prefix = "Bearer "
		if len(authHeader) > len(prefix) && authHeader[:len(prefix)] == prefix {
			return authHeader[len(prefix):]
		}
	}
	return r.URL.Query().Get("token")
}

func (s *Service) checkOrigin(r *http.Request) bool {
	if len(s.cfg.Server.AllowedOrigins) == 0 {
		return true
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return false
	}
	for _, allowed := range s.cfg.Server.AllowedOrigins {
		if origin == allowed {
			return true
		}
	}
	return false
}

func newConnID() string {
	raw := make([]byte, 12)
	if _, err := randomRead(raw); err != nil {
		panic(err)
	}
	return hex.EncodeToString(raw)
}

func roomFromYJSPath(pathValue string) (string, error) {
	if !strings.HasPrefix(pathValue, yjsPathPrefix) {
		return "", errors.New("invalid yjs path")
	}
	escaped := strings.TrimPrefix(pathValue, yjsPathPrefix)
	if escaped == "" {
		return "", errors.New("room is required")
	}
	room, err := url.PathUnescape(escaped)
	if err != nil {
		return "", errors.New("room must be URL-escaped")
	}
	if err := protocol.ValidateRoomName(room); err != nil {
		return "", err
	}
	return room, nil
}

func (c *clientConn) writeLoop() {
	for {
		select {
		case <-c.done:
			return
		case message := <-c.send:
			c.writeMu.Lock()
			_ = c.ws.SetWriteDeadline(time.Now().Add(writeWait))
			err := c.ws.WriteJSON(message)
			c.writeMu.Unlock()
			if err != nil {
				return
			}
		}
	}
}

func (c *yjsConn) writeLoop() {
	for {
		select {
		case <-c.done:
			return
		case message := <-c.send:
			c.writeMu.Lock()
			_ = c.ws.SetWriteDeadline(time.Now().Add(writeWait))
			err := c.ws.WriteMessage(websocket.BinaryMessage, message)
			c.writeMu.Unlock()
			if err != nil {
				return
			}
		}
	}
}

func (c *clientConn) enqueue(message outboundMessage) error {
	select {
	case <-c.done:
		return errors.New("connection is closed")
	case c.send <- message:
		return nil
	default:
		c.service.mu.Lock()
		c.service.stats.QueueOverflowsTotal++
		c.service.syncStatsLocked()
		c.service.mu.Unlock()
		c.service.metrics.QueueOverflowsTotal.Inc()
		c.close(openrtcerr.DescriptorFor(openrtcerr.CodeQueueOverflow).WSCloseCode, openrtcerr.WSCloseReason(openrtcerr.CodeQueueOverflow))
		return errors.New("outbound queue overflow")
	}
}

func (c *yjsConn) enqueueFrame(kind byte, update []byte) error {
	frame := make([]byte, 1+len(update))
	frame[0] = kind
	copy(frame[1:], update)

	select {
	case <-c.done:
		return errors.New("connection is closed")
	case c.send <- frame:
		return nil
	default:
		c.close(openrtcerr.DescriptorFor(openrtcerr.CodeQueueOverflow).WSCloseCode, openrtcerr.WSCloseReason(openrtcerr.CodeQueueOverflow))
		return errors.New("outbound queue overflow")
	}
}

func (c *clientConn) close(code int, reason string) {
	c.closeMu.Lock()
	defer c.closeMu.Unlock()
	if c.closed {
		return
	}
	c.closed = true
	close(c.done)
	if c.ws == nil {
		return
	}
	c.writeMu.Lock()
	_ = c.ws.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(code, reason), time.Now().Add(writeWait))
	c.writeMu.Unlock()
	_ = c.ws.Close()
}

func (c *yjsConn) close(code int, reason string) {
	c.closeMu.Lock()
	defer c.closeMu.Unlock()
	if c.closed {
		return
	}
	c.closed = true
	close(c.done)
	if c.ws == nil {
		return
	}
	c.writeMu.Lock()
	_ = c.ws.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(code, reason), time.Now().Add(writeWait))
	c.writeMu.Unlock()
	_ = c.ws.Close()
}

func (l *emitLimiter) Allow() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	nowWindow := time.Now().Unix()
	if l.window != nowWindow {
		l.window = nowWindow
		l.count = 0
	}
	if l.count >= l.limit {
		return false
	}
	l.count++
	return true
}
