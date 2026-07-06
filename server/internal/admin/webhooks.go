package admin

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/openrtc/openrtc/server/internal/cluster"
)

const (
	webhookUserAgent           = "OpenRTC-Webhooks/1"
	webhookRetryWorkerInterval = 30 * time.Second
)

type webhookEnvelope struct {
	ID        string `json:"id"`
	Event     string `json:"event"`
	CreatedAt string `json:"createdAt"`
	Data      any    `json:"data"`
}

func (s *Service) dispatchWebhook(ctx context.Context, eventName string, data any) {
	if s.cfg.Webhooks == nil || len(s.cfg.Webhooks.URLs) == 0 {
		return
	}

	now := time.Now().UTC()
	webhookID, err := newRecordID("evt")
	if err != nil {
		s.logWebhookDelivery("id", eventName, 0, err)
		return
	}
	envelope := webhookEnvelope{
		ID:        webhookID,
		Event:     eventName,
		CreatedAt: now.Format(time.RFC3339Nano),
		Data:      data,
	}
	body, err := json.Marshal(envelope)
	if err != nil {
		s.logWebhookDelivery("marshal", eventName, 0, err)
		return
	}

	timestamp := strconv.FormatInt(now.Unix(), 10)
	signature := "v1=" + signWebhookPayload(s.cfg.Webhooks.Secret, timestamp, body)
	client := s.webhookClient
	if client == nil {
		client = http.DefaultClient
	}
	deliveryStore, _ := s.store.(interface {
		CreateWebhookDelivery(context.Context, cluster.WebhookDeliveryRecord) (cluster.WebhookDeliveryRecord, error)
		UpdateWebhookDelivery(context.Context, cluster.WebhookDeliveryRecord) (cluster.WebhookDeliveryRecord, error)
	})

	for index, targetURL := range s.cfg.Webhooks.URLs {
		deliveryID, err := newRecordID("wd")
		if err != nil {
			s.logWebhookDelivery("id", eventName, index, err)
			continue
		}
		delivery := cluster.WebhookDeliveryRecord{
			ID:        deliveryID,
			WebhookID: envelope.ID,
			Event:     eventName,
			URL:       targetURL,
			Status:    "pending",
			Payload:   body,
			CreatedAt: now,
			UpdatedAt: now,
		}
		if deliveryStore != nil {
			if stored, err := deliveryStore.CreateWebhookDelivery(ctx, delivery); err == nil {
				delivery = stored
			} else {
				s.logWebhookDelivery("record", eventName, index, err)
			}
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(body))
		if err != nil {
			s.logWebhookDelivery("request", eventName, index, err)
			s.updateWebhookDeliveryRecord(ctx, deliveryStore, delivery, 0, err)
			continue
		}
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("User-Agent", webhookUserAgent)
		request.Header.Set("OpenRTC-Webhook-Id", envelope.ID)
		request.Header.Set("OpenRTC-Webhook-Event", eventName)
		request.Header.Set("OpenRTC-Webhook-Timestamp", timestamp)
		request.Header.Set("OpenRTC-Webhook-Signature", signature)

		response, err := client.Do(request)
		if err != nil {
			s.logWebhookDelivery("send", eventName, index, err)
			s.updateWebhookDeliveryRecord(ctx, deliveryStore, delivery, 0, err)
			continue
		}
		_, _ = io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			s.logWebhookDelivery("status", eventName, index, webhookStatusError(response.StatusCode))
			s.updateWebhookDeliveryRecord(ctx, deliveryStore, delivery, response.StatusCode, webhookStatusError(response.StatusCode))
			continue
		}
		s.updateWebhookDeliveryRecord(ctx, deliveryStore, delivery, response.StatusCode, nil)
	}
}

func (s *Service) retryWebhookDelivery(ctx context.Context, store interface {
	UpdateWebhookDelivery(context.Context, cluster.WebhookDeliveryRecord) (cluster.WebhookDeliveryRecord, error)
}, record cluster.WebhookDeliveryRecord) (cluster.WebhookDeliveryRecord, error) {
	if s.cfg.Webhooks == nil {
		return record, errors.New("webhook retry requires webhook configuration")
	}
	timestamp := strconv.FormatInt(time.Now().UTC().Unix(), 10)
	signature := "v1=" + signWebhookPayload(s.cfg.Webhooks.Secret, timestamp, record.Payload)
	client := s.webhookClient
	if client == nil {
		client = http.DefaultClient
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, record.URL, bytes.NewReader(record.Payload))
	if err != nil {
		return s.updateWebhookDeliveryRecord(ctx, store, record, 0, err), err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", webhookUserAgent)
	request.Header.Set("OpenRTC-Webhook-Id", record.WebhookID)
	request.Header.Set("OpenRTC-Webhook-Event", record.Event)
	request.Header.Set("OpenRTC-Webhook-Timestamp", timestamp)
	request.Header.Set("OpenRTC-Webhook-Signature", signature)

	response, err := client.Do(request)
	if err != nil {
		return s.updateWebhookDeliveryRecord(ctx, store, record, 0, err), nil
	}
	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return s.updateWebhookDeliveryRecord(ctx, store, record, response.StatusCode, webhookStatusError(response.StatusCode)), nil
	}
	return s.updateWebhookDeliveryRecord(ctx, store, record, response.StatusCode, nil), nil
}

func (s *Service) startWebhookRetryWorker() {
	if s.ctx == nil || s.cfg.Webhooks == nil || len(s.cfg.Webhooks.URLs) == 0 || s.store == nil {
		return
	}
	if _, ok := s.store.(interface {
		ListWebhookDeliveries(context.Context, string, int) ([]cluster.WebhookDeliveryRecord, error)
		UpdateWebhookDelivery(context.Context, cluster.WebhookDeliveryRecord) (cluster.WebhookDeliveryRecord, error)
	}); !ok {
		return
	}
	s.webhookWorkerDone = make(chan struct{})
	go func() {
		defer close(s.webhookWorkerDone)
		timer := time.NewTimer(webhookRetryWorkerInterval)
		defer timer.Stop()
		for {
			select {
			case <-s.ctx.Done():
				return
			case <-timer.C:
				s.retryDueWebhookDeliveries(s.ctx, time.Now().UTC(), 100)
				timer.Reset(webhookRetryWorkerInterval)
			}
		}
	}()
}

func (s *Service) retryDueWebhookDeliveries(ctx context.Context, now time.Time, limit int) int {
	store, ok := s.store.(interface {
		ListWebhookDeliveries(context.Context, string, int) ([]cluster.WebhookDeliveryRecord, error)
		UpdateWebhookDelivery(context.Context, cluster.WebhookDeliveryRecord) (cluster.WebhookDeliveryRecord, error)
	})
	if !ok || s.cfg.Webhooks == nil {
		return 0
	}
	records, err := store.ListWebhookDeliveries(ctx, "failed", limit)
	if err != nil {
		if s.logger != nil {
			s.logger.Printf("webhook retry scan failed: %v", err)
		}
		return 0
	}
	retried := 0
	for _, record := range records {
		if record.Status == "dead" {
			continue
		}
		if record.NextAttemptAt != nil && record.NextAttemptAt.After(now) {
			continue
		}
		if _, err := s.retryWebhookDelivery(ctx, store, record); err != nil && s.logger != nil {
			s.logger.Printf("webhook retry failed id=%s event=%s error=%v", record.ID, record.Event, err)
		}
		retried++
	}
	return retried
}

func (s *Service) updateWebhookDeliveryRecord(ctx context.Context, store interface {
	UpdateWebhookDelivery(context.Context, cluster.WebhookDeliveryRecord) (cluster.WebhookDeliveryRecord, error)
}, record cluster.WebhookDeliveryRecord, statusCode int, deliveryErr error) cluster.WebhookDeliveryRecord {
	if store == nil {
		return record
	}
	record.Attempts++
	record.LastStatusCode = statusCode
	record.UpdatedAt = time.Now().UTC()
	if deliveryErr != nil {
		record.Status = "failed"
		record.LastError = deliveryErr.Error()
		next := record.UpdatedAt.Add(webhookRetryDelay(record.Attempts))
		record.NextAttemptAt = &next
		if record.Attempts >= 10 {
			record.Status = "dead"
			record.NextAttemptAt = nil
		}
	} else {
		record.Status = "delivered"
		record.LastError = ""
		record.NextAttemptAt = nil
	}
	updated, err := store.UpdateWebhookDelivery(ctx, record)
	if err != nil {
		s.logWebhookDelivery("record-update", record.Event, 0, err)
		return record
	}
	return updated
}

func webhookRetryDelay(attempts int) time.Duration {
	if attempts < 1 {
		attempts = 1
	}
	if attempts > 6 {
		attempts = 6
	}
	return time.Duration(1<<uint(attempts-1)) * time.Minute
}

func signWebhookPayload(secret string, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(timestamp))
	_, _ = mac.Write([]byte("."))
	_, _ = mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

type webhookStatusError int

func (e webhookStatusError) Error() string {
	return "webhook endpoint returned HTTP " + strconv.Itoa(int(e))
}

func (s *Service) logWebhookDelivery(stage string, eventName string, endpointIndex int, err error) {
	if s.logger == nil || err == nil {
		return
	}
	s.logger.Printf("webhook delivery failed stage=%s event=%s endpoint=%d error=%v", stage, eventName, endpointIndex, err)
}
