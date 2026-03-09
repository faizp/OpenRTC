package runtimeapp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/openrtc/openrtc/server/internal/auth"
	"github.com/openrtc/openrtc/server/internal/cluster"
	"github.com/openrtc/openrtc/server/internal/config"
	openrtcerr "github.com/openrtc/openrtc/server/internal/errors"
	"github.com/openrtc/openrtc/server/internal/observability"
	"github.com/openrtc/openrtc/server/internal/protocol"
	"github.com/openrtc/openrtc/server/internal/stats"
)

const (
	heartbeatInterval = 15 * time.Second
	reconcileInterval = 30 * time.Second
	defaultJoinLimit  = 100
	writeWait         = 5 * time.Second
	readWait          = 30 * time.Second
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
	rooms    map[string]map[string]*clientConn
	presence map[string]map[string]json.RawMessage
	stats    stats.Snapshot
}

type clientConn struct {
	id      string
	ws      *websocket.Conn
	service *Service
	claims  *auth.Claims
	rooms   map[string]struct{}
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
		rooms:    make(map[string]map[string]*clientConn),
		presence: make(map[string]map[string]json.RawMessage),
	}

	if cfg.Redis != nil {
		store, err := cluster.NewRedisStore(cfg.Redis.URL, cfg.Redis.ChannelPrefix)
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
		CheckOrigin: func(*http.Request) bool { return true },
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
		rooms:   make(map[string]struct{}),
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

	switch message.Type {
	case protocol.TypeJoin:
		return s.handleJoin(conn, message)
	case protocol.TypeLeave:
		return s.handleLeave(conn, message)
	case protocol.TypeEmit:
		return s.handleEmit(conn, message)
	case protocol.TypePresenceSet:
		return s.handlePresence(conn, message)
	default:
		return conn.enqueue(outboundMessage{
			T: "ERROR",
			Payload: openrtcerr.APIError{
				Code:    openrtcerr.CodeBadRequest,
				Message: "unsupported message type",
			},
		})
	}
}

func (s *Service) handleJoin(conn *clientConn, message protocol.Message) error {
	if !conn.claims.Allows("join", message.Room, s.cfg.Tenant.EnforcePrefix, s.cfg.Tenant.Separator) {
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

	s.mu.Lock()
	if _, exists := conn.rooms[message.Room]; exists {
		s.mu.Unlock()
		roomMembers, roomPresence, nextCursor, err := s.snapshotRoom(message.Room, message.JoinMeta)
		if err != nil {
			return err
		}
		return conn.enqueue(outboundMessage{T: "JOINED", ID: message.ID, Room: message.Room, Payload: map[string]any{
			"members":     roomMembers,
			"presence":    roomPresence,
			"next_cursor": nextCursor,
		}})
	}
	if len(conn.rooms) >= s.cfg.Limits.RoomsPerConnection {
		s.mu.Unlock()
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

	conn.rooms[message.Room] = struct{}{}
	members := s.rooms[message.Room]
	if members == nil {
		members = make(map[string]*clientConn)
		s.rooms[message.Room] = members
	}
	members[conn.id] = conn
	s.stats.JoinsTotal++
	s.syncStatsLocked()
	s.mu.Unlock()

	s.metrics.JoinsTotal.Inc()
	if s.store != nil {
		if err := s.store.JoinRoom(s.ctx, conn.id, message.Room); err != nil {
			return err
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
	s.mu.Lock()
	if _, exists := conn.rooms[message.Room]; exists {
		delete(conn.rooms, message.Room)
		if members := s.rooms[message.Room]; members != nil {
			delete(members, conn.id)
			if len(members) == 0 {
				delete(s.rooms, message.Room)
				delete(s.presence, message.Room)
			}
		}
		s.stats.LeavesTotal++
		s.syncStatsLocked()
	}
	s.mu.Unlock()

	s.metrics.LeavesTotal.Inc()
	if s.store != nil {
		if err := s.store.LeaveRoom(s.ctx, conn.id, message.Room); err != nil {
			return err
		}
	}

	return conn.enqueue(outboundMessage{T: "LEFT", ID: message.ID, Room: message.Room})
}

func (s *Service) handleEmit(conn *clientConn, message protocol.Message) error {
	if !conn.claims.Allows("publish", message.Room, s.cfg.Tenant.EnforcePrefix, s.cfg.Tenant.Separator) {
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
	if !conn.claims.Allows("presence", message.Room, s.cfg.Tenant.EnforcePrefix, s.cfg.Tenant.Separator) {
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

	s.mu.Lock()
	roomPresence := s.presence[message.Room]
	if roomPresence == nil {
		roomPresence = make(map[string]json.RawMessage)
		s.presence[message.Room] = roomPresence
	}
	roomPresence[conn.id] = append(json.RawMessage(nil), message.Payload...)
	s.stats.PresenceUpdatesTotal++
	s.syncStatsLocked()
	s.mu.Unlock()

	s.metrics.PresenceUpdatesTotal.Inc()
	if s.store != nil {
		if err := s.store.SetPresence(s.ctx, conn.id, message.Room, message.Payload); err != nil {
			return err
		}
	}

	return s.broadcastPresence(message.Room, conn.id, message.Payload)
}

func (s *Service) handleClusterEvent(event cluster.PublishedEvent) {
	if event.OriginNode == s.cfg.NodeID {
		return
	}
	_ = s.broadcastEvent(event, false)
}

func (s *Service) broadcastEvent(event cluster.PublishedEvent, countMetric bool) error {
	s.mu.RLock()
	members := s.rooms[event.Room]
	targets := make([]*clientConn, 0, len(members))
	for connID, member := range members {
		if event.ExcludeSenderConnID != "" && connID == event.ExcludeSenderConnID {
			continue
		}
		targets = append(targets, member)
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

func (s *Service) broadcastPresence(room string, connID string, state json.RawMessage) error {
	s.mu.RLock()
	members := s.rooms[room]
	targets := make([]*clientConn, 0, len(members))
	for _, member := range members {
		targets = append(targets, member)
	}
	s.mu.RUnlock()

	for _, target := range targets {
		if err := target.enqueue(outboundMessage{
			T:    "PRESENCE",
			Room: room,
			Payload: map[string]any{
				"conn_id": connID,
				"state":   state,
			},
		}); err != nil {
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

	s.mu.RLock()
	localMembers := s.rooms[room]
	members := make([]string, 0, len(localMembers))
	for connID := range localMembers {
		members = append(members, connID)
	}
	presence := make(map[string]json.RawMessage, len(s.presence[room]))
	for connID, state := range s.presence[room] {
		presence[connID] = state
	}
	s.mu.RUnlock()

	page, pagePresence, nextCursor := protocol.PaginateMembers(members, presence, limit, cursor)
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

func (s *Service) unregisterConn(conn *clientConn) {
	s.mu.Lock()
	delete(s.conns, conn.id)
	for room := range conn.rooms {
		if members := s.rooms[room]; members != nil {
			delete(members, conn.id)
			if len(members) == 0 {
				delete(s.rooms, room)
				delete(s.presence, room)
			}
		}
	}
	s.syncStatsLocked()
	s.mu.Unlock()
	s.metrics.ActiveConnections.Dec()
	conn.close(websocket.CloseNormalClosure, "closing")

	if s.store != nil {
		_ = s.store.CleanupConnection(s.ctx, s.cfg.NodeID, conn.id)
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
		ActiveRooms:          int64(len(s.rooms)),
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

func (s *Service) tenantPrefix(claims *auth.Claims) string {
	if !s.cfg.Tenant.EnforcePrefix || claims.Tenant == "" {
		return ""
	}
	return claims.Tenant + s.cfg.Tenant.Separator
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

func newConnID() string {
	raw := make([]byte, 12)
	if _, err := rand.Read(raw); err != nil {
		panic(err)
	}
	return hex.EncodeToString(raw)
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
