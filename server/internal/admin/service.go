package admin

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"sync"

	"github.com/openrtc/openrtc/server/internal/auth"
	"github.com/openrtc/openrtc/server/internal/cluster"
	"github.com/openrtc/openrtc/server/internal/config"
	openrtcerr "github.com/openrtc/openrtc/server/internal/errors"
	"github.com/openrtc/openrtc/server/internal/observability"
	"github.com/openrtc/openrtc/server/internal/stats"
)

type Service struct {
	cfg      config.RuntimeConfig
	logger   *log.Logger
	verifier *auth.Verifier
	store    cluster.Store
	metrics  *observability.AdminMetrics

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

func NewService(cfg config.RuntimeConfig, logger *log.Logger) (*Service, error) {
	var verifier *auth.Verifier
	if cfg.AdminAuth != nil {
		verifier = auth.NewVerifier(cfg.AdminAuth.Issuer, cfg.AdminAuth.Audience, cfg.Auth.JWKSURL)
	}

	service := &Service{
		cfg:      cfg,
		logger:   logger,
		verifier: verifier,
		metrics:  observability.NewAdminMetrics(),
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

func (s *Service) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/publish", s.handlePublish)
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
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		s.writeError(w, openrtcerr.CodeBadRequest, "request body must be valid JSON", "", http.StatusBadRequest)
		return
	}
	if request.Room == "" || request.Event == "" || len(request.Payload) == 0 {
		s.writeError(w, openrtcerr.CodeBadRequest, "room, event, and payload are required", "", http.StatusBadRequest)
		return
	}
	if !claims.Allows("publish", request.Room, s.cfg.Tenant.EnforcePrefix, s.cfg.Tenant.Separator) {
		s.writeError(w, openrtcerr.CodeRoomForbidden, "room publish is not permitted", "", http.StatusForbidden)
		return
	}

	if err := s.store.PublishEvent(r.Context(), cluster.PublishedEvent{
		Room:                request.Room,
		Event:               request.Event,
		Payload:             request.Payload,
		ExcludeSenderConnID: request.ExcludeSenderConnID,
		TraceID:             request.TraceID,
		OriginNode:          "admin:" + s.cfg.NodeID,
	}); err != nil {
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

func (s *Service) writeError(w http.ResponseWriter, code openrtcerr.Code, message string, requestID string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(openrtcerr.APIError{
		Code:      code,
		Message:   message,
		RequestID: requestID,
	})
}
