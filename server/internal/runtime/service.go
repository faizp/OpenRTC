package runtimeapp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
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
		protocol.TypeJoin:        (*Service).handleJoin,
		protocol.TypeLeave:       (*Service).handleLeave,
		protocol.TypeEmit:        (*Service).handleEmit,
		protocol.TypePresenceSet: (*Service).handlePresence,
	}
	sendInitialYJSDocument = sendYJSDocument
)

const (
	defaultJoinLimit = 100
	yjsPathPrefix    = "/yjs/"
	yjsFrameUpdate   = byte(cluster.YJSEventUpdate)
	yjsFrameSnapshot = byte(cluster.YJSEventSnapshot)
	writeWait        = 5 * time.Second
	readWait         = 30 * time.Second
)

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
	yjsRooms map[string]map[string]*yjsConn
	yjsDocs  map[string]*memoryYJSDocument
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

type memoryYJSDocument struct {
	Snapshot           []byte
	SnapshotCheckpoint int64
	Updates            [][]byte
	UpdateSequences    []int64
	NextSequence       int64
}

type outboundMessage struct {
	T       string      `json:"t"`
	ID      string      `json:"id,omitempty"`
	Room    string      `json:"room,omitempty"`
	Event   string      `json:"event,omitempty"`
	Payload interface{} `json:"payload,omitempty"`
	Meta    interface{} `json:"meta,omitempty"`
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
		yjsRooms: make(map[string]map[string]*yjsConn),
		yjsDocs:  make(map[string]*memoryYJSDocument),
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

func (s *Service) Close() error {
	s.cancel()
	if s.store != nil {
		return s.store.Close()
	}
	return nil
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
		if !s.allowsRoomAction(r.Context(), claims, "publish", room) {
			conn.close(openrtcerr.DescriptorFor(openrtcerr.CodeRoomForbidden).WSCloseCode, openrtcerr.WSCloseReason(openrtcerr.CodeRoomForbidden))
			return
		}
		if !conn.limiter.Allow() {
			conn.close(openrtcerr.DescriptorFor(openrtcerr.CodeRateLimited).WSCloseCode, openrtcerr.WSCloseReason(openrtcerr.CodeRateLimited))
			return
		}

		kind := payload[0]
		if kind != yjsFrameUpdate {
			conn.close(openrtcerr.DescriptorFor(openrtcerr.CodeBadRequest).WSCloseCode, openrtcerr.WSCloseReason(openrtcerr.CodeBadRequest))
			return
		}
		update := append([]byte(nil), payload[1:]...)
		event := cluster.YJSEvent{
			Room:         room,
			Kind:         cluster.YJSEventKind(kind),
			Update:       update,
			OriginNode:   s.cfg.NodeID,
			OriginConnID: conn.id,
		}
		event, err = s.storeYJSEvent(event)
		if err != nil {
			conn.close(openrtcerr.DescriptorFor(openrtcerr.CodeInternal).WSCloseCode, openrtcerr.WSCloseReason(openrtcerr.CodeInternal))
			return
		}
		if err := s.broadcastYJSEvent(event); err != nil {
			return
		}
		if s.store != nil {
			if err := s.store.PublishYJSEvent(s.ctx, event); err != nil {
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
	if len(document.Snapshot) > 0 {
		if err := conn.enqueueFrame(yjsFrameSnapshot, document.Snapshot); err != nil {
			return err
		}
	}
	for _, update := range document.Updates {
		if err := conn.enqueueFrame(yjsFrameUpdate, update); err != nil {
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

	if !joinResult.AlreadyJoined {
		s.mu.Lock()
		s.stats.JoinsTotal++
		s.syncStatsLocked()
		s.mu.Unlock()

		s.metrics.JoinsTotal.Inc()
		if s.store != nil {
			if err := s.store.JoinRoom(s.ctx, conn.id, message.Room); err != nil {
				return err
			}
		}
	}

	roomMembers, roomPresence, nextCursor, err := s.snapshotRoom(message.Room, message.JoinMeta)
	if err != nil {
		return err
	}

	return conn.enqueue(outboundMessage{
		T:    "JOINED",
		ID:   message.ID,
		Room: message.Room,
		Payload: map[string]any{
			"members":     roomMembers,
			"presence":    roomPresence,
			"next_cursor": nextCursor,
		},
	})
}

func (s *Service) handleLeave(conn *clientConn, message protocol.Message) error {
	leaveResult := s.roomEngine().Leave(conn.id, message.Room)
	if leaveResult.Left {
		s.mu.Lock()
		s.stats.LeavesTotal++
		s.syncStatsLocked()
		s.mu.Unlock()
	}

	if leaveResult.Left {
		s.metrics.LeavesTotal.Inc()
	}
	if leaveResult.Left && s.store != nil {
		if err := s.store.LeaveRoom(s.ctx, conn.id, message.Room); err != nil {
			return err
		}
	}
	if leaveResult.Left {
		event := cluster.PresenceEvent{
			Room:       message.Room,
			ConnID:     conn.id,
			Offline:    true,
			OriginNode: s.cfg.NodeID,
		}
		if err := s.broadcastPresenceEvent(event); err != nil {
			return err
		}
		if s.store != nil {
			if err := s.store.PublishPresence(s.ctx, event); err != nil {
				return err
			}
		}
	}

	return conn.enqueue(outboundMessage{T: "LEFT", ID: message.ID, Room: message.Room})
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

	if err := s.broadcastEvent(cluster.PublishedEvent{
		Room:       message.Room,
		Event:      message.Event,
		Payload:    message.Payload,
		TraceID:    traceID,
		OriginNode: s.cfg.NodeID,
	}, true); err != nil {
		return err
	}
	if s.store != nil {
		return s.store.PublishEvent(s.ctx, cluster.PublishedEvent{
			Room:       message.Room,
			Event:      message.Event,
			Payload:    message.Payload,
			TraceID:    traceID,
			OriginNode: s.cfg.NodeID,
		})
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

	s.roomEngine().SetPresence(conn.id, message.Room, message.Payload)
	s.mu.Lock()
	s.stats.PresenceUpdatesTotal++
	s.syncStatsLocked()
	s.mu.Unlock()

	s.metrics.PresenceUpdatesTotal.Inc()
	if s.store != nil {
		if err := s.store.SetPresence(s.ctx, conn.id, message.Room, message.Payload); err != nil {
			return err
		}
	}

	event := cluster.PresenceEvent{
		Room:       message.Room,
		ConnID:     conn.id,
		State:      append(json.RawMessage(nil), message.Payload...),
		OriginNode: s.cfg.NodeID,
	}
	if err := s.broadcastPresenceEvent(event); err != nil {
		return err
	}
	if s.store != nil {
		return s.store.PublishPresence(s.ctx, event)
	}
	return nil
}

func (s *Service) handleClusterEvent(event cluster.PublishedEvent) {
	if event.OriginNode == s.cfg.NodeID {
		return
	}
	_ = s.broadcastEvent(event, false)
}

func (s *Service) handleClusterPresence(event cluster.PresenceEvent) {
	if event.OriginNode == s.cfg.NodeID {
		return
	}
	_ = s.broadcastPresenceEvent(event)
}

func (s *Service) broadcastEvent(event cluster.PublishedEvent, countMetric bool) error {
	targetIDs := s.roomEngine().MemberIDs(event.Room, event.ExcludeSenderConnID)
	s.mu.RLock()
	targets := make([]*clientConn, 0, len(targetIDs))
	for _, connID := range targetIDs {
		if member := s.conns[connID]; member != nil {
			targets = append(targets, member)
		}
	}
	s.mu.RUnlock()

	for _, target := range targets {
		if err := target.enqueue(outboundMessage{
			T:       "EVENT",
			Room:    event.Room,
			Event:   event.Event,
			Payload: event.Payload,
			Meta: map[string]any{
				"trace_id": event.TraceID,
			},
		}); err != nil {
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

func (s *Service) broadcastPresenceEvent(event cluster.PresenceEvent) error {
	targetIDs := s.roomEngine().MemberIDs(event.Room, "")
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

func (s *Service) handleClusterYJSEvent(event cluster.YJSEvent) {
	if event.OriginNode == s.cfg.NodeID {
		return
	}
	_ = s.broadcastYJSEvent(event)
}

func (s *Service) loadYJSDocument(room string) (cluster.YJSDocument, error) {
	if s.store != nil {
		return s.store.LoadYJSDocument(s.ctx, room)
	}

	s.mu.RLock()
	doc := s.yjsDocs[room]
	if doc == nil {
		s.mu.RUnlock()
		return cluster.YJSDocument{}, nil
	}
	out := cluster.YJSDocument{
		Snapshot:           append([]byte(nil), doc.Snapshot...),
		SnapshotCheckpoint: doc.SnapshotCheckpoint,
		Updates:            make([][]byte, 0, len(doc.Updates)),
		UpdateSequences:    append([]int64(nil), doc.UpdateSequences...),
	}
	for _, update := range doc.Updates {
		out.Updates = append(out.Updates, append([]byte(nil), update...))
	}
	s.mu.RUnlock()
	return out, nil
}

func (s *Service) storeYJSEvent(event cluster.YJSEvent) (cluster.YJSEvent, error) {
	if s.store != nil {
		if event.Kind == cluster.YJSEventSnapshot {
			return event, s.store.StoreYJSSnapshot(s.ctx, event.Room, event.Update)
		}
		sequence, err := s.store.AppendYJSUpdate(s.ctx, event.Room, event.Update)
		event.Sequence = sequence
		return event, err
	}

	s.mu.Lock()
	doc := s.yjsDocs[event.Room]
	if doc == nil {
		doc = &memoryYJSDocument{}
		s.yjsDocs[event.Room] = doc
	}
	if event.Kind == cluster.YJSEventSnapshot {
		doc.Snapshot = append([]byte(nil), event.Update...)
	} else {
		doc.NextSequence++
		event.Sequence = doc.NextSequence
		doc.Updates = append(doc.Updates, append([]byte(nil), event.Update...))
		doc.UpdateSequences = append(doc.UpdateSequences, doc.NextSequence)
	}
	s.mu.Unlock()
	return event, nil
}

func (s *Service) broadcastYJSEvent(event cluster.YJSEvent) error {
	s.mu.RLock()
	members := s.yjsRooms[event.Room]
	targets := make([]*yjsConn, 0, len(members))
	for connID, member := range members {
		if event.OriginConnID != "" && connID == event.OriginConnID {
			continue
		}
		targets = append(targets, member)
	}
	s.mu.RUnlock()

	for _, target := range targets {
		if err := target.enqueueFrame(byte(event.Kind), event.Update); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) snapshotRoom(room string, joinMeta *protocol.JoinMeta) ([]string, map[string]json.RawMessage, string, error) {
	limit := defaultJoinLimit
	cursor := ""
	if joinMeta != nil {
		if joinMeta.Limit > 0 {
			limit = joinMeta.Limit
		}
		cursor = joinMeta.Cursor
	}

	if s.store != nil {
		snapshot, err := s.store.SnapshotRoom(s.ctx, room)
		if err != nil {
			return nil, nil, "", err
		}
		members, presence, nextCursor := protocol.PaginateMembers(snapshot.Members, snapshot.Presence, limit, cursor)
		return members, presence, nextCursor, nil
	}

	snapshot := s.roomEngine().Snapshot(room)

	page, pagePresence, nextCursor := protocol.PaginateMembers(snapshot.Members, snapshot.Presence, limit, cursor)
	return page, pagePresence, nextCursor, nil
}

func (s *Service) registerConn(conn *clientConn) {
	s.mu.Lock()
	s.conns[conn.id] = conn
	s.syncStatsLocked()
	s.mu.Unlock()
	s.metrics.ActiveConnections.Inc()

	if s.store != nil {
		_ = s.store.TouchConnection(s.ctx, conn.id, cluster.ConnectionMeta{
			NodeID:      s.cfg.NodeID,
			Subject:     conn.claims.Subject,
			Tenant:      conn.claims.Tenant,
			ConnectedAt: time.Now(),
		})
	}
}

func (s *Service) registerYJSConn(conn *yjsConn) {
	s.mu.Lock()
	members := s.yjsRooms[conn.room]
	if members == nil {
		members = make(map[string]*yjsConn)
		s.yjsRooms[conn.room] = members
	}
	members[conn.id] = conn
	s.mu.Unlock()
}

func (s *Service) unregisterYJSConn(conn *yjsConn) {
	s.mu.Lock()
	if members := s.yjsRooms[conn.room]; members != nil {
		delete(members, conn.id)
		if len(members) == 0 {
			delete(s.yjsRooms, conn.room)
		}
	}
	s.mu.Unlock()
	conn.close(websocket.CloseNormalClosure, "closing")
}

func (s *Service) unregisterConn(conn *clientConn) {
	rooms := s.roomEngine().Disconnect(conn.id)
	s.mu.Lock()
	delete(s.conns, conn.id)
	s.syncStatsLocked()
	s.mu.Unlock()
	s.metrics.ActiveConnections.Dec()
	conn.close(websocket.CloseNormalClosure, "closing")

	if s.store != nil {
		_ = s.store.CleanupConnection(s.ctx, s.cfg.NodeID, conn.id)
	}
	for _, room := range rooms {
		event := cluster.PresenceEvent{
			Room:       room,
			ConnID:     conn.id,
			Offline:    true,
			OriginNode: s.cfg.NodeID,
		}
		_ = s.broadcastPresenceEvent(event)
		if s.store != nil {
			_ = s.store.PublishPresence(s.ctx, event)
		}
	}
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

			if s.store != nil {
				_ = s.store.TouchConnection(s.ctx, conn.id, cluster.ConnectionMeta{
					NodeID:      s.cfg.NodeID,
					Subject:     conn.claims.Subject,
					Tenant:      conn.claims.Tenant,
					ConnectedAt: time.Now(),
				})
			}
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
