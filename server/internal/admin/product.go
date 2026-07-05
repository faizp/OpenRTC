package admin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/openrtc/openrtc/server/internal/auth"
	"github.com/openrtc/openrtc/server/internal/cluster"
	openrtcerr "github.com/openrtc/openrtc/server/internal/errors"
	"github.com/openrtc/openrtc/server/internal/protocol"
	"github.com/openrtc/openrtc/server/internal/stats"
)

type productionStore interface {
	CreateTenant(context.Context, cluster.TenantRecord) (cluster.TenantRecord, error)
	GetTenant(context.Context, string) (cluster.TenantRecord, error)
	UpdateTenant(context.Context, string, cluster.TenantUpdate) (cluster.TenantRecord, error)
	ListTenants(context.Context) ([]cluster.TenantRecord, error)
	CreateProject(context.Context, cluster.ProjectRecord) (cluster.ProjectRecord, error)
	GetProject(context.Context, string, string) (cluster.ProjectRecord, error)
	UpdateProject(context.Context, string, string, cluster.ProjectUpdate) (cluster.ProjectRecord, error)
	ListProjects(context.Context, string) ([]cluster.ProjectRecord, error)
	CreateAPIKey(context.Context, cluster.APIKeyRecord) (cluster.APIKeyRecord, error)
	GetAPIKey(context.Context, string) (cluster.APIKeyRecord, error)
	ListAPIKeys(context.Context, string, string) ([]cluster.APIKeyRecord, error)
	RevokeAPIKey(context.Context, string) (cluster.APIKeyRecord, error)
	RecordAuditLog(context.Context, cluster.AuditLogRecord) (cluster.AuditLogRecord, error)
	ListAuditLogs(context.Context, string, string, int) ([]cluster.AuditLogRecord, error)
	IncrementUsage(context.Context, cluster.UsageIncrement) (cluster.UsageRecord, error)
	ListUsage(context.Context, string, string, string, string) ([]cluster.UsageRecord, error)
	CreateWebhookDelivery(context.Context, cluster.WebhookDeliveryRecord) (cluster.WebhookDeliveryRecord, error)
	GetWebhookDelivery(context.Context, string) (cluster.WebhookDeliveryRecord, error)
	UpdateWebhookDelivery(context.Context, cluster.WebhookDeliveryRecord) (cluster.WebhookDeliveryRecord, error)
	ListWebhookDeliveries(context.Context, string, int) ([]cluster.WebhookDeliveryRecord, error)
	CreateVersionSnapshot(context.Context, cluster.VersionSnapshotRecord) (cluster.VersionSnapshotRecord, error)
	GetVersionSnapshot(context.Context, string, string, string) (cluster.VersionSnapshotRecord, error)
	ListVersionSnapshots(context.Context, string, string, int) ([]cluster.VersionSnapshotRecord, error)
	UpsertRichTextDocument(context.Context, cluster.RichTextDocumentRecord) (cluster.RichTextDocumentRecord, error)
	GetRichTextDocument(context.Context, string, string) (cluster.RichTextDocumentRecord, error)
	ListRichTextDocuments(context.Context, string) ([]cluster.RichTextDocumentRecord, error)
	UpsertResumeSession(context.Context, cluster.ResumeSessionRecord) (cluster.ResumeSessionRecord, error)
	GetResumeSession(context.Context, string) (cluster.ResumeSessionRecord, error)
	DeleteResumeSession(context.Context, string) error
	ListResumeSessions(context.Context, string, string, string, string, bool, int) ([]cluster.ResumeSessionRecord, error)
}

type tenantRequest struct {
	ID       string          `json:"id"`
	Name     string          `json:"name,omitempty"`
	Metadata json.RawMessage `json:"metadata,omitempty"`
}

type tenantUpdateRequest struct {
	Name     *string          `json:"name,omitempty"`
	Metadata *json.RawMessage `json:"metadata,omitempty"`
}

type projectRequest struct {
	ID       string          `json:"id"`
	Name     string          `json:"name,omitempty"`
	Metadata json.RawMessage `json:"metadata,omitempty"`
}

type projectUpdateRequest struct {
	Name     *string          `json:"name,omitempty"`
	Metadata *json.RawMessage `json:"metadata,omitempty"`
}

type apiKeyCreateRequest struct {
	TenantID  string   `json:"tenantId"`
	ProjectID string   `json:"projectId"`
	Name      string   `json:"name"`
	Scopes    []string `json:"scopes,omitempty"`
}

type apiKeyCreateResponse struct {
	cluster.APIKeyRecord
	Secret string `json:"secret"`
}

type versionSnapshotRequest struct {
	ID         string          `json:"id,omitempty"`
	DocumentID string          `json:"documentId,omitempty"`
	Label      string          `json:"label,omitempty"`
	Document   json.RawMessage `json:"document,omitempty"`
	Metadata   json.RawMessage `json:"metadata,omitempty"`
}

type richTextDocumentRequest struct {
	Content  json.RawMessage `json:"content"`
	Metadata json.RawMessage `json:"metadata,omitempty"`
}

type resumeSessionRequest struct {
	ID          string            `json:"id,omitempty"`
	TenantID    string            `json:"tenantId,omitempty"`
	ProjectID   string            `json:"projectId,omitempty"`
	Subject     string            `json:"subject"`
	Rooms       []string          `json:"rooms"`
	RoomCursors map[string]uint64 `json:"roomCursors,omitempty"`
	Metadata    json.RawMessage   `json:"metadata,omitempty"`
	TTLSeconds  int               `json:"ttlSeconds,omitempty"`
}

var errProductConsoleForbidden = errors.New("product console access is not permitted")

type productConsoleSelection struct {
	TenantID  string
	ProjectID string
	RoomID    string
	Limit     int
}

type productEnvironmentSummary struct {
	Environment string `json:"environment,omitempty"`
	Region      string `json:"region,omitempty"`
}

type productDashboardSummary struct {
	Rooms       int `json:"rooms"`
	ActiveUsers int `json:"activeUsers"`
	Events      int `json:"events"`
	StorageDocs int `json:"storageDocs"`
	Errors      int `json:"errors"`
}

type productStorageSnapshot struct {
	Found    bool            `json:"found"`
	Document json.RawMessage `json:"document,omitempty"`
}

type productConsoleError struct {
	Source    string    `json:"source"`
	Message   string    `json:"message"`
	Status    string    `json:"status,omitempty"`
	Resource  string    `json:"resource,omitempty"`
	CreatedAt time.Time `json:"createdAt,omitempty"`
	UpdatedAt time.Time `json:"updatedAt,omitempty"`
}

type productDashboardResponse struct {
	GeneratedAt       time.Time                        `json:"generatedAt"`
	TenantID          string                           `json:"tenantId"`
	ProjectID         string                           `json:"projectId"`
	RoomID            string                           `json:"roomId,omitempty"`
	Tenant            cluster.TenantRecord             `json:"tenant"`
	Project           cluster.ProjectRecord            `json:"project"`
	Environment       productEnvironmentSummary        `json:"environment"`
	Summary           productDashboardSummary          `json:"summary"`
	APIKeys           []cluster.APIKeyRecord           `json:"apiKeys"`
	Rooms             []cluster.RoomRecord             `json:"rooms"`
	ActiveUsers       []cluster.ActiveUser             `json:"activeUsers"`
	Events            []cluster.PublishedEvent         `json:"events"`
	Storage           productStorageSnapshot           `json:"storage"`
	Stats             stats.Snapshot                   `json:"stats"`
	Usage             []cluster.UsageRecord            `json:"usage"`
	AuditLogs         []cluster.AuditLogRecord         `json:"auditLogs"`
	WebhookDeliveries []cluster.WebhookDeliveryRecord  `json:"webhookDeliveries"`
	ResumeSessions    []cluster.ResumeSessionRecord    `json:"resumeSessions"`
	RichTextDocuments []cluster.RichTextDocumentRecord `json:"richTextDocuments"`
	VersionSnapshots  []cluster.VersionSnapshotRecord  `json:"versionSnapshots"`
	Errors            []productConsoleError            `json:"errors"`
	Observability     productDashboardObservability    `json:"observability"`
}

type productDashboardObservability struct {
	Logs     []cluster.AuditLogRecord        `json:"logs"`
	Errors   []productConsoleError           `json:"errors"`
	Usage    []cluster.UsageRecord           `json:"usage"`
	Webhooks []cluster.WebhookDeliveryRecord `json:"webhooks"`
}

type productStatusCheck struct {
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	Message   string    `json:"message,omitempty"`
	CheckedAt time.Time `json:"checkedAt"`
}

type productStatusResponse struct {
	GeneratedAt time.Time                 `json:"generatedAt"`
	Status      string                    `json:"status"`
	TenantID    string                    `json:"tenantId"`
	ProjectID   string                    `json:"projectId"`
	RoomID      string                    `json:"roomId,omitempty"`
	Environment productEnvironmentSummary `json:"environment"`
	Checks      []productStatusCheck      `json:"checks"`
	Errors      []productConsoleError     `json:"errors"`
	PublicPage  productPublicStatusPage   `json:"publicPage"`
}

type productPublicStatusPage struct {
	Title   string `json:"title"`
	Message string `json:"message"`
}

type productSupportDebugBundleResponse struct {
	GeneratedAt      time.Time                 `json:"generatedAt"`
	TenantID         string                    `json:"tenantId"`
	ProjectID        string                    `json:"projectId"`
	RoomID           string                    `json:"roomId,omitempty"`
	Environment      productEnvironmentSummary `json:"environment"`
	Dashboard        productDashboardResponse  `json:"dashboard"`
	Status           productStatusResponse     `json:"status"`
	SafeConfig       productSafeConfig         `json:"safeConfig"`
	SuggestedActions []string                  `json:"suggestedActions"`
}

type productSafeConfig struct {
	Mode                  string `json:"mode"`
	NodeID                string `json:"nodeId,omitempty"`
	RedisConfigured       bool   `json:"redisConfigured"`
	RuntimeAuthConfigured bool   `json:"runtimeAuthConfigured"`
	AdminAuthConfigured   bool   `json:"adminAuthConfigured"`
	WebhooksConfigured    bool   `json:"webhooksConfigured"`
	WebhookEndpointCount  int    `json:"webhookEndpointCount"`
	WebSocketPath         string `json:"webSocketPath"`
	TenantEnforcePrefix   bool   `json:"tenantEnforcePrefix"`
	TenantSeparator       string `json:"tenantSeparator"`
	PayloadMaxBytes       int    `json:"payloadMaxBytes"`
	EnvelopeMaxBytes      int    `json:"envelopeMaxBytes"`
}

type listResponse[T any] struct {
	Data []T `json:"data"`
}

func (s *Service) handleTenants(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleListTenants(w, r)
	case http.MethodPost:
		s.handleCreateTenant(w, r)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Service) handleTenant(w http.ResponseWriter, r *http.Request) {
	tenantID, child, err := tenantPathParts(r.URL.Path)
	if err != nil {
		s.writeError(w, openrtcerr.CodeBadRequest, err.Error(), "", http.StatusBadRequest)
		return
	}
	if child == "projects" || strings.HasPrefix(child, "projects/") {
		s.handleTenantProjects(w, r, tenantID, child)
		return
	}
	if child != "" {
		s.writeError(w, openrtcerr.CodeBadRequest, "unsupported tenant subresource", "", http.StatusBadRequest)
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.handleGetTenant(w, r, tenantID)
	case http.MethodPatch:
		s.handleUpdateTenant(w, r, tenantID)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Service) handleListTenants(w http.ResponseWriter, r *http.Request) {
	claims, store, ok := s.authenticateProduction(w, r)
	if !ok {
		return
	}
	tenants, err := store.ListTenants(r.Context())
	if err != nil {
		s.writeError(w, openrtcerr.CodeInternal, err.Error(), "", http.StatusInternalServerError)
		return
	}
	if s.cfg.Tenant.EnforcePrefix && claims.Tenant != "" && !s.allowsTenantAdmin(claims, "*") {
		filtered := tenants[:0]
		for _, tenant := range tenants {
			if tenant.ID == claims.Tenant {
				filtered = append(filtered, tenant)
			}
		}
		tenants = filtered
	}
	writeJSON(w, http.StatusOK, listResponse[cluster.TenantRecord]{Data: tenants})
}

func (s *Service) handleCreateTenant(w http.ResponseWriter, r *http.Request) {
	claims, store, ok := s.authenticateProduction(w, r)
	if !ok {
		return
	}
	var request tenantRequest
	if err := decodeRequest(w, r, s.cfg.Limits.EnvelopeMaxBytes, &request); err != nil {
		s.writeError(w, openrtcerr.CodeBadRequest, "request body must be valid JSON", "", http.StatusBadRequest)
		return
	}
	if err := validateProductID("tenant id", request.ID); err != nil {
		s.writeError(w, openrtcerr.CodeBadRequest, err.Error(), "", http.StatusBadRequest)
		return
	}
	if err := validateProductMetadata(request.Metadata, true, s.cfg.Limits.PayloadMaxBytes); err != nil {
		s.writeError(w, err.Code, err.Message, "", openrtcerr.DescriptorFor(err.Code).HTTPStatus)
		return
	}
	if !s.allowsTenantAdmin(claims, request.ID) {
		s.writeError(w, openrtcerr.CodeRoomForbidden, "tenant administration is not permitted", "", http.StatusForbidden)
		return
	}
	record, err := store.CreateTenant(r.Context(), cluster.TenantRecord{
		ID:       request.ID,
		Name:     request.Name,
		Metadata: normalizedMetadata(request.Metadata),
	})
	if errors.Is(err, cluster.ErrTenantAlreadyExists) {
		s.writeError(w, openrtcerr.CodeRoomConflict, "tenant already exists", "", http.StatusConflict)
		return
	}
	if err != nil {
		s.writeError(w, openrtcerr.CodeInternal, err.Error(), "", http.StatusInternalServerError)
		return
	}
	s.recordAuditLog(r.Context(), claims, "tenant.create", "tenant", record.ID, record.ID, "", map[string]any{"name": record.Name})
	writeJSON(w, http.StatusCreated, record)
}

func (s *Service) handleGetTenant(w http.ResponseWriter, r *http.Request, tenantID string) {
	claims, store, ok := s.authenticateProduction(w, r)
	if !ok {
		return
	}
	if !s.allowsTenantAdmin(claims, tenantID) {
		s.writeError(w, openrtcerr.CodeRoomForbidden, "tenant administration is not permitted", "", http.StatusForbidden)
		return
	}
	record, err := store.GetTenant(r.Context(), tenantID)
	if errors.Is(err, cluster.ErrTenantNotFound) {
		s.writeError(w, openrtcerr.CodeRoomNotFound, "tenant not found", "", http.StatusNotFound)
		return
	}
	if err != nil {
		s.writeError(w, openrtcerr.CodeInternal, err.Error(), "", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, record)
}

func (s *Service) handleUpdateTenant(w http.ResponseWriter, r *http.Request, tenantID string) {
	claims, store, ok := s.authenticateProduction(w, r)
	if !ok {
		return
	}
	if !s.allowsTenantAdmin(claims, tenantID) {
		s.writeError(w, openrtcerr.CodeRoomForbidden, "tenant administration is not permitted", "", http.StatusForbidden)
		return
	}
	var request tenantUpdateRequest
	if err := decodeRequest(w, r, s.cfg.Limits.EnvelopeMaxBytes, &request); err != nil {
		s.writeError(w, openrtcerr.CodeBadRequest, "request body must be valid JSON", "", http.StatusBadRequest)
		return
	}
	update := cluster.TenantUpdate{}
	if request.Name != nil {
		update.Name = strings.TrimSpace(*request.Name)
		update.NameSet = true
	}
	if request.Metadata != nil {
		if err := validateProductMetadata(*request.Metadata, false, s.cfg.Limits.PayloadMaxBytes); err != nil {
			s.writeError(w, err.Code, err.Message, "", openrtcerr.DescriptorFor(err.Code).HTTPStatus)
			return
		}
		update.Metadata = normalizedMetadata(*request.Metadata)
		update.MetadataSet = true
	}
	record, err := store.UpdateTenant(r.Context(), tenantID, update)
	if errors.Is(err, cluster.ErrTenantNotFound) {
		s.writeError(w, openrtcerr.CodeRoomNotFound, "tenant not found", "", http.StatusNotFound)
		return
	}
	if err != nil {
		s.writeError(w, openrtcerr.CodeInternal, err.Error(), "", http.StatusInternalServerError)
		return
	}
	s.recordAuditLog(r.Context(), claims, "tenant.update", "tenant", tenantID, tenantID, "", nil)
	writeJSON(w, http.StatusOK, record)
}

func (s *Service) handleTenantProjects(w http.ResponseWriter, r *http.Request, tenantID string, child string) {
	projectID := ""
	if strings.HasPrefix(child, "projects/") {
		raw := strings.TrimPrefix(child, "projects/")
		if strings.Contains(raw, "/") {
			s.writeError(w, openrtcerr.CodeBadRequest, "unsupported project subresource", "", http.StatusBadRequest)
			return
		}
		decoded, err := url.PathUnescape(raw)
		if err != nil {
			s.writeError(w, openrtcerr.CodeBadRequest, "project id must be URL-escaped", "", http.StatusBadRequest)
			return
		}
		if err := validateProductID("project id", decoded); err != nil {
			s.writeError(w, openrtcerr.CodeBadRequest, err.Error(), "", http.StatusBadRequest)
			return
		}
		projectID = decoded
	}
	if projectID == "" {
		switch r.Method {
		case http.MethodGet:
			s.handleListProjects(w, r, tenantID)
		case http.MethodPost:
			s.handleCreateProject(w, r, tenantID)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.handleGetProject(w, r, tenantID, projectID)
	case http.MethodPatch:
		s.handleUpdateProject(w, r, tenantID, projectID)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Service) handleListProjects(w http.ResponseWriter, r *http.Request, tenantID string) {
	claims, store, ok := s.authenticateProduction(w, r)
	if !ok {
		return
	}
	if !s.allowsTenantAdmin(claims, tenantID) {
		s.writeError(w, openrtcerr.CodeRoomForbidden, "project administration is not permitted", "", http.StatusForbidden)
		return
	}
	projects, err := store.ListProjects(r.Context(), tenantID)
	if err != nil {
		s.writeError(w, openrtcerr.CodeInternal, err.Error(), "", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, listResponse[cluster.ProjectRecord]{Data: projects})
}

func (s *Service) handleCreateProject(w http.ResponseWriter, r *http.Request, tenantID string) {
	claims, store, ok := s.authenticateProduction(w, r)
	if !ok {
		return
	}
	if !s.allowsTenantAdmin(claims, tenantID) {
		s.writeError(w, openrtcerr.CodeRoomForbidden, "project administration is not permitted", "", http.StatusForbidden)
		return
	}
	var request projectRequest
	if err := decodeRequest(w, r, s.cfg.Limits.EnvelopeMaxBytes, &request); err != nil {
		s.writeError(w, openrtcerr.CodeBadRequest, "request body must be valid JSON", "", http.StatusBadRequest)
		return
	}
	if err := validateProductID("project id", request.ID); err != nil {
		s.writeError(w, openrtcerr.CodeBadRequest, err.Error(), "", http.StatusBadRequest)
		return
	}
	if err := validateProductMetadata(request.Metadata, true, s.cfg.Limits.PayloadMaxBytes); err != nil {
		s.writeError(w, err.Code, err.Message, "", openrtcerr.DescriptorFor(err.Code).HTTPStatus)
		return
	}
	record, err := store.CreateProject(r.Context(), cluster.ProjectRecord{
		ID:       request.ID,
		TenantID: tenantID,
		Name:     request.Name,
		Metadata: normalizedMetadata(request.Metadata),
	})
	if errors.Is(err, cluster.ErrTenantNotFound) {
		s.writeError(w, openrtcerr.CodeRoomNotFound, "tenant not found", "", http.StatusNotFound)
		return
	}
	if errors.Is(err, cluster.ErrProjectAlreadyExists) {
		s.writeError(w, openrtcerr.CodeRoomConflict, "project already exists", "", http.StatusConflict)
		return
	}
	if err != nil {
		s.writeError(w, openrtcerr.CodeInternal, err.Error(), "", http.StatusInternalServerError)
		return
	}
	s.recordAuditLog(r.Context(), claims, "project.create", "project", record.ID, tenantID, record.ID, map[string]any{"name": record.Name})
	writeJSON(w, http.StatusCreated, record)
}

func (s *Service) handleGetProject(w http.ResponseWriter, r *http.Request, tenantID string, projectID string) {
	claims, store, ok := s.authenticateProduction(w, r)
	if !ok {
		return
	}
	if !s.allowsTenantAdmin(claims, tenantID) {
		s.writeError(w, openrtcerr.CodeRoomForbidden, "project administration is not permitted", "", http.StatusForbidden)
		return
	}
	record, err := store.GetProject(r.Context(), tenantID, projectID)
	if errors.Is(err, cluster.ErrProjectNotFound) {
		s.writeError(w, openrtcerr.CodeRoomNotFound, "project not found", "", http.StatusNotFound)
		return
	}
	if err != nil {
		s.writeError(w, openrtcerr.CodeInternal, err.Error(), "", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, record)
}

func (s *Service) handleUpdateProject(w http.ResponseWriter, r *http.Request, tenantID string, projectID string) {
	claims, store, ok := s.authenticateProduction(w, r)
	if !ok {
		return
	}
	if !s.allowsTenantAdmin(claims, tenantID) {
		s.writeError(w, openrtcerr.CodeRoomForbidden, "project administration is not permitted", "", http.StatusForbidden)
		return
	}
	var request projectUpdateRequest
	if err := decodeRequest(w, r, s.cfg.Limits.EnvelopeMaxBytes, &request); err != nil {
		s.writeError(w, openrtcerr.CodeBadRequest, "request body must be valid JSON", "", http.StatusBadRequest)
		return
	}
	update := cluster.ProjectUpdate{}
	if request.Name != nil {
		update.Name = strings.TrimSpace(*request.Name)
		update.NameSet = true
	}
	if request.Metadata != nil {
		if err := validateProductMetadata(*request.Metadata, false, s.cfg.Limits.PayloadMaxBytes); err != nil {
			s.writeError(w, err.Code, err.Message, "", openrtcerr.DescriptorFor(err.Code).HTTPStatus)
			return
		}
		update.Metadata = normalizedMetadata(*request.Metadata)
		update.MetadataSet = true
	}
	record, err := store.UpdateProject(r.Context(), tenantID, projectID, update)
	if errors.Is(err, cluster.ErrProjectNotFound) {
		s.writeError(w, openrtcerr.CodeRoomNotFound, "project not found", "", http.StatusNotFound)
		return
	}
	if err != nil {
		s.writeError(w, openrtcerr.CodeInternal, err.Error(), "", http.StatusInternalServerError)
		return
	}
	s.recordAuditLog(r.Context(), claims, "project.update", "project", projectID, tenantID, projectID, nil)
	writeJSON(w, http.StatusOK, record)
}

func (s *Service) handleAPIKeys(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleListAPIKeys(w, r)
	case http.MethodPost:
		s.handleCreateAPIKey(w, r)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Service) handleAPIKeyAction(w http.ResponseWriter, r *http.Request) {
	id, action, err := itemActionParts(r.URL.Path, "/v1/api-keys/")
	if err != nil {
		s.writeError(w, openrtcerr.CodeBadRequest, err.Error(), "", http.StatusBadRequest)
		return
	}
	if action != "revoke" {
		s.writeError(w, openrtcerr.CodeBadRequest, "unsupported API key action", "", http.StatusBadRequest)
		return
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	claims, store, ok := s.authenticateProduction(w, r)
	if !ok {
		return
	}
	current, err := store.GetAPIKey(r.Context(), id)
	if errors.Is(err, cluster.ErrAPIKeyNotFound) {
		s.writeError(w, openrtcerr.CodeRoomNotFound, "api key not found", "", http.StatusNotFound)
		return
	}
	if err != nil {
		s.writeError(w, openrtcerr.CodeInternal, err.Error(), "", http.StatusInternalServerError)
		return
	}
	if !s.allowsTenantAdmin(claims, current.TenantID) {
		s.writeError(w, openrtcerr.CodeRoomForbidden, "api key administration is not permitted", "", http.StatusForbidden)
		return
	}
	record, err := store.RevokeAPIKey(r.Context(), id)
	if err != nil {
		s.writeError(w, openrtcerr.CodeInternal, err.Error(), "", http.StatusInternalServerError)
		return
	}
	s.recordAuditLog(r.Context(), claims, "api_key.revoke", "api_key", id, record.TenantID, record.ProjectID, nil)
	writeJSON(w, http.StatusOK, record)
}

func (s *Service) handleListAPIKeys(w http.ResponseWriter, r *http.Request) {
	claims, store, ok := s.authenticateProduction(w, r)
	if !ok {
		return
	}
	tenantID := r.URL.Query().Get("tenantId")
	projectID := r.URL.Query().Get("projectId")
	if err := validateProductID("tenant id", tenantID); err != nil {
		s.writeError(w, openrtcerr.CodeBadRequest, err.Error(), "", http.StatusBadRequest)
		return
	}
	if err := validateProductID("project id", projectID); err != nil {
		s.writeError(w, openrtcerr.CodeBadRequest, err.Error(), "", http.StatusBadRequest)
		return
	}
	if !s.allowsTenantAdmin(claims, tenantID) {
		s.writeError(w, openrtcerr.CodeRoomForbidden, "api key administration is not permitted", "", http.StatusForbidden)
		return
	}
	keys, err := store.ListAPIKeys(r.Context(), tenantID, projectID)
	if err != nil {
		s.writeError(w, openrtcerr.CodeInternal, err.Error(), "", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, listResponse[cluster.APIKeyRecord]{Data: keys})
}

func (s *Service) handleCreateAPIKey(w http.ResponseWriter, r *http.Request) {
	claims, store, ok := s.authenticateProduction(w, r)
	if !ok {
		return
	}
	var request apiKeyCreateRequest
	if err := decodeRequest(w, r, s.cfg.Limits.EnvelopeMaxBytes, &request); err != nil {
		s.writeError(w, openrtcerr.CodeBadRequest, "request body must be valid JSON", "", http.StatusBadRequest)
		return
	}
	if err := validateProductID("tenant id", request.TenantID); err != nil {
		s.writeError(w, openrtcerr.CodeBadRequest, err.Error(), "", http.StatusBadRequest)
		return
	}
	if err := validateProductID("project id", request.ProjectID); err != nil {
		s.writeError(w, openrtcerr.CodeBadRequest, err.Error(), "", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(request.Name) == "" {
		s.writeError(w, openrtcerr.CodeBadRequest, "api key name is required", "", http.StatusBadRequest)
		return
	}
	if !s.allowsTenantAdmin(claims, request.TenantID) {
		s.writeError(w, openrtcerr.CodeRoomForbidden, "api key administration is not permitted", "", http.StatusForbidden)
		return
	}
	prefix, secret, secretHash := newAPIKeyMaterial()
	record, err := store.CreateAPIKey(r.Context(), cluster.APIKeyRecord{
		ID:         newRecordID("key"),
		TenantID:   request.TenantID,
		ProjectID:  request.ProjectID,
		Name:       strings.TrimSpace(request.Name),
		Prefix:     prefix,
		SecretHash: secretHash,
		Scopes:     request.Scopes,
	})
	if errors.Is(err, cluster.ErrProjectNotFound) {
		s.writeError(w, openrtcerr.CodeRoomNotFound, "project not found", "", http.StatusNotFound)
		return
	}
	if errors.Is(err, cluster.ErrAPIKeyAlreadyExists) {
		s.writeError(w, openrtcerr.CodeRoomConflict, "api key already exists", "", http.StatusConflict)
		return
	}
	if err != nil {
		s.writeError(w, openrtcerr.CodeInternal, err.Error(), "", http.StatusInternalServerError)
		return
	}
	s.recordAuditLog(r.Context(), claims, "api_key.create", "api_key", record.ID, record.TenantID, record.ProjectID, map[string]any{"name": record.Name})
	writeJSON(w, http.StatusCreated, apiKeyCreateResponse{APIKeyRecord: record, Secret: secret})
}

func (s *Service) handleAuditLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	claims, store, ok := s.authenticateProduction(w, r)
	if !ok {
		return
	}
	tenantID := firstNonEmpty(r.URL.Query().Get("tenantId"), claims.Tenant)
	projectID := r.URL.Query().Get("projectId")
	if tenantID != "" && !s.allowsTenantAdmin(claims, tenantID) {
		s.writeError(w, openrtcerr.CodeRoomForbidden, "audit log access is not permitted", "", http.StatusForbidden)
		return
	}
	limit, err := parseProductListLimit(r.URL.Query().Get("limit"))
	if err != nil {
		s.writeError(w, openrtcerr.CodeBadRequest, err.Error(), "", http.StatusBadRequest)
		return
	}
	records, err := store.ListAuditLogs(r.Context(), tenantID, projectID, limit)
	if err != nil {
		s.writeError(w, openrtcerr.CodeInternal, err.Error(), "", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, listResponse[cluster.AuditLogRecord]{Data: records})
}

func (s *Service) handleUsage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	claims, store, ok := s.authenticateProduction(w, r)
	if !ok {
		return
	}
	tenantID := firstNonEmpty(r.URL.Query().Get("tenantId"), claims.Tenant)
	projectID := r.URL.Query().Get("projectId")
	roomID := r.URL.Query().Get("roomId")
	window := r.URL.Query().Get("window")
	if tenantID == "" || projectID == "" {
		s.writeError(w, openrtcerr.CodeBadRequest, "tenantId and projectId are required", "", http.StatusBadRequest)
		return
	}
	if !s.allowsTenantAdmin(claims, tenantID) {
		s.writeError(w, openrtcerr.CodeRoomForbidden, "usage access is not permitted", "", http.StatusForbidden)
		return
	}
	records, err := store.ListUsage(r.Context(), tenantID, projectID, roomID, window)
	if err != nil {
		s.writeError(w, openrtcerr.CodeInternal, err.Error(), "", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, listResponse[cluster.UsageRecord]{Data: records})
}

func (s *Service) handleProductDashboard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	claims, store, ok := s.authenticateProduction(w, r)
	if !ok {
		return
	}
	selection, err := s.productConsoleSelection(r, claims)
	if err != nil {
		if errors.Is(err, errProductConsoleForbidden) {
			s.writeError(w, openrtcerr.CodeRoomForbidden, err.Error(), "", http.StatusForbidden)
			return
		}
		s.writeError(w, openrtcerr.CodeBadRequest, err.Error(), "", http.StatusBadRequest)
		return
	}
	dashboard, err := s.buildProductDashboard(r.Context(), store, selection)
	if errors.Is(err, cluster.ErrTenantNotFound) {
		s.writeError(w, openrtcerr.CodeRoomNotFound, "tenant not found", "", http.StatusNotFound)
		return
	}
	if errors.Is(err, cluster.ErrProjectNotFound) {
		s.writeError(w, openrtcerr.CodeRoomNotFound, "project not found", "", http.StatusNotFound)
		return
	}
	if err != nil {
		s.writeError(w, openrtcerr.CodeInternal, err.Error(), "", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, dashboard)
}

func (s *Service) handleProductStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	claims, store, ok := s.authenticateProduction(w, r)
	if !ok {
		return
	}
	selection, err := s.productConsoleSelection(r, claims)
	if err != nil {
		if errors.Is(err, errProductConsoleForbidden) {
			s.writeError(w, openrtcerr.CodeRoomForbidden, err.Error(), "", http.StatusForbidden)
			return
		}
		s.writeError(w, openrtcerr.CodeBadRequest, err.Error(), "", http.StatusBadRequest)
		return
	}
	status, err := s.buildProductStatus(r.Context(), store, selection)
	if err != nil {
		s.writeError(w, openrtcerr.CodeInternal, err.Error(), "", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (s *Service) handleSupportDebugBundle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	claims, store, ok := s.authenticateProduction(w, r)
	if !ok {
		return
	}
	selection, err := s.productConsoleSelection(r, claims)
	if err != nil {
		if errors.Is(err, errProductConsoleForbidden) {
			s.writeError(w, openrtcerr.CodeRoomForbidden, err.Error(), "", http.StatusForbidden)
			return
		}
		s.writeError(w, openrtcerr.CodeBadRequest, err.Error(), "", http.StatusBadRequest)
		return
	}
	dashboard, err := s.buildProductDashboard(r.Context(), store, selection)
	if err != nil {
		s.writeError(w, openrtcerr.CodeInternal, err.Error(), "", http.StatusInternalServerError)
		return
	}
	status, err := s.buildProductStatus(r.Context(), store, selection)
	if err != nil {
		s.writeError(w, openrtcerr.CodeInternal, err.Error(), "", http.StatusInternalServerError)
		return
	}
	bundle := productSupportDebugBundleResponse{
		GeneratedAt:      time.Now().UTC(),
		TenantID:         selection.TenantID,
		ProjectID:        selection.ProjectID,
		RoomID:           selection.RoomID,
		Environment:      dashboard.Environment,
		Dashboard:        dashboard,
		Status:           status,
		SafeConfig:       s.safeProductConfig(),
		SuggestedActions: productSuggestedActions(status, dashboard),
	}
	writeJSON(w, http.StatusOK, bundle)
}

func (s *Service) handleWebhookDeliveries(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	_, store, ok := s.authenticateProduction(w, r)
	if !ok {
		return
	}
	limit, err := parseProductListLimit(r.URL.Query().Get("limit"))
	if err != nil {
		s.writeError(w, openrtcerr.CodeBadRequest, err.Error(), "", http.StatusBadRequest)
		return
	}
	records, err := store.ListWebhookDeliveries(r.Context(), r.URL.Query().Get("status"), limit)
	if err != nil {
		s.writeError(w, openrtcerr.CodeInternal, err.Error(), "", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, listResponse[cluster.WebhookDeliveryRecord]{Data: records})
}

func (s *Service) handleWebhookDeliveryAction(w http.ResponseWriter, r *http.Request) {
	id, action, err := itemActionParts(r.URL.Path, "/v1/webhook-deliveries/")
	if err != nil {
		s.writeError(w, openrtcerr.CodeBadRequest, err.Error(), "", http.StatusBadRequest)
		return
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	claims, store, ok := s.authenticateProduction(w, r)
	if !ok {
		return
	}
	record, err := store.GetWebhookDelivery(r.Context(), id)
	if errors.Is(err, cluster.ErrWebhookDeliveryNotFound) {
		s.writeError(w, openrtcerr.CodeRoomNotFound, "webhook delivery not found", "", http.StatusNotFound)
		return
	}
	if err != nil {
		s.writeError(w, openrtcerr.CodeInternal, err.Error(), "", http.StatusInternalServerError)
		return
	}
	switch action {
	case "retry":
		updated, err := s.retryWebhookDelivery(r.Context(), store, record)
		if err != nil {
			s.writeError(w, openrtcerr.CodeInternal, err.Error(), "", http.StatusInternalServerError)
			return
		}
		s.recordAuditLog(r.Context(), claims, "webhook_delivery.retry", "webhook_delivery", id, "", "", map[string]any{"event": record.Event})
		writeJSON(w, http.StatusOK, updated)
	case "dlq", "dead-letter":
		record.Status = "dead"
		record.UpdatedAt = time.Now().UTC()
		updated, err := store.UpdateWebhookDelivery(r.Context(), record)
		if err != nil {
			s.writeError(w, openrtcerr.CodeInternal, err.Error(), "", http.StatusInternalServerError)
			return
		}
		s.recordAuditLog(r.Context(), claims, "webhook_delivery.dlq", "webhook_delivery", id, "", "", map[string]any{"event": record.Event})
		writeJSON(w, http.StatusOK, updated)
	default:
		s.writeError(w, openrtcerr.CodeBadRequest, "unsupported webhook delivery action", "", http.StatusBadRequest)
	}
}

func (s *Service) handleResumeSessions(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleListResumeSessions(w, r)
	case http.MethodPost:
		s.handleUpsertResumeSession(w, r)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Service) handleResumeSession(w http.ResponseWriter, r *http.Request) {
	id, err := itemIDFromPath(r.URL.Path, "/v1/resume-sessions/")
	if err != nil {
		s.writeError(w, openrtcerr.CodeBadRequest, err.Error(), "", http.StatusBadRequest)
		return
	}
	claims, store, ok := s.authenticateProduction(w, r)
	if !ok {
		return
	}
	record, err := store.GetResumeSession(r.Context(), id)
	if errors.Is(err, cluster.ErrResumeSessionNotFound) {
		s.writeError(w, openrtcerr.CodeRoomNotFound, "resume session not found", "", http.StatusNotFound)
		return
	}
	if err != nil {
		s.writeError(w, openrtcerr.CodeInternal, err.Error(), "", http.StatusInternalServerError)
		return
	}
	if !s.allowsTenantAdmin(claims, record.TenantID) {
		s.writeError(w, openrtcerr.CodeRoomForbidden, "resume session access is not permitted", "", http.StatusForbidden)
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, record)
	case http.MethodDelete:
		if err := store.DeleteResumeSession(r.Context(), id); err != nil {
			s.writeError(w, openrtcerr.CodeInternal, err.Error(), "", http.StatusInternalServerError)
			return
		}
		s.recordAuditLog(r.Context(), claims, "resume_session.delete", "resume_session", id, record.TenantID, record.ProjectID, nil)
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Service) handleListResumeSessions(w http.ResponseWriter, r *http.Request) {
	claims, store, ok := s.authenticateProduction(w, r)
	if !ok {
		return
	}
	tenantID := firstNonEmpty(r.URL.Query().Get("tenantId"), claims.Tenant)
	projectID := r.URL.Query().Get("projectId")
	if tenantID == "" || projectID == "" {
		s.writeError(w, openrtcerr.CodeBadRequest, "tenantId and projectId are required", "", http.StatusBadRequest)
		return
	}
	if !s.allowsTenantAdmin(claims, tenantID) {
		s.writeError(w, openrtcerr.CodeRoomForbidden, "resume session access is not permitted", "", http.StatusForbidden)
		return
	}
	limit, err := parseProductListLimit(r.URL.Query().Get("limit"))
	if err != nil {
		s.writeError(w, openrtcerr.CodeBadRequest, err.Error(), "", http.StatusBadRequest)
		return
	}
	records, err := store.ListResumeSessions(r.Context(), tenantID, projectID, r.URL.Query().Get("roomId"), r.URL.Query().Get("subject"), r.URL.Query().Get("active") != "false", limit)
	if err != nil {
		s.writeError(w, openrtcerr.CodeInternal, err.Error(), "", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, listResponse[cluster.ResumeSessionRecord]{Data: records})
}

func (s *Service) handleUpsertResumeSession(w http.ResponseWriter, r *http.Request) {
	claims, store, ok := s.authenticateProduction(w, r)
	if !ok {
		return
	}
	var request resumeSessionRequest
	if err := decodeRequest(w, r, s.cfg.Limits.EnvelopeMaxBytes, &request); err != nil {
		s.writeError(w, openrtcerr.CodeBadRequest, "request body must be valid JSON", "", http.StatusBadRequest)
		return
	}
	tenantID := firstNonEmpty(request.TenantID, claims.Tenant)
	if err := validateProductID("tenant id", tenantID); err != nil {
		s.writeError(w, openrtcerr.CodeBadRequest, err.Error(), "", http.StatusBadRequest)
		return
	}
	if err := validateProductID("project id", request.ProjectID); err != nil {
		s.writeError(w, openrtcerr.CodeBadRequest, err.Error(), "", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(request.Subject) == "" {
		s.writeError(w, openrtcerr.CodeBadRequest, "subject is required", "", http.StatusBadRequest)
		return
	}
	if len(request.Rooms) == 0 {
		s.writeError(w, openrtcerr.CodeBadRequest, "at least one room is required", "", http.StatusBadRequest)
		return
	}
	for _, room := range request.Rooms {
		if err := protocol.ValidateRoomName(room); err != nil {
			s.writeError(w, openrtcerr.CodeBadRequest, err.Error(), "", http.StatusBadRequest)
			return
		}
	}
	if !s.allowsTenantAdmin(claims, tenantID) {
		s.writeError(w, openrtcerr.CodeRoomForbidden, "resume session administration is not permitted", "", http.StatusForbidden)
		return
	}
	ttl := 24 * time.Hour
	if request.TTLSeconds > 0 {
		if request.TTLSeconds > 86400*30 {
			s.writeError(w, openrtcerr.CodeBadRequest, "ttlSeconds must be at most 30 days", "", http.StatusBadRequest)
			return
		}
		ttl = time.Duration(request.TTLSeconds) * time.Second
	}
	id := request.ID
	if id == "" {
		id = newRecordID("rs")
	}
	record, err := store.UpsertResumeSession(r.Context(), cluster.ResumeSessionRecord{
		ID:          id,
		TenantID:    tenantID,
		ProjectID:   request.ProjectID,
		Subject:     strings.TrimSpace(request.Subject),
		Rooms:       request.Rooms,
		RoomCursors: request.RoomCursors,
		Metadata:    normalizedMetadata(request.Metadata),
		ExpiresAt:   time.Now().UTC().Add(ttl),
	})
	if err != nil {
		s.writeError(w, openrtcerr.CodeInternal, err.Error(), "", http.StatusInternalServerError)
		return
	}
	s.recordAuditLog(r.Context(), claims, "resume_session.upsert", "resume_session", record.ID, record.TenantID, record.ProjectID, nil)
	writeJSON(w, http.StatusCreated, record)
}

func (s *Service) handleVersionSnapshots(w http.ResponseWriter, r *http.Request, room string) {
	switch r.Method {
	case http.MethodGet:
		s.handleListVersionSnapshots(w, r, room)
	case http.MethodPost:
		s.handleCreateVersionSnapshot(w, r, room)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Service) handleVersionSnapshot(w http.ResponseWriter, r *http.Request, room string, subresource string) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	versionID, err := itemIDFromPath(subresource, "versions/")
	if err != nil {
		s.writeError(w, openrtcerr.CodeBadRequest, err.Error(), "", http.StatusBadRequest)
		return
	}
	claims, store, ok := s.authenticateProduction(w, r)
	if !ok {
		return
	}
	if !s.allowsRoomAction(r.Context(), claims, "storage:read", room) {
		s.writeError(w, openrtcerr.CodeRoomForbidden, "room version history is not permitted", "", http.StatusForbidden)
		return
	}
	documentID := firstNonEmpty(r.URL.Query().Get("documentId"), "storage")
	record, err := store.GetVersionSnapshot(r.Context(), room, documentID, versionID)
	if errors.Is(err, cluster.ErrVersionSnapshotNotFound) {
		s.writeError(w, openrtcerr.CodeRoomNotFound, "version snapshot not found", "", http.StatusNotFound)
		return
	}
	if err != nil {
		s.writeError(w, openrtcerr.CodeInternal, err.Error(), "", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, record)
}

func (s *Service) handleListVersionSnapshots(w http.ResponseWriter, r *http.Request, room string) {
	claims, store, ok := s.authenticateProduction(w, r)
	if !ok {
		return
	}
	if !s.allowsRoomAction(r.Context(), claims, "storage:read", room) {
		s.writeError(w, openrtcerr.CodeRoomForbidden, "room version history is not permitted", "", http.StatusForbidden)
		return
	}
	limit, err := parseProductListLimit(r.URL.Query().Get("limit"))
	if err != nil {
		s.writeError(w, openrtcerr.CodeBadRequest, err.Error(), "", http.StatusBadRequest)
		return
	}
	documentID := firstNonEmpty(r.URL.Query().Get("documentId"), "storage")
	records, err := store.ListVersionSnapshots(r.Context(), room, documentID, limit)
	if err != nil {
		s.writeError(w, openrtcerr.CodeInternal, err.Error(), "", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, listResponse[cluster.VersionSnapshotRecord]{Data: records})
}

func (s *Service) handleCreateVersionSnapshot(w http.ResponseWriter, r *http.Request, room string) {
	claims, store, ok := s.authenticateProduction(w, r)
	if !ok {
		return
	}
	if !s.allowsRoomAction(r.Context(), claims, "storage:write", room) {
		s.writeError(w, openrtcerr.CodeRoomForbidden, "room version history is not permitted", "", http.StatusForbidden)
		return
	}
	var request versionSnapshotRequest
	if err := decodeRequest(w, r, s.cfg.Limits.EnvelopeMaxBytes, &request); err != nil {
		s.writeError(w, openrtcerr.CodeBadRequest, "request body must be valid JSON", "", http.StatusBadRequest)
		return
	}
	document := request.Document
	if len(document) == 0 {
		var err error
		document, err = s.store.GetStorage(r.Context(), room)
		if errors.Is(err, cluster.ErrStorageNotFound) {
			s.writeError(w, openrtcerr.CodeStorageNotFound, "storage document not found", "", http.StatusNotFound)
			return
		}
		if err != nil {
			s.writeError(w, openrtcerr.CodeInternal, err.Error(), "", http.StatusInternalServerError)
			return
		}
	}
	if !json.Valid(document) {
		s.writeError(w, openrtcerr.CodeBadRequest, "document must be valid JSON", "", http.StatusBadRequest)
		return
	}
	id := request.ID
	if id == "" {
		id = newRecordID("ver")
	}
	record, err := store.CreateVersionSnapshot(r.Context(), cluster.VersionSnapshotRecord{
		ID:         id,
		RoomID:     room,
		DocumentID: firstNonEmpty(request.DocumentID, "storage"),
		Label:      strings.TrimSpace(request.Label),
		Document:   document,
		Metadata:   normalizedMetadata(request.Metadata),
	})
	if err != nil {
		s.writeError(w, openrtcerr.CodeInternal, err.Error(), "", http.StatusInternalServerError)
		return
	}
	s.recordUsage(r.Context(), r, room, "version_snapshots.created", 1)
	s.recordAuditLog(r.Context(), claims, "version_snapshot.create", "room", room, tenantFromRoom(room, s.cfg.Tenant.Separator), projectFromRequest(r), map[string]any{"documentId": record.DocumentID})
	writeJSON(w, http.StatusCreated, record)
}

func (s *Service) handleRichTextDocuments(w http.ResponseWriter, r *http.Request, room string, subresource string) {
	if subresource == "rich-text" {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		s.handleListRichTextDocuments(w, r, room)
		return
	}
	documentID, err := itemIDFromPath(subresource, "rich-text/")
	if err != nil {
		s.writeError(w, openrtcerr.CodeBadRequest, err.Error(), "", http.StatusBadRequest)
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.handleGetRichTextDocument(w, r, room, documentID)
	case http.MethodPut:
		s.handleUpsertRichTextDocument(w, r, room, documentID)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Service) handleListRichTextDocuments(w http.ResponseWriter, r *http.Request, room string) {
	claims, store, ok := s.authenticateProduction(w, r)
	if !ok {
		return
	}
	if !s.allowsRoomAction(r.Context(), claims, "storage:read", room) {
		s.writeError(w, openrtcerr.CodeRoomForbidden, "rich-text document access is not permitted", "", http.StatusForbidden)
		return
	}
	records, err := store.ListRichTextDocuments(r.Context(), room)
	if err != nil {
		s.writeError(w, openrtcerr.CodeInternal, err.Error(), "", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, listResponse[cluster.RichTextDocumentRecord]{Data: records})
}

func (s *Service) handleGetRichTextDocument(w http.ResponseWriter, r *http.Request, room string, documentID string) {
	claims, store, ok := s.authenticateProduction(w, r)
	if !ok {
		return
	}
	if !s.allowsRoomAction(r.Context(), claims, "storage:read", room) {
		s.writeError(w, openrtcerr.CodeRoomForbidden, "rich-text document access is not permitted", "", http.StatusForbidden)
		return
	}
	record, err := store.GetRichTextDocument(r.Context(), room, documentID)
	if errors.Is(err, cluster.ErrRichTextDocumentNotFound) {
		s.writeError(w, openrtcerr.CodeRoomNotFound, "rich-text document not found", "", http.StatusNotFound)
		return
	}
	if err != nil {
		s.writeError(w, openrtcerr.CodeInternal, err.Error(), "", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, record)
}

func (s *Service) handleUpsertRichTextDocument(w http.ResponseWriter, r *http.Request, room string, documentID string) {
	claims, store, ok := s.authenticateProduction(w, r)
	if !ok {
		return
	}
	if !s.allowsRoomAction(r.Context(), claims, "storage:write", room) {
		s.writeError(w, openrtcerr.CodeRoomForbidden, "rich-text document access is not permitted", "", http.StatusForbidden)
		return
	}
	var request richTextDocumentRequest
	if err := decodeRequest(w, r, s.cfg.Limits.EnvelopeMaxBytes, &request); err != nil {
		s.writeError(w, openrtcerr.CodeBadRequest, "request body must be valid JSON", "", http.StatusBadRequest)
		return
	}
	if len(request.Content) == 0 || !json.Valid(request.Content) {
		s.writeError(w, openrtcerr.CodeBadRequest, "content must be valid JSON", "", http.StatusBadRequest)
		return
	}
	if err := validateProductMetadata(request.Metadata, true, s.cfg.Limits.PayloadMaxBytes); err != nil {
		s.writeError(w, err.Code, err.Message, "", openrtcerr.DescriptorFor(err.Code).HTTPStatus)
		return
	}
	record, err := store.UpsertRichTextDocument(r.Context(), cluster.RichTextDocumentRecord{
		ID:       documentID,
		RoomID:   room,
		Content:  request.Content,
		Metadata: normalizedMetadata(request.Metadata),
	})
	if err != nil {
		s.writeError(w, openrtcerr.CodeInternal, err.Error(), "", http.StatusInternalServerError)
		return
	}
	_, _ = store.CreateVersionSnapshot(r.Context(), cluster.VersionSnapshotRecord{
		ID:         newRecordID("ver"),
		RoomID:     room,
		DocumentID: "rich:" + documentID,
		Label:      "rich-text.update",
		Document:   request.Content,
	})
	s.recordUsage(r.Context(), r, room, "rich_text_documents.upserted", 1)
	s.recordAuditLog(r.Context(), claims, "rich_text_document.upsert", "room", room, tenantFromRoom(room, s.cfg.Tenant.Separator), projectFromRequest(r), map[string]any{"documentId": documentID})
	writeJSON(w, http.StatusOK, record)
}

func (s *Service) productConsoleSelection(r *http.Request, claims *auth.Claims) (productConsoleSelection, error) {
	tenantID := firstNonEmpty(r.URL.Query().Get("tenantId"), claims.Tenant)
	projectID := r.URL.Query().Get("projectId")
	roomID := r.URL.Query().Get("roomId")
	if err := validateProductID("tenant id", tenantID); err != nil {
		return productConsoleSelection{}, err
	}
	if err := validateProductID("project id", projectID); err != nil {
		return productConsoleSelection{}, err
	}
	if roomID != "" {
		if err := protocol.ValidateRoomName(roomID); err != nil {
			return productConsoleSelection{}, err
		}
	}
	if !s.allowsTenantAdmin(claims, tenantID) {
		return productConsoleSelection{}, errProductConsoleForbidden
	}
	limit, err := parseProductListLimit(r.URL.Query().Get("limit"))
	if err != nil {
		return productConsoleSelection{}, err
	}
	return productConsoleSelection{
		TenantID:  tenantID,
		ProjectID: projectID,
		RoomID:    roomID,
		Limit:     limit,
	}, nil
}

func (s *Service) buildProductDashboard(ctx context.Context, store productionStore, selection productConsoleSelection) (productDashboardResponse, error) {
	tenant, err := store.GetTenant(ctx, selection.TenantID)
	if err != nil {
		return productDashboardResponse{}, err
	}
	project, err := store.GetProject(ctx, selection.TenantID, selection.ProjectID)
	if err != nil {
		return productDashboardResponse{}, err
	}
	apiKeys, err := store.ListAPIKeys(ctx, selection.TenantID, selection.ProjectID)
	if err != nil {
		return productDashboardResponse{}, err
	}
	usage, err := store.ListUsage(ctx, selection.TenantID, selection.ProjectID, selection.RoomID, "")
	if err != nil {
		return productDashboardResponse{}, err
	}
	audits, err := store.ListAuditLogs(ctx, selection.TenantID, selection.ProjectID, selection.Limit)
	if err != nil {
		return productDashboardResponse{}, err
	}
	deliveries, err := store.ListWebhookDeliveries(ctx, "", selection.Limit)
	if err != nil {
		return productDashboardResponse{}, err
	}
	resumeSessions, err := store.ListResumeSessions(ctx, selection.TenantID, selection.ProjectID, selection.RoomID, "", true, selection.Limit)
	if err != nil {
		return productDashboardResponse{}, err
	}

	var rooms []cluster.RoomRecord
	roomID := selection.RoomID
	if s.store != nil {
		roomList, err := s.store.ListRooms(ctx, selection.TenantID+s.cfg.Tenant.Separator, 0, selection.Limit)
		if err != nil {
			return productDashboardResponse{}, err
		}
		rooms = roomList.Rooms
		if roomID == "" && len(rooms) > 0 {
			roomID = rooms[0].ID
		}
		if roomID != "" && !roomInList(rooms, roomID) {
			room, err := s.store.GetRoom(ctx, roomID)
			if err != nil {
				return productDashboardResponse{}, err
			}
			rooms = append([]cluster.RoomRecord{room}, rooms...)
		}
	}

	var activeUsers []cluster.ActiveUser
	var events []cluster.PublishedEvent
	storage := productStorageSnapshot{}
	var richDocs []cluster.RichTextDocumentRecord
	var versions []cluster.VersionSnapshotRecord
	if roomID != "" {
		activeUsers, err = s.store.ActiveUsers(ctx, roomID)
		if err != nil {
			return productDashboardResponse{}, err
		}
		eventList, err := s.store.ListPublishedEvents(ctx, roomID, 0, selection.Limit)
		if err != nil {
			return productDashboardResponse{}, err
		}
		events = eventList.Events
		document, err := s.store.GetStorage(ctx, roomID)
		if errors.Is(err, cluster.ErrStorageNotFound) {
			storage.Found = false
		} else if err != nil {
			return productDashboardResponse{}, err
		} else {
			storage.Found = true
			storage.Document = document
		}
		richDocs, err = store.ListRichTextDocuments(ctx, roomID)
		if err != nil {
			return productDashboardResponse{}, err
		}
		versions, err = store.ListVersionSnapshots(ctx, roomID, "storage", selection.Limit)
		if err != nil {
			return productDashboardResponse{}, err
		}
	}

	statsSnapshot, err := s.store.AggregateStats(ctx)
	if err != nil {
		return productDashboardResponse{}, err
	}
	errorsList := productErrorsFromDeliveries(deliveries)
	environment := productEnvironmentFromProject(project)
	dashboard := productDashboardResponse{
		GeneratedAt:       time.Now().UTC(),
		TenantID:          selection.TenantID,
		ProjectID:         selection.ProjectID,
		RoomID:            roomID,
		Tenant:            tenant,
		Project:           project,
		Environment:       environment,
		APIKeys:           apiKeys,
		Rooms:             rooms,
		ActiveUsers:       activeUsers,
		Events:            events,
		Storage:           storage,
		Stats:             statsSnapshot,
		Usage:             usage,
		AuditLogs:         audits,
		WebhookDeliveries: deliveries,
		ResumeSessions:    resumeSessions,
		RichTextDocuments: richDocs,
		VersionSnapshots:  versions,
		Errors:            errorsList,
		Observability: productDashboardObservability{
			Logs:     audits,
			Errors:   errorsList,
			Usage:    usage,
			Webhooks: deliveries,
		},
	}
	dashboard.Summary = productDashboardSummary{
		Rooms:       len(rooms),
		ActiveUsers: len(activeUsers),
		Events:      len(events),
		StorageDocs: boolInt(storage.Found) + len(richDocs),
		Errors:      len(errorsList),
	}
	return dashboard, nil
}

func (s *Service) buildProductStatus(ctx context.Context, store productionStore, selection productConsoleSelection) (productStatusResponse, error) {
	now := time.Now().UTC()
	checks := []productStatusCheck{{
		Name:      "admin",
		Status:    "ok",
		Message:   "Admin API is serving customer console requests.",
		CheckedAt: now,
	}}
	environment := productEnvironmentSummary{}
	if err := s.store.Healthy(ctx); err != nil {
		checks = append(checks, productStatusCheck{Name: "storage", Status: "error", Message: err.Error(), CheckedAt: now})
	} else {
		checks = append(checks, productStatusCheck{Name: "storage", Status: "ok", Message: "Redis-backed product storage is reachable.", CheckedAt: now})
	}
	tenant, err := store.GetTenant(ctx, selection.TenantID)
	if errors.Is(err, cluster.ErrTenantNotFound) {
		checks = append(checks, productStatusCheck{Name: "tenant", Status: "error", Message: "Tenant record is missing.", CheckedAt: now})
	} else if err != nil {
		checks = append(checks, productStatusCheck{Name: "tenant", Status: "error", Message: err.Error(), CheckedAt: now})
	} else {
		checks = append(checks, productStatusCheck{Name: "tenant", Status: "ok", Message: tenant.Name, CheckedAt: now})
	}
	project, err := store.GetProject(ctx, selection.TenantID, selection.ProjectID)
	if errors.Is(err, cluster.ErrProjectNotFound) {
		checks = append(checks, productStatusCheck{Name: "project", Status: "error", Message: "Project record is missing.", CheckedAt: now})
	} else if err != nil {
		checks = append(checks, productStatusCheck{Name: "project", Status: "error", Message: err.Error(), CheckedAt: now})
	} else {
		environment = productEnvironmentFromProject(project)
		checks = append(checks, productStatusCheck{Name: "project", Status: "ok", Message: project.Name, CheckedAt: now})
	}
	if selection.RoomID != "" {
		if _, err := s.store.GetRoom(ctx, selection.RoomID); errors.Is(err, cluster.ErrRoomNotFound) {
			checks = append(checks, productStatusCheck{Name: "room", Status: "error", Message: "Room record is missing.", CheckedAt: now})
		} else if err != nil {
			checks = append(checks, productStatusCheck{Name: "room", Status: "error", Message: err.Error(), CheckedAt: now})
		} else {
			checks = append(checks, productStatusCheck{Name: "room", Status: "ok", Message: selection.RoomID, CheckedAt: now})
		}
	}
	if _, err := s.store.AggregateStats(ctx); err != nil {
		checks = append(checks, productStatusCheck{Name: "runtime_stats", Status: "degraded", Message: err.Error(), CheckedAt: now})
	} else {
		checks = append(checks, productStatusCheck{Name: "runtime_stats", Status: "ok", Message: "Runtime aggregate stats are available.", CheckedAt: now})
	}
	deliveries, err := store.ListWebhookDeliveries(ctx, "", selection.Limit)
	if err != nil {
		checks = append(checks, productStatusCheck{Name: "webhooks", Status: "degraded", Message: err.Error(), CheckedAt: now})
	} else {
		errorsList := productErrorsFromDeliveries(deliveries)
		if len(errorsList) > 0 {
			checks = append(checks, productStatusCheck{Name: "webhooks", Status: "degraded", Message: "Failed or dead-lettered webhook deliveries need attention.", CheckedAt: now})
		} else {
			checks = append(checks, productStatusCheck{Name: "webhooks", Status: "ok", Message: "No failed webhook deliveries in recent history.", CheckedAt: now})
		}
		status := productStatusFromChecks(checks)
		return productStatusResponse{
			GeneratedAt: now,
			Status:      status,
			TenantID:    selection.TenantID,
			ProjectID:   selection.ProjectID,
			RoomID:      selection.RoomID,
			Environment: environment,
			Checks:      checks,
			Errors:      errorsList,
			PublicPage:  productPublicPage(status, len(errorsList)),
		}, nil
	}
	status := productStatusFromChecks(checks)
	return productStatusResponse{
		GeneratedAt: now,
		Status:      status,
		TenantID:    selection.TenantID,
		ProjectID:   selection.ProjectID,
		RoomID:      selection.RoomID,
		Environment: environment,
		Checks:      checks,
		PublicPage:  productPublicPage(status, 0),
	}, nil
}

func (s *Service) safeProductConfig() productSafeConfig {
	webhookEndpointCount := 0
	if s.cfg.Webhooks != nil {
		webhookEndpointCount = len(s.cfg.Webhooks.URLs)
	}
	return productSafeConfig{
		Mode:                  string(s.cfg.Mode),
		NodeID:                s.cfg.NodeID,
		RedisConfigured:       s.cfg.Redis != nil || s.store != nil,
		RuntimeAuthConfigured: s.cfg.Auth.Issuer != "" && s.cfg.Auth.Audience != "" && s.cfg.Auth.JWKSURL != "",
		AdminAuthConfigured:   s.cfg.AdminAuth != nil,
		WebhooksConfigured:    s.cfg.Webhooks != nil && webhookEndpointCount > 0,
		WebhookEndpointCount:  webhookEndpointCount,
		WebSocketPath:         s.cfg.Server.WSPath,
		TenantEnforcePrefix:   s.cfg.Tenant.EnforcePrefix,
		TenantSeparator:       s.cfg.Tenant.Separator,
		PayloadMaxBytes:       s.cfg.Limits.PayloadMaxBytes,
		EnvelopeMaxBytes:      s.cfg.Limits.EnvelopeMaxBytes,
	}
}

func productEnvironmentFromProject(project cluster.ProjectRecord) productEnvironmentSummary {
	metadata := map[string]any{}
	if len(project.Metadata) > 0 {
		_ = json.Unmarshal(project.Metadata, &metadata)
	}
	return productEnvironmentSummary{
		Environment: metadataString(metadata, "environment"),
		Region:      metadataString(metadata, "region"),
	}
}

func metadataString(metadata map[string]any, key string) string {
	value, ok := metadata[key]
	if !ok {
		return ""
	}
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(text)
}

func productErrorsFromDeliveries(deliveries []cluster.WebhookDeliveryRecord) []productConsoleError {
	errorsList := make([]productConsoleError, 0)
	for _, delivery := range deliveries {
		if delivery.Status != "failed" && delivery.Status != "dead" {
			continue
		}
		message := delivery.LastError
		if message == "" && delivery.LastStatusCode > 0 {
			message = "webhook returned HTTP " + strconv.Itoa(delivery.LastStatusCode)
		}
		if message == "" {
			message = "webhook delivery is " + delivery.Status
		}
		errorsList = append(errorsList, productConsoleError{
			Source:    "webhook",
			Message:   message,
			Status:    delivery.Status,
			Resource:  delivery.ID,
			CreatedAt: delivery.CreatedAt,
			UpdatedAt: delivery.UpdatedAt,
		})
	}
	return errorsList
}

func productStatusFromChecks(checks []productStatusCheck) string {
	status := "ok"
	for _, check := range checks {
		if check.Status == "error" {
			return "error"
		}
		if check.Status == "degraded" {
			status = "degraded"
		}
	}
	return status
}

func productPublicPage(status string, errorCount int) productPublicStatusPage {
	switch status {
	case "ok":
		return productPublicStatusPage{Title: "All systems operational", Message: "Realtime rooms, storage, webhooks, and product records are available."}
	case "degraded":
		return productPublicStatusPage{Title: "Partial service degradation", Message: strconv.Itoa(errorCount) + " recent issue(s) need review."}
	default:
		return productPublicStatusPage{Title: "Service disruption", Message: "One or more required product systems are unavailable."}
	}
}

func productSuggestedActions(status productStatusResponse, dashboard productDashboardResponse) []string {
	actions := make([]string, 0)
	if status.Status != "ok" {
		actions = append(actions, "Review failing status checks and recent customer-visible errors.")
	}
	if dashboard.Summary.Errors > 0 {
		actions = append(actions, "Retry failed webhooks or move permanently failing deliveries to the DLQ.")
	}
	if len(dashboard.APIKeys) == 0 {
		actions = append(actions, "Create a scoped server API key before integrating this workspace.")
	}
	if len(dashboard.Rooms) == 0 {
		actions = append(actions, "Create a first room for the selected workspace.")
	}
	if len(actions) == 0 {
		actions = append(actions, "No immediate support actions are required.")
	}
	return actions
}

func roomInList(rooms []cluster.RoomRecord, roomID string) bool {
	for _, room := range rooms {
		if room.ID == roomID {
			return true
		}
	}
	return false
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func (s *Service) authenticateProduction(w http.ResponseWriter, r *http.Request) (*auth.Claims, productionStore, bool) {
	claims, err := s.authenticate(r)
	if err != nil {
		s.writeError(w, openrtcerr.CodeAuthInvalid, "invalid bearer token", "", http.StatusUnauthorized)
		return nil, nil, false
	}
	store, ok := s.store.(productionStore)
	if !ok {
		s.writeError(w, openrtcerr.CodeInternal, "production product APIs require redis backing", "", http.StatusServiceUnavailable)
		return nil, nil, false
	}
	return claims, store, true
}

func (s *Service) allowsTenantAdmin(claims *auth.Claims, tenantID string) bool {
	if claims == nil {
		return false
	}
	if tenantID == "*" {
		return claims.Allows("admin", claims.Tenant+s.cfg.Tenant.Separator+"__admin", s.cfg.Tenant.EnforcePrefix, s.cfg.Tenant.Separator) ||
			claims.Allows("admin", "__admin", false, s.cfg.Tenant.Separator)
	}
	if s.cfg.Tenant.EnforcePrefix && claims.Tenant != "" && tenantID != claims.Tenant {
		return false
	}
	probeRoom := tenantID + s.cfg.Tenant.Separator + "__admin"
	return claims.Allows("admin", probeRoom, s.cfg.Tenant.EnforcePrefix, s.cfg.Tenant.Separator) ||
		claims.Allows("rooms", probeRoom, s.cfg.Tenant.EnforcePrefix, s.cfg.Tenant.Separator)
}

func (s *Service) recordAuditLog(ctx context.Context, claims *auth.Claims, action string, resourceType string, resourceID string, tenantID string, projectID string, metadata any) {
	store, ok := s.store.(interface {
		RecordAuditLog(context.Context, cluster.AuditLogRecord) (cluster.AuditLogRecord, error)
	})
	if !ok {
		return
	}
	raw := json.RawMessage(`{}`)
	if metadata != nil {
		if encoded, err := json.Marshal(metadata); err == nil && json.Valid(encoded) {
			raw = encoded
		}
	}
	actorID := ""
	if claims != nil {
		actorID = claims.Subject
		if tenantID == "" {
			tenantID = claims.Tenant
		}
	}
	_, err := store.RecordAuditLog(ctx, cluster.AuditLogRecord{
		ID:           newRecordID("aud"),
		TenantID:     tenantID,
		ProjectID:    projectID,
		ActorID:      actorID,
		Action:       action,
		ResourceType: resourceType,
		ResourceID:   resourceID,
		Metadata:     raw,
	})
	if err != nil && s.logger != nil {
		s.logger.Printf("audit log write failed action=%s resource=%s error=%v", action, resourceID, err)
	}
}

func (s *Service) recordUsage(ctx context.Context, r *http.Request, room string, metric string, count int64) {
	store, ok := s.store.(interface {
		IncrementUsage(context.Context, cluster.UsageIncrement) (cluster.UsageRecord, error)
	})
	if !ok {
		return
	}
	tenantID := firstNonEmpty(r.Header.Get("OpenRTC-Tenant-Id"), tenantFromRoom(room, s.cfg.Tenant.Separator))
	projectID := projectFromRequest(r)
	if tenantID == "" || projectID == "" || metric == "" {
		return
	}
	_, err := store.IncrementUsage(ctx, cluster.UsageIncrement{
		TenantID:  tenantID,
		ProjectID: projectID,
		RoomID:    room,
		Metric:    metric,
		Window:    time.Now().UTC().Format("2006-01-02"),
		Count:     count,
	})
	if err != nil && s.logger != nil {
		s.logger.Printf("usage meter write failed metric=%s room=%s error=%v", metric, room, err)
	}
}

func (s *Service) recordVersionSnapshot(ctx context.Context, room string, documentID string, label string, document json.RawMessage) {
	store, ok := s.store.(interface {
		CreateVersionSnapshot(context.Context, cluster.VersionSnapshotRecord) (cluster.VersionSnapshotRecord, error)
	})
	if !ok || len(document) == 0 || !json.Valid(document) {
		return
	}
	_, err := store.CreateVersionSnapshot(ctx, cluster.VersionSnapshotRecord{
		ID:         newRecordID("ver"),
		RoomID:     room,
		DocumentID: firstNonEmpty(documentID, "storage"),
		Label:      label,
		Document:   document,
	})
	if err != nil && s.logger != nil {
		s.logger.Printf("version snapshot write failed room=%s document=%s error=%v", room, documentID, err)
	}
}

func tenantPathParts(path string) (string, string, error) {
	raw := strings.TrimPrefix(path, "/v1/tenants/")
	if raw == "" || raw == path {
		return "", "", errors.New("tenant id is required")
	}
	parts := strings.SplitN(raw, "/", 2)
	tenantID, err := url.PathUnescape(parts[0])
	if err != nil {
		return "", "", errors.New("tenant id must be URL-escaped")
	}
	if err := validateProductID("tenant id", tenantID); err != nil {
		return "", "", err
	}
	child := ""
	if len(parts) == 2 {
		child = parts[1]
	}
	return tenantID, child, nil
}

func itemIDFromPath(path string, prefix string) (string, error) {
	raw := strings.TrimPrefix(path, prefix)
	if raw == "" || raw == path || strings.Contains(raw, "/") {
		return "", errors.New("item id is required")
	}
	id, err := url.PathUnescape(raw)
	if err != nil {
		return "", errors.New("item id must be URL-escaped")
	}
	if strings.TrimSpace(id) == "" || strings.Contains(id, "/") {
		return "", errors.New("item id is invalid")
	}
	return id, nil
}

func itemActionParts(path string, prefix string) (string, string, error) {
	raw := strings.TrimPrefix(path, prefix)
	if raw == "" || raw == path {
		return "", "", errors.New("item id is required")
	}
	parts := strings.SplitN(raw, "/", 2)
	if len(parts) != 2 || parts[1] == "" {
		return "", "", errors.New("item action is required")
	}
	id, err := url.PathUnescape(parts[0])
	if err != nil {
		return "", "", errors.New("item id must be URL-escaped")
	}
	if strings.TrimSpace(id) == "" || strings.Contains(id, "/") {
		return "", "", errors.New("item id is invalid")
	}
	return id, parts[1], nil
}

func validateProductID(label string, id string) error {
	if id == "" {
		return errors.New(label + " is required")
	}
	if len(id) > 128 {
		return errors.New(label + " is too long")
	}
	for _, ch := range id {
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '-' || ch == '_' || ch == '.' {
			continue
		}
		return errors.New(label + " must use letters, numbers, dash, underscore, or dot")
	}
	return nil
}

func validateProductMetadata(raw json.RawMessage, optional bool, maxBytes int) *protocol.ParseError {
	return validateMetadata(raw, optional, maxBytes)
}

func parseProductListLimit(raw string) (int, error) {
	if raw == "" {
		return 100, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil {
		return 0, err
	}
	if limit <= 0 || limit > 200 {
		return 0, errors.New("limit must be between 1 and 200")
	}
	return limit, nil
}

func tenantFromRoom(room string, separator string) string {
	if separator == "" {
		separator = ":"
	}
	before, _, ok := strings.Cut(room, separator)
	if !ok {
		return ""
	}
	return before
}

func projectFromRequest(r *http.Request) string {
	return firstNonEmpty(r.Header.Get("OpenRTC-Project-Id"), r.URL.Query().Get("projectId"))
}

func newAPIKeyMaterial() (string, string, string) {
	raw := make([]byte, 28)
	if _, err := randomRead(raw); err != nil {
		panic(err)
	}
	prefix := "ortc_" + hex.EncodeToString(raw[:4])
	secret := "ortc_sk_" + prefix + "_" + hex.EncodeToString(raw[4:])
	hash := sha256.Sum256([]byte(secret))
	return prefix, secret, "sha256:" + hex.EncodeToString(hash[:])
}
