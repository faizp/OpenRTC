package sdkgo

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestClientPublish(t *testing.T) {
	requestFixture := loadFixture(t, "../testdata/contracts/publish-request.json")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/publish" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if payload["room"] != requestFixture["room"] {
			t.Fatalf("unexpected room: %#v", payload)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	client := NewClient(server.URL, "token")
	if err := client.Publish(context.Background(), PublishRequest{
		Room:    requestFixture["room"].(string),
		Event:   requestFixture["event"].(string),
		Payload: requestFixture["payload"],
	}); err != nil {
		t.Fatalf("publish returned error: %v", err)
	}
}

func TestClientPublishMapsAPIError(t *testing.T) {
	errorFixture := loadFixture(t, "../testdata/contracts/error-response.json")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(errorFixture)
	}))
	defer server.Close()

	client := NewClient(server.URL, "token")
	err := client.Publish(context.Background(), PublishRequest{
		Room:    "tenant-a:room-1",
		Event:   "evt",
		Payload: map[string]any{"ok": true},
	})
	if err == nil {
		t.Fatalf("expected error")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected api error, got %T", err)
	}
	if apiErr.Code != "ROOM_FORBIDDEN" || apiErr.RequestID != "req-123" || apiErr.StatusCode != http.StatusForbidden {
		t.Fatalf("unexpected api error: %+v", apiErr)
	}
}

func loadFixture(t *testing.T, path string) map[string]any {
	t.Helper()
	content, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(content, &payload); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	return payload
}
