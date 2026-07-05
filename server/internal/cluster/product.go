package cluster

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

var (
	ErrTenantNotFound           = errors.New("tenant not found")
	ErrTenantAlreadyExists      = errors.New("tenant already exists")
	ErrProjectNotFound          = errors.New("project not found")
	ErrProjectAlreadyExists     = errors.New("project already exists")
	ErrAPIKeyNotFound           = errors.New("api key not found")
	ErrAPIKeyAlreadyExists      = errors.New("api key already exists")
	ErrWebhookDeliveryNotFound  = errors.New("webhook delivery not found")
	ErrVersionSnapshotNotFound  = errors.New("version snapshot not found")
	ErrResumeSessionNotFound    = errors.New("resume session not found")
	ErrRichTextDocumentNotFound = errors.New("rich text document not found")
)

type TenantRecord struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Metadata  json.RawMessage `json:"metadata"`
	CreatedAt time.Time       `json:"createdAt"`
	UpdatedAt time.Time       `json:"updatedAt"`
}

type TenantUpdate struct {
	Name        string
	NameSet     bool
	Metadata    json.RawMessage
	MetadataSet bool
}

type ProjectRecord struct {
	ID        string          `json:"id"`
	TenantID  string          `json:"tenantId"`
	Name      string          `json:"name"`
	Metadata  json.RawMessage `json:"metadata"`
	CreatedAt time.Time       `json:"createdAt"`
	UpdatedAt time.Time       `json:"updatedAt"`
}

type ProjectUpdate struct {
	Name        string
	NameSet     bool
	Metadata    json.RawMessage
	MetadataSet bool
}

type APIKeyRecord struct {
	ID         string     `json:"id"`
	TenantID   string     `json:"tenantId"`
	ProjectID  string     `json:"projectId"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	SecretHash string     `json:"-"`
	Scopes     []string   `json:"scopes"`
	Revoked    bool       `json:"revoked"`
	CreatedAt  time.Time  `json:"createdAt"`
	UpdatedAt  time.Time  `json:"updatedAt"`
	LastUsedAt *time.Time `json:"lastUsedAt,omitempty"`
}

type AuditLogRecord struct {
	ID           string          `json:"id"`
	TenantID     string          `json:"tenantId,omitempty"`
	ProjectID    string          `json:"projectId,omitempty"`
	ActorID      string          `json:"actorId,omitempty"`
	Action       string          `json:"action"`
	ResourceType string          `json:"resourceType"`
	ResourceID   string          `json:"resourceId"`
	Metadata     json.RawMessage `json:"metadata"`
	CreatedAt    time.Time       `json:"createdAt"`
}

type UsageRecord struct {
	TenantID  string    `json:"tenantId,omitempty"`
	ProjectID string    `json:"projectId,omitempty"`
	RoomID    string    `json:"roomId,omitempty"`
	Metric    string    `json:"metric"`
	Window    string    `json:"window"`
	Count     int64     `json:"count"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type UsageIncrement struct {
	TenantID  string
	ProjectID string
	RoomID    string
	Metric    string
	Window    string
	Count     int64
}

type WebhookDeliveryRecord struct {
	ID             string          `json:"id"`
	WebhookID      string          `json:"webhookId"`
	Event          string          `json:"event"`
	URL            string          `json:"url"`
	Status         string          `json:"status"`
	Attempts       int             `json:"attempts"`
	LastStatusCode int             `json:"lastStatusCode,omitempty"`
	LastError      string          `json:"lastError,omitempty"`
	Payload        json.RawMessage `json:"payload"`
	CreatedAt      time.Time       `json:"createdAt"`
	UpdatedAt      time.Time       `json:"updatedAt"`
	NextAttemptAt  *time.Time      `json:"nextAttemptAt,omitempty"`
}

type VersionSnapshotRecord struct {
	ID         string          `json:"id"`
	RoomID     string          `json:"roomId"`
	DocumentID string          `json:"documentId"`
	Label      string          `json:"label,omitempty"`
	Document   json.RawMessage `json:"document"`
	Metadata   json.RawMessage `json:"metadata"`
	CreatedAt  time.Time       `json:"createdAt"`
}

type RichTextDocumentRecord struct {
	ID        string          `json:"id"`
	RoomID    string          `json:"roomId"`
	Content   json.RawMessage `json:"content"`
	Metadata  json.RawMessage `json:"metadata"`
	CreatedAt time.Time       `json:"createdAt"`
	UpdatedAt time.Time       `json:"updatedAt"`
}

type ResumeSessionRecord struct {
	ID          string            `json:"id"`
	TenantID    string            `json:"tenantId,omitempty"`
	ProjectID   string            `json:"projectId,omitempty"`
	Subject     string            `json:"subject"`
	Rooms       []string          `json:"rooms"`
	RoomCursors map[string]uint64 `json:"roomCursors"`
	Metadata    json.RawMessage   `json:"metadata"`
	CreatedAt   time.Time         `json:"createdAt"`
	UpdatedAt   time.Time         `json:"updatedAt"`
	ExpiresAt   time.Time         `json:"expiresAt"`
}

func (s *RedisStore) CreateTenant(ctx context.Context, tenant TenantRecord) (TenantRecord, error) {
	now := time.Now().UTC()
	tenant.CreatedAt = now
	tenant.UpdatedAt = now
	tenant = normalizeTenantRecord(tenant)
	key := productTenantKey(tenant.ID)
	err := s.client.Watch(ctx, func(tx *redis.Tx) error {
		exists, err := tx.Exists(ctx, key).Result()
		if err != nil {
			return err
		}
		if exists > 0 {
			return ErrTenantAlreadyExists
		}
		_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
			pipe.HSet(ctx, key, encodeTenantRecord(tenant))
			pipe.SAdd(ctx, productTenantIndexKey(), tenant.ID)
			return nil
		})
		return err
	}, key)
	if err != nil {
		return TenantRecord{}, err
	}
	return tenant, nil
}

func (s *RedisStore) GetTenant(ctx context.Context, tenantID string) (TenantRecord, error) {
	return s.loadTenantRecord(ctx, tenantID)
}

func (s *RedisStore) UpdateTenant(ctx context.Context, tenantID string, update TenantUpdate) (TenantRecord, error) {
	key := productTenantKey(tenantID)
	var updated TenantRecord
	err := s.client.Watch(ctx, func(tx *redis.Tx) error {
		values, err := tx.HGetAll(ctx, key).Result()
		if err != nil {
			return err
		}
		if len(values) == 0 {
			return ErrTenantNotFound
		}
		current, err := decodeTenantRecord(values)
		if err != nil {
			return err
		}
		if update.NameSet {
			current.Name = update.Name
		}
		if update.MetadataSet {
			current.Metadata = append(json.RawMessage(nil), update.Metadata...)
		}
		current.UpdatedAt = time.Now().UTC()
		current = normalizeTenantRecord(current)
		_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
			pipe.HSet(ctx, key, encodeTenantRecord(current))
			return nil
		})
		updated = current
		return err
	}, key)
	if err != nil {
		return TenantRecord{}, err
	}
	return updated, nil
}

func (s *RedisStore) ListTenants(ctx context.Context) ([]TenantRecord, error) {
	ids, err := s.client.SMembers(ctx, productTenantIndexKey()).Result()
	if err != nil {
		return nil, err
	}
	tenants := make([]TenantRecord, 0, len(ids))
	for _, id := range ids {
		record, err := s.loadTenantRecord(ctx, id)
		if errors.Is(err, ErrTenantNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		tenants = append(tenants, record)
	}
	sort.Slice(tenants, func(i, j int) bool { return tenants[i].ID < tenants[j].ID })
	return tenants, nil
}

func (s *RedisStore) CreateProject(ctx context.Context, project ProjectRecord) (ProjectRecord, error) {
	now := time.Now().UTC()
	project.CreatedAt = now
	project.UpdatedAt = now
	project = normalizeProjectRecord(project)
	key := productProjectKey(project.TenantID, project.ID)
	err := s.client.Watch(ctx, func(tx *redis.Tx) error {
		exists, err := tx.Exists(ctx, productTenantKey(project.TenantID), key).Result()
		if err != nil {
			return err
		}
		if exists == 0 {
			return ErrTenantNotFound
		}
		if exists == 2 {
			return ErrProjectAlreadyExists
		}
		_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
			pipe.HSet(ctx, key, encodeProjectRecord(project))
			pipe.SAdd(ctx, productProjectIndexKey(project.TenantID), project.ID)
			return nil
		})
		return err
	}, productTenantKey(project.TenantID), key)
	if err != nil {
		return ProjectRecord{}, err
	}
	return project, nil
}

func (s *RedisStore) GetProject(ctx context.Context, tenantID string, projectID string) (ProjectRecord, error) {
	return s.loadProjectRecord(ctx, tenantID, projectID)
}

func (s *RedisStore) UpdateProject(ctx context.Context, tenantID string, projectID string, update ProjectUpdate) (ProjectRecord, error) {
	key := productProjectKey(tenantID, projectID)
	var updated ProjectRecord
	err := s.client.Watch(ctx, func(tx *redis.Tx) error {
		values, err := tx.HGetAll(ctx, key).Result()
		if err != nil {
			return err
		}
		if len(values) == 0 {
			return ErrProjectNotFound
		}
		current, err := decodeProjectRecord(values)
		if err != nil {
			return err
		}
		if update.NameSet {
			current.Name = update.Name
		}
		if update.MetadataSet {
			current.Metadata = append(json.RawMessage(nil), update.Metadata...)
		}
		current.UpdatedAt = time.Now().UTC()
		current = normalizeProjectRecord(current)
		_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
			pipe.HSet(ctx, key, encodeProjectRecord(current))
			return nil
		})
		updated = current
		return err
	}, key)
	if err != nil {
		return ProjectRecord{}, err
	}
	return updated, nil
}

func (s *RedisStore) ListProjects(ctx context.Context, tenantID string) ([]ProjectRecord, error) {
	ids, err := s.client.SMembers(ctx, productProjectIndexKey(tenantID)).Result()
	if err != nil {
		return nil, err
	}
	projects := make([]ProjectRecord, 0, len(ids))
	for _, id := range ids {
		record, err := s.loadProjectRecord(ctx, tenantID, id)
		if errors.Is(err, ErrProjectNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		projects = append(projects, record)
	}
	sort.Slice(projects, func(i, j int) bool { return projects[i].ID < projects[j].ID })
	return projects, nil
}

func (s *RedisStore) CreateAPIKey(ctx context.Context, key APIKeyRecord) (APIKeyRecord, error) {
	now := time.Now().UTC()
	key.CreatedAt = now
	key.UpdatedAt = now
	key.Scopes = normalizeStringList(key.Scopes)
	recordKey := productAPIKeyKey(key.ID)
	err := s.client.Watch(ctx, func(tx *redis.Tx) error {
		exists, err := tx.Exists(ctx, productProjectKey(key.TenantID, key.ProjectID), recordKey).Result()
		if err != nil {
			return err
		}
		if exists == 0 {
			return ErrProjectNotFound
		}
		if exists == 2 {
			return ErrAPIKeyAlreadyExists
		}
		_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
			pipe.HSet(ctx, recordKey, encodeAPIKeyRecord(key))
			pipe.SAdd(ctx, productAPIKeyIndexKey(key.TenantID, key.ProjectID), key.ID)
			pipe.Set(ctx, productAPIKeyPrefixKey(key.Prefix), key.ID, 0)
			return nil
		})
		return err
	}, productProjectKey(key.TenantID, key.ProjectID), recordKey)
	if err != nil {
		return APIKeyRecord{}, err
	}
	return key, nil
}

func (s *RedisStore) GetAPIKey(ctx context.Context, id string) (APIKeyRecord, error) {
	return s.loadAPIKeyRecord(ctx, id)
}

func (s *RedisStore) VerifyAPIKey(ctx context.Context, secret string) (APIKeyRecord, error) {
	prefix, ok := apiKeyPrefixFromSecret(secret)
	if !ok {
		return APIKeyRecord{}, ErrAPIKeyNotFound
	}
	id, err := s.client.Get(ctx, productAPIKeyPrefixKey(prefix)).Result()
	if errors.Is(err, redis.Nil) {
		return APIKeyRecord{}, ErrAPIKeyNotFound
	}
	if err != nil {
		return APIKeyRecord{}, err
	}
	record, err := s.loadAPIKeyRecord(ctx, id)
	if err != nil {
		return APIKeyRecord{}, err
	}
	if record.Revoked {
		return APIKeyRecord{}, ErrAPIKeyNotFound
	}
	hash := apiKeySecretHash(secret)
	if subtle.ConstantTimeCompare([]byte(record.SecretHash), []byte(hash)) != 1 {
		return APIKeyRecord{}, ErrAPIKeyNotFound
	}
	now := time.Now().UTC()
	record.LastUsedAt = &now
	record.UpdatedAt = now
	if err := s.client.HSet(ctx, productAPIKeyKey(record.ID), encodeAPIKeyRecord(record)).Err(); err != nil {
		return APIKeyRecord{}, err
	}
	return record, nil
}

func (s *RedisStore) ListAPIKeys(ctx context.Context, tenantID string, projectID string) ([]APIKeyRecord, error) {
	ids, err := s.client.SMembers(ctx, productAPIKeyIndexKey(tenantID, projectID)).Result()
	if err != nil {
		return nil, err
	}
	keys := make([]APIKeyRecord, 0, len(ids))
	for _, id := range ids {
		record, err := s.loadAPIKeyRecord(ctx, id)
		if errors.Is(err, ErrAPIKeyNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		keys = append(keys, record)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].CreatedAt.After(keys[j].CreatedAt) })
	return keys, nil
}

func (s *RedisStore) RevokeAPIKey(ctx context.Context, id string) (APIKeyRecord, error) {
	key := productAPIKeyKey(id)
	var updated APIKeyRecord
	err := s.client.Watch(ctx, func(tx *redis.Tx) error {
		values, err := tx.HGetAll(ctx, key).Result()
		if err != nil {
			return err
		}
		if len(values) == 0 {
			return ErrAPIKeyNotFound
		}
		current, err := decodeAPIKeyRecord(values)
		if err != nil {
			return err
		}
		current.Revoked = true
		current.UpdatedAt = time.Now().UTC()
		_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
			pipe.HSet(ctx, key, encodeAPIKeyRecord(current))
			return nil
		})
		updated = current
		return err
	}, key)
	if err != nil {
		return APIKeyRecord{}, err
	}
	return updated, nil
}

func (s *RedisStore) RecordAuditLog(ctx context.Context, record AuditLogRecord) (AuditLogRecord, error) {
	record = normalizeAuditLogRecord(record)
	raw, err := json.Marshal(record)
	if err != nil {
		return AuditLogRecord{}, err
	}
	score := float64(record.CreatedAt.UnixNano())
	pipe := s.client.TxPipeline()
	pipe.ZAdd(ctx, productAuditIndexKey(""), redis.Z{Score: score, Member: string(raw)})
	if record.TenantID != "" {
		pipe.ZAdd(ctx, productAuditIndexKey(record.TenantID), redis.Z{Score: score, Member: string(raw)})
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return AuditLogRecord{}, err
	}
	return record, nil
}

func (s *RedisStore) ListAuditLogs(ctx context.Context, tenantID string, projectID string, limit int) ([]AuditLogRecord, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	values, err := s.client.ZRevRange(ctx, productAuditIndexKey(tenantID), 0, int64(limit*2)).Result()
	if err != nil {
		return nil, err
	}
	records := make([]AuditLogRecord, 0, len(values))
	for _, value := range values {
		var record AuditLogRecord
		if err := json.Unmarshal([]byte(value), &record); err != nil {
			continue
		}
		if projectID != "" && record.ProjectID != projectID {
			continue
		}
		records = append(records, record)
		if len(records) >= limit {
			break
		}
	}
	return records, nil
}

func (s *RedisStore) IncrementUsage(ctx context.Context, increment UsageIncrement) (UsageRecord, error) {
	if increment.Count == 0 {
		increment.Count = 1
	}
	if increment.Window == "" {
		increment.Window = time.Now().UTC().Format("2006-01-02")
	}
	key := productUsageKey(increment.TenantID, increment.ProjectID, increment.RoomID, increment.Metric, increment.Window)
	count, err := s.client.IncrBy(ctx, key, increment.Count).Result()
	if err != nil {
		return UsageRecord{}, err
	}
	now := time.Now().UTC()
	record := UsageRecord{
		TenantID:  increment.TenantID,
		ProjectID: increment.ProjectID,
		RoomID:    increment.RoomID,
		Metric:    increment.Metric,
		Window:    increment.Window,
		Count:     count,
		UpdatedAt: now,
	}
	raw, _ := json.Marshal(record)
	pipe := s.client.TxPipeline()
	pipe.Set(ctx, productUsageRecordKey(key), raw, 0)
	pipe.SAdd(ctx, productUsageIndexKey(increment.TenantID, increment.ProjectID), key)
	_, err = pipe.Exec(ctx)
	return record, err
}

func (s *RedisStore) ListUsage(ctx context.Context, tenantID string, projectID string, roomID string, window string) ([]UsageRecord, error) {
	keys, err := s.client.SMembers(ctx, productUsageIndexKey(tenantID, projectID)).Result()
	if err != nil {
		return nil, err
	}
	records := make([]UsageRecord, 0, len(keys))
	for _, key := range keys {
		raw, err := s.client.Get(ctx, productUsageRecordKey(key)).Result()
		if errors.Is(err, redis.Nil) {
			continue
		}
		if err != nil {
			return nil, err
		}
		var record UsageRecord
		if err := json.Unmarshal([]byte(raw), &record); err != nil {
			continue
		}
		if roomID != "" && record.RoomID != roomID {
			continue
		}
		if window != "" && record.Window != window {
			continue
		}
		records = append(records, record)
	}
	sort.Slice(records, func(i, j int) bool {
		if records[i].Window == records[j].Window {
			return records[i].Metric < records[j].Metric
		}
		return records[i].Window > records[j].Window
	})
	return records, nil
}

func (s *RedisStore) CreateWebhookDelivery(ctx context.Context, record WebhookDeliveryRecord) (WebhookDeliveryRecord, error) {
	record = normalizeWebhookDeliveryRecord(record)
	key := productWebhookDeliveryKey(record.ID)
	err := s.client.HSet(ctx, key, encodeWebhookDeliveryRecord(record)).Err()
	if err != nil {
		return WebhookDeliveryRecord{}, err
	}
	if err := s.client.ZAdd(ctx, productWebhookDeliveryIndexKey(), redis.Z{
		Score:  float64(record.CreatedAt.UnixNano()),
		Member: record.ID,
	}).Err(); err != nil {
		return WebhookDeliveryRecord{}, err
	}
	return record, nil
}

func (s *RedisStore) GetWebhookDelivery(ctx context.Context, id string) (WebhookDeliveryRecord, error) {
	return s.loadWebhookDeliveryRecord(ctx, id)
}

func (s *RedisStore) UpdateWebhookDelivery(ctx context.Context, record WebhookDeliveryRecord) (WebhookDeliveryRecord, error) {
	record = normalizeWebhookDeliveryRecord(record)
	key := productWebhookDeliveryKey(record.ID)
	exists, err := s.client.Exists(ctx, key).Result()
	if err != nil {
		return WebhookDeliveryRecord{}, err
	}
	if exists == 0 {
		return WebhookDeliveryRecord{}, ErrWebhookDeliveryNotFound
	}
	if err := s.client.HSet(ctx, key, encodeWebhookDeliveryRecord(record)).Err(); err != nil {
		return WebhookDeliveryRecord{}, err
	}
	return record, nil
}

func (s *RedisStore) ListWebhookDeliveries(ctx context.Context, status string, limit int) ([]WebhookDeliveryRecord, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	ids, err := s.client.ZRevRange(ctx, productWebhookDeliveryIndexKey(), 0, int64(limit*3)).Result()
	if err != nil {
		return nil, err
	}
	records := make([]WebhookDeliveryRecord, 0, len(ids))
	for _, id := range ids {
		record, err := s.loadWebhookDeliveryRecord(ctx, id)
		if errors.Is(err, ErrWebhookDeliveryNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if status != "" && record.Status != status {
			continue
		}
		records = append(records, record)
		if len(records) >= limit {
			break
		}
	}
	return records, nil
}

func (s *RedisStore) CreateVersionSnapshot(ctx context.Context, record VersionSnapshotRecord) (VersionSnapshotRecord, error) {
	record = normalizeVersionSnapshotRecord(record)
	raw, err := json.Marshal(record)
	if err != nil {
		return VersionSnapshotRecord{}, err
	}
	key := productVersionSnapshotKey(record.RoomID, record.DocumentID, record.ID)
	index := productVersionSnapshotIndexKey(record.RoomID, record.DocumentID)
	pipe := s.client.TxPipeline()
	pipe.Set(ctx, key, raw, 0)
	pipe.ZAdd(ctx, index, redis.Z{Score: float64(record.CreatedAt.UnixNano()), Member: record.ID})
	if _, err := pipe.Exec(ctx); err != nil {
		return VersionSnapshotRecord{}, err
	}
	return record, nil
}

func (s *RedisStore) GetVersionSnapshot(ctx context.Context, roomID string, documentID string, id string) (VersionSnapshotRecord, error) {
	raw, err := s.client.Get(ctx, productVersionSnapshotKey(roomID, documentID, id)).Result()
	if errors.Is(err, redis.Nil) {
		return VersionSnapshotRecord{}, ErrVersionSnapshotNotFound
	}
	if err != nil {
		return VersionSnapshotRecord{}, err
	}
	var record VersionSnapshotRecord
	if err := json.Unmarshal([]byte(raw), &record); err != nil {
		return VersionSnapshotRecord{}, err
	}
	return normalizeVersionSnapshotRecord(record), nil
}

func (s *RedisStore) ListVersionSnapshots(ctx context.Context, roomID string, documentID string, limit int) ([]VersionSnapshotRecord, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	ids, err := s.client.ZRevRange(ctx, productVersionSnapshotIndexKey(roomID, documentID), 0, int64(limit-1)).Result()
	if err != nil {
		return nil, err
	}
	records := make([]VersionSnapshotRecord, 0, len(ids))
	for _, id := range ids {
		record, err := s.GetVersionSnapshot(ctx, roomID, documentID, id)
		if errors.Is(err, ErrVersionSnapshotNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, nil
}

func (s *RedisStore) UpsertRichTextDocument(ctx context.Context, record RichTextDocumentRecord) (RichTextDocumentRecord, error) {
	now := time.Now().UTC()
	key := productRichTextDocumentKey(record.RoomID, record.ID)
	values, err := s.client.HGetAll(ctx, key).Result()
	if err != nil {
		return RichTextDocumentRecord{}, err
	}
	if len(values) > 0 {
		current, err := decodeRichTextDocumentRecord(values)
		if err != nil {
			return RichTextDocumentRecord{}, err
		}
		record.CreatedAt = current.CreatedAt
	} else {
		record.CreatedAt = now
	}
	record.UpdatedAt = now
	record = normalizeRichTextDocumentRecord(record)
	pipe := s.client.TxPipeline()
	pipe.HSet(ctx, key, encodeRichTextDocumentRecord(record))
	pipe.SAdd(ctx, productRichTextDocumentIndexKey(record.RoomID), record.ID)
	if _, err := pipe.Exec(ctx); err != nil {
		return RichTextDocumentRecord{}, err
	}
	return record, nil
}

func (s *RedisStore) GetRichTextDocument(ctx context.Context, roomID string, id string) (RichTextDocumentRecord, error) {
	values, err := s.client.HGetAll(ctx, productRichTextDocumentKey(roomID, id)).Result()
	if err != nil {
		return RichTextDocumentRecord{}, err
	}
	if len(values) == 0 {
		return RichTextDocumentRecord{}, ErrRichTextDocumentNotFound
	}
	return decodeRichTextDocumentRecord(values)
}

func (s *RedisStore) ListRichTextDocuments(ctx context.Context, roomID string) ([]RichTextDocumentRecord, error) {
	ids, err := s.client.SMembers(ctx, productRichTextDocumentIndexKey(roomID)).Result()
	if err != nil {
		return nil, err
	}
	records := make([]RichTextDocumentRecord, 0, len(ids))
	for _, id := range ids {
		record, err := s.GetRichTextDocument(ctx, roomID, id)
		if errors.Is(err, ErrRichTextDocumentNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	sort.Slice(records, func(i, j int) bool { return records[i].ID < records[j].ID })
	return records, nil
}

func (s *RedisStore) UpsertResumeSession(ctx context.Context, record ResumeSessionRecord) (ResumeSessionRecord, error) {
	now := time.Now().UTC()
	key := productResumeSessionKey(record.ID)
	values, err := s.client.HGetAll(ctx, key).Result()
	if err != nil {
		return ResumeSessionRecord{}, err
	}
	if len(values) > 0 {
		current, err := decodeResumeSessionRecord(values)
		if err != nil {
			return ResumeSessionRecord{}, err
		}
		record.CreatedAt = current.CreatedAt
	} else {
		record.CreatedAt = now
	}
	record.UpdatedAt = now
	record = normalizeResumeSessionRecord(record)
	pipe := s.client.TxPipeline()
	pipe.HSet(ctx, key, encodeResumeSessionRecord(record))
	pipe.SAdd(ctx, productResumeSessionIndexKey(record.TenantID, record.ProjectID), record.ID)
	for _, room := range record.Rooms {
		pipe.SAdd(ctx, productResumeSessionRoomIndexKey(room), record.ID)
	}
	pipe.ExpireAt(ctx, key, record.ExpiresAt)
	if _, err := pipe.Exec(ctx); err != nil {
		return ResumeSessionRecord{}, err
	}
	return record, nil
}

func (s *RedisStore) GetResumeSession(ctx context.Context, id string) (ResumeSessionRecord, error) {
	return s.loadResumeSessionRecord(ctx, id)
}

func (s *RedisStore) DeleteResumeSession(ctx context.Context, id string) error {
	deleted, err := s.client.Del(ctx, productResumeSessionKey(id)).Result()
	if err != nil {
		return err
	}
	if deleted == 0 {
		return ErrResumeSessionNotFound
	}
	return nil
}

func (s *RedisStore) ListResumeSessions(ctx context.Context, tenantID string, projectID string, roomID string, subject string, activeOnly bool, limit int) ([]ResumeSessionRecord, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	var ids []string
	var err error
	if roomID != "" {
		ids, err = s.client.SMembers(ctx, productResumeSessionRoomIndexKey(roomID)).Result()
	} else {
		ids, err = s.client.SMembers(ctx, productResumeSessionIndexKey(tenantID, projectID)).Result()
	}
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	records := make([]ResumeSessionRecord, 0, len(ids))
	for _, id := range ids {
		record, err := s.loadResumeSessionRecord(ctx, id)
		if errors.Is(err, ErrResumeSessionNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if tenantID != "" && record.TenantID != tenantID {
			continue
		}
		if projectID != "" && record.ProjectID != projectID {
			continue
		}
		if subject != "" && record.Subject != subject {
			continue
		}
		if activeOnly && !record.ExpiresAt.After(now) {
			continue
		}
		records = append(records, record)
		if len(records) >= limit {
			break
		}
	}
	sort.Slice(records, func(i, j int) bool { return records[i].UpdatedAt.After(records[j].UpdatedAt) })
	return records, nil
}

func (s *RedisStore) loadTenantRecord(ctx context.Context, tenantID string) (TenantRecord, error) {
	values, err := s.client.HGetAll(ctx, productTenantKey(tenantID)).Result()
	if err != nil {
		return TenantRecord{}, err
	}
	if len(values) == 0 {
		return TenantRecord{}, ErrTenantNotFound
	}
	return decodeTenantRecord(values)
}

func (s *RedisStore) loadProjectRecord(ctx context.Context, tenantID string, projectID string) (ProjectRecord, error) {
	values, err := s.client.HGetAll(ctx, productProjectKey(tenantID, projectID)).Result()
	if err != nil {
		return ProjectRecord{}, err
	}
	if len(values) == 0 {
		return ProjectRecord{}, ErrProjectNotFound
	}
	return decodeProjectRecord(values)
}

func (s *RedisStore) loadAPIKeyRecord(ctx context.Context, id string) (APIKeyRecord, error) {
	values, err := s.client.HGetAll(ctx, productAPIKeyKey(id)).Result()
	if err != nil {
		return APIKeyRecord{}, err
	}
	if len(values) == 0 {
		return APIKeyRecord{}, ErrAPIKeyNotFound
	}
	return decodeAPIKeyRecord(values)
}

func (s *RedisStore) loadWebhookDeliveryRecord(ctx context.Context, id string) (WebhookDeliveryRecord, error) {
	values, err := s.client.HGetAll(ctx, productWebhookDeliveryKey(id)).Result()
	if err != nil {
		return WebhookDeliveryRecord{}, err
	}
	if len(values) == 0 {
		return WebhookDeliveryRecord{}, ErrWebhookDeliveryNotFound
	}
	return decodeWebhookDeliveryRecord(values)
}

func (s *RedisStore) loadResumeSessionRecord(ctx context.Context, id string) (ResumeSessionRecord, error) {
	values, err := s.client.HGetAll(ctx, productResumeSessionKey(id)).Result()
	if err != nil {
		return ResumeSessionRecord{}, err
	}
	if len(values) == 0 {
		return ResumeSessionRecord{}, ErrResumeSessionNotFound
	}
	return decodeResumeSessionRecord(values)
}

func encodeTenantRecord(record TenantRecord) map[string]any {
	record = normalizeTenantRecord(record)
	return map[string]any{
		"id":         record.ID,
		"name":       record.Name,
		"metadata":   string(record.Metadata),
		"created_at": record.CreatedAt.UTC().Format(time.RFC3339Nano),
		"updated_at": record.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func decodeTenantRecord(values map[string]string) (TenantRecord, error) {
	createdAt, err := time.Parse(time.RFC3339Nano, values["created_at"])
	if err != nil {
		return TenantRecord{}, err
	}
	updatedAt, err := time.Parse(time.RFC3339Nano, values["updated_at"])
	if err != nil {
		return TenantRecord{}, err
	}
	return normalizeTenantRecord(TenantRecord{
		ID:        values["id"],
		Name:      values["name"],
		Metadata:  json.RawMessage(defaultString(values["metadata"], "{}")),
		CreatedAt: createdAt.UTC(),
		UpdatedAt: updatedAt.UTC(),
	}), nil
}

func encodeProjectRecord(record ProjectRecord) map[string]any {
	record = normalizeProjectRecord(record)
	return map[string]any{
		"id":         record.ID,
		"tenant_id":  record.TenantID,
		"name":       record.Name,
		"metadata":   string(record.Metadata),
		"created_at": record.CreatedAt.UTC().Format(time.RFC3339Nano),
		"updated_at": record.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func decodeProjectRecord(values map[string]string) (ProjectRecord, error) {
	createdAt, err := time.Parse(time.RFC3339Nano, values["created_at"])
	if err != nil {
		return ProjectRecord{}, err
	}
	updatedAt, err := time.Parse(time.RFC3339Nano, values["updated_at"])
	if err != nil {
		return ProjectRecord{}, err
	}
	return normalizeProjectRecord(ProjectRecord{
		ID:        values["id"],
		TenantID:  values["tenant_id"],
		Name:      values["name"],
		Metadata:  json.RawMessage(defaultString(values["metadata"], "{}")),
		CreatedAt: createdAt.UTC(),
		UpdatedAt: updatedAt.UTC(),
	}), nil
}

func encodeAPIKeyRecord(record APIKeyRecord) map[string]any {
	scopes, _ := json.Marshal(normalizeStringList(record.Scopes))
	lastUsedAt := ""
	if record.LastUsedAt != nil {
		lastUsedAt = record.LastUsedAt.UTC().Format(time.RFC3339Nano)
	}
	return map[string]any{
		"id":           record.ID,
		"tenant_id":    record.TenantID,
		"project_id":   record.ProjectID,
		"name":         record.Name,
		"prefix":       record.Prefix,
		"secret_hash":  record.SecretHash,
		"scopes":       string(scopes),
		"revoked":      strconv.FormatBool(record.Revoked),
		"created_at":   record.CreatedAt.UTC().Format(time.RFC3339Nano),
		"updated_at":   record.UpdatedAt.UTC().Format(time.RFC3339Nano),
		"last_used_at": lastUsedAt,
	}
}

func decodeAPIKeyRecord(values map[string]string) (APIKeyRecord, error) {
	createdAt, err := time.Parse(time.RFC3339Nano, values["created_at"])
	if err != nil {
		return APIKeyRecord{}, err
	}
	updatedAt, err := time.Parse(time.RFC3339Nano, values["updated_at"])
	if err != nil {
		return APIKeyRecord{}, err
	}
	revoked, err := strconv.ParseBool(defaultString(values["revoked"], "false"))
	if err != nil {
		return APIKeyRecord{}, err
	}
	var scopes []string
	if raw := values["scopes"]; raw != "" {
		if err := json.Unmarshal([]byte(raw), &scopes); err != nil {
			return APIKeyRecord{}, err
		}
	}
	var lastUsedAt *time.Time
	if raw := values["last_used_at"]; raw != "" {
		parsed, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			return APIKeyRecord{}, err
		}
		parsed = parsed.UTC()
		lastUsedAt = &parsed
	}
	return APIKeyRecord{
		ID:         values["id"],
		TenantID:   values["tenant_id"],
		ProjectID:  values["project_id"],
		Name:       values["name"],
		Prefix:     values["prefix"],
		SecretHash: values["secret_hash"],
		Scopes:     normalizeStringList(scopes),
		Revoked:    revoked,
		CreatedAt:  createdAt.UTC(),
		UpdatedAt:  updatedAt.UTC(),
		LastUsedAt: lastUsedAt,
	}, nil
}

func encodeWebhookDeliveryRecord(record WebhookDeliveryRecord) map[string]any {
	record = normalizeWebhookDeliveryRecord(record)
	nextAttemptAt := ""
	if record.NextAttemptAt != nil {
		nextAttemptAt = record.NextAttemptAt.UTC().Format(time.RFC3339Nano)
	}
	return map[string]any{
		"id":               record.ID,
		"webhook_id":       record.WebhookID,
		"event":            record.Event,
		"url":              record.URL,
		"status":           record.Status,
		"attempts":         strconv.Itoa(record.Attempts),
		"last_status_code": strconv.Itoa(record.LastStatusCode),
		"last_error":       record.LastError,
		"payload":          string(record.Payload),
		"created_at":       record.CreatedAt.UTC().Format(time.RFC3339Nano),
		"updated_at":       record.UpdatedAt.UTC().Format(time.RFC3339Nano),
		"next_attempt_at":  nextAttemptAt,
	}
}

func decodeWebhookDeliveryRecord(values map[string]string) (WebhookDeliveryRecord, error) {
	createdAt, err := time.Parse(time.RFC3339Nano, values["created_at"])
	if err != nil {
		return WebhookDeliveryRecord{}, err
	}
	updatedAt, err := time.Parse(time.RFC3339Nano, values["updated_at"])
	if err != nil {
		return WebhookDeliveryRecord{}, err
	}
	attempts, err := strconv.Atoi(defaultString(values["attempts"], "0"))
	if err != nil {
		return WebhookDeliveryRecord{}, err
	}
	statusCode, err := strconv.Atoi(defaultString(values["last_status_code"], "0"))
	if err != nil {
		return WebhookDeliveryRecord{}, err
	}
	var nextAttemptAt *time.Time
	if raw := values["next_attempt_at"]; raw != "" {
		parsed, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			return WebhookDeliveryRecord{}, err
		}
		parsed = parsed.UTC()
		nextAttemptAt = &parsed
	}
	return normalizeWebhookDeliveryRecord(WebhookDeliveryRecord{
		ID:             values["id"],
		WebhookID:      values["webhook_id"],
		Event:          values["event"],
		URL:            values["url"],
		Status:         values["status"],
		Attempts:       attempts,
		LastStatusCode: statusCode,
		LastError:      values["last_error"],
		Payload:        json.RawMessage(defaultString(values["payload"], "{}")),
		CreatedAt:      createdAt.UTC(),
		UpdatedAt:      updatedAt.UTC(),
		NextAttemptAt:  nextAttemptAt,
	}), nil
}

func encodeRichTextDocumentRecord(record RichTextDocumentRecord) map[string]any {
	record = normalizeRichTextDocumentRecord(record)
	return map[string]any{
		"id":         record.ID,
		"room_id":    record.RoomID,
		"content":    string(record.Content),
		"metadata":   string(record.Metadata),
		"created_at": record.CreatedAt.UTC().Format(time.RFC3339Nano),
		"updated_at": record.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func decodeRichTextDocumentRecord(values map[string]string) (RichTextDocumentRecord, error) {
	createdAt, err := time.Parse(time.RFC3339Nano, values["created_at"])
	if err != nil {
		return RichTextDocumentRecord{}, err
	}
	updatedAt, err := time.Parse(time.RFC3339Nano, values["updated_at"])
	if err != nil {
		return RichTextDocumentRecord{}, err
	}
	return normalizeRichTextDocumentRecord(RichTextDocumentRecord{
		ID:        values["id"],
		RoomID:    values["room_id"],
		Content:   json.RawMessage(defaultString(values["content"], "{}")),
		Metadata:  json.RawMessage(defaultString(values["metadata"], "{}")),
		CreatedAt: createdAt.UTC(),
		UpdatedAt: updatedAt.UTC(),
	}), nil
}

func encodeResumeSessionRecord(record ResumeSessionRecord) map[string]any {
	record = normalizeResumeSessionRecord(record)
	rooms, _ := json.Marshal(record.Rooms)
	cursors, _ := json.Marshal(record.RoomCursors)
	return map[string]any{
		"id":           record.ID,
		"tenant_id":    record.TenantID,
		"project_id":   record.ProjectID,
		"subject":      record.Subject,
		"rooms":        string(rooms),
		"room_cursors": string(cursors),
		"metadata":     string(record.Metadata),
		"created_at":   record.CreatedAt.UTC().Format(time.RFC3339Nano),
		"updated_at":   record.UpdatedAt.UTC().Format(time.RFC3339Nano),
		"expires_at":   record.ExpiresAt.UTC().Format(time.RFC3339Nano),
	}
}

func decodeResumeSessionRecord(values map[string]string) (ResumeSessionRecord, error) {
	createdAt, err := time.Parse(time.RFC3339Nano, values["created_at"])
	if err != nil {
		return ResumeSessionRecord{}, err
	}
	updatedAt, err := time.Parse(time.RFC3339Nano, values["updated_at"])
	if err != nil {
		return ResumeSessionRecord{}, err
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, values["expires_at"])
	if err != nil {
		return ResumeSessionRecord{}, err
	}
	var rooms []string
	if raw := values["rooms"]; raw != "" {
		if err := json.Unmarshal([]byte(raw), &rooms); err != nil {
			return ResumeSessionRecord{}, err
		}
	}
	cursors := map[string]uint64{}
	if raw := values["room_cursors"]; raw != "" {
		if err := json.Unmarshal([]byte(raw), &cursors); err != nil {
			return ResumeSessionRecord{}, err
		}
	}
	return normalizeResumeSessionRecord(ResumeSessionRecord{
		ID:          values["id"],
		TenantID:    values["tenant_id"],
		ProjectID:   values["project_id"],
		Subject:     values["subject"],
		Rooms:       rooms,
		RoomCursors: cursors,
		Metadata:    json.RawMessage(defaultString(values["metadata"], "{}")),
		CreatedAt:   createdAt.UTC(),
		UpdatedAt:   updatedAt.UTC(),
		ExpiresAt:   expiresAt.UTC(),
	}), nil
}

func normalizeTenantRecord(record TenantRecord) TenantRecord {
	record.ID = strings.TrimSpace(record.ID)
	record.Name = strings.TrimSpace(record.Name)
	if record.Name == "" {
		record.Name = record.ID
	}
	record.Metadata = normalizedJSON(record.Metadata)
	record.CreatedAt = record.CreatedAt.UTC()
	record.UpdatedAt = record.UpdatedAt.UTC()
	return record
}

func normalizeProjectRecord(record ProjectRecord) ProjectRecord {
	record.ID = strings.TrimSpace(record.ID)
	record.TenantID = strings.TrimSpace(record.TenantID)
	record.Name = strings.TrimSpace(record.Name)
	if record.Name == "" {
		record.Name = record.ID
	}
	record.Metadata = normalizedJSON(record.Metadata)
	record.CreatedAt = record.CreatedAt.UTC()
	record.UpdatedAt = record.UpdatedAt.UTC()
	return record
}

func normalizeAuditLogRecord(record AuditLogRecord) AuditLogRecord {
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now().UTC()
	}
	record.Metadata = normalizedJSON(record.Metadata)
	return record
}

func normalizeWebhookDeliveryRecord(record WebhookDeliveryRecord) WebhookDeliveryRecord {
	if record.Status == "" {
		record.Status = "pending"
	}
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now().UTC()
	}
	if record.UpdatedAt.IsZero() {
		record.UpdatedAt = record.CreatedAt
	}
	if len(record.Payload) == 0 {
		record.Payload = json.RawMessage(`{}`)
	}
	record.Payload = append(json.RawMessage(nil), record.Payload...)
	return record
}

func normalizeVersionSnapshotRecord(record VersionSnapshotRecord) VersionSnapshotRecord {
	if record.DocumentID == "" {
		record.DocumentID = "storage"
	}
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now().UTC()
	}
	record.Document = normalizedJSON(record.Document)
	record.Metadata = normalizedJSON(record.Metadata)
	return record
}

func normalizeRichTextDocumentRecord(record RichTextDocumentRecord) RichTextDocumentRecord {
	record.Content = normalizedJSON(record.Content)
	record.Metadata = normalizedJSON(record.Metadata)
	record.CreatedAt = record.CreatedAt.UTC()
	record.UpdatedAt = record.UpdatedAt.UTC()
	return record
}

func normalizeResumeSessionRecord(record ResumeSessionRecord) ResumeSessionRecord {
	record.Rooms = normalizeStringList(record.Rooms)
	if record.RoomCursors == nil {
		record.RoomCursors = map[string]uint64{}
	}
	record.Metadata = normalizedJSON(record.Metadata)
	if record.ExpiresAt.IsZero() {
		record.ExpiresAt = time.Now().UTC().Add(24 * time.Hour)
	}
	record.CreatedAt = record.CreatedAt.UTC()
	record.UpdatedAt = record.UpdatedAt.UTC()
	record.ExpiresAt = record.ExpiresAt.UTC()
	return record
}

func normalizedJSON(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage(`{}`)
	}
	return append(json.RawMessage(nil), raw...)
}

func apiKeyPrefixFromSecret(secret string) (string, bool) {
	const secretPrefix = "ortc_sk_"
	const apiKeyPrefixLen = len("ortc_") + 8
	if !strings.HasPrefix(secret, secretPrefix) {
		return "", false
	}
	remainder := strings.TrimPrefix(secret, secretPrefix)
	if len(remainder) <= apiKeyPrefixLen || remainder[apiKeyPrefixLen] != '_' {
		return "", false
	}
	prefix := remainder[:apiKeyPrefixLen]
	if !strings.HasPrefix(prefix, "ortc_") {
		return "", false
	}
	return prefix, true
}

func apiKeySecretHash(secret string) string {
	hash := sha256.Sum256([]byte(secret))
	return "sha256:" + hex.EncodeToString(hash[:])
}

func productTenantIndexKey() string {
	return "product:tenants"
}

func productTenantKey(id string) string {
	return "product:tenant:" + id
}

func productProjectIndexKey(tenantID string) string {
	return "product:tenant:" + tenantID + ":projects"
}

func productProjectKey(tenantID string, projectID string) string {
	return "product:tenant:" + tenantID + ":project:" + projectID
}

func productAPIKeyIndexKey(tenantID string, projectID string) string {
	return "product:tenant:" + tenantID + ":project:" + projectID + ":api_keys"
}

func productAPIKeyKey(id string) string {
	return "product:api_key:" + id
}

func productAPIKeyPrefixKey(prefix string) string {
	return "product:api_key_prefix:" + prefix
}

func productAuditIndexKey(tenantID string) string {
	if tenantID == "" {
		return "product:audit"
	}
	return "product:tenant:" + tenantID + ":audit"
}

func productUsageIndexKey(tenantID string, projectID string) string {
	return "product:tenant:" + tenantID + ":project:" + projectID + ":usage"
}

func productUsageKey(tenantID string, projectID string, roomID string, metric string, window string) string {
	return strings.Join([]string{tenantID, projectID, roomID, metric, window}, "\x00")
}

func productUsageRecordKey(key string) string {
	return "product:usage:" + key
}

func productWebhookDeliveryIndexKey() string {
	return "product:webhook_deliveries"
}

func productWebhookDeliveryKey(id string) string {
	return "product:webhook_delivery:" + id
}

func productVersionSnapshotIndexKey(roomID string, documentID string) string {
	return "product:room:" + roomID + ":versions:" + documentID
}

func productVersionSnapshotKey(roomID string, documentID string, id string) string {
	return productVersionSnapshotIndexKey(roomID, documentID) + ":" + id
}

func productRichTextDocumentIndexKey(roomID string) string {
	return "product:room:" + roomID + ":rich_text"
}

func productRichTextDocumentKey(roomID string, id string) string {
	return productRichTextDocumentIndexKey(roomID) + ":" + id
}

func productResumeSessionIndexKey(tenantID string, projectID string) string {
	return "product:tenant:" + tenantID + ":project:" + projectID + ":resume_sessions"
}

func productResumeSessionRoomIndexKey(roomID string) string {
	return "product:room:" + roomID + ":resume_sessions"
}

func productResumeSessionKey(id string) string {
	return "product:resume_session:" + id
}
