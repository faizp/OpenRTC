package admin

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"time"
)

const (
	webhookUserAgent = "OpenRTC-Webhooks/1"
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
	envelope := webhookEnvelope{
		ID:        newRecordID("evt"),
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

	for index, targetURL := range s.cfg.Webhooks.URLs {
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(body))
		if err != nil {
			s.logWebhookDelivery("request", eventName, index, err)
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
			continue
		}
		_, _ = io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			s.logWebhookDelivery("status", eventName, index, webhookStatusError(response.StatusCode))
		}
	}
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
