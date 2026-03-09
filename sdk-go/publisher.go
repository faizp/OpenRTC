package sdkgo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type PublishRequest struct {
	Room                string `json:"room"`
	Event               string `json:"event"`
	Payload             any    `json:"payload"`
	ExcludeSenderConnID string `json:"exclude_sender_conn_id,omitempty"`
	TraceID             string `json:"trace_id,omitempty"`
}

type Publisher interface {
	Publish(ctx context.Context, req PublishRequest) error
}

type Stats struct {
	ActiveConnections    int64 `json:"active_connections"`
	ActiveRooms          int64 `json:"active_rooms"`
	JoinsTotal           int64 `json:"joins_total"`
	LeavesTotal          int64 `json:"leaves_total"`
	EventsTotal          int64 `json:"events_total"`
	PresenceUpdatesTotal int64 `json:"presence_updates_total"`
	QueueOverflowsTotal  int64 `json:"queue_overflows_total"`
	AdminPublishesTotal  int64 `json:"admin_publishes_total"`
}

type APIError struct {
	Code       string
	Message    string
	RequestID  string
	StatusCode int
}

func (e *APIError) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
	retries    int
}

type Option func(*Client)

func NewClient(baseURL string, token string, options ...Option) *Client {
	client := &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
		},
		retries: 1,
	}
	for _, option := range options {
		option(client)
	}
	return client
}

func WithHTTPClient(httpClient *http.Client) Option {
	return func(client *Client) {
		client.httpClient = httpClient
	}
}

func WithRetries(retries int) Option {
	return func(client *Client) {
		client.retries = retries
	}
}

func (c *Client) Publish(ctx context.Context, req PublishRequest) error {
	body, err := json.Marshal(req)
	if err != nil {
		return err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/publish", bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
	request.Header.Set("Content-Type", "application/json")

	response, err := c.do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusAccepted && response.StatusCode != http.StatusOK {
		return decodeAPIError(response)
	}
	return nil
}

func (c *Client) Stats(ctx context.Context) (Stats, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/v1/stats", nil)
	if err != nil {
		return Stats{}, err
	}
	request.Header.Set("Authorization", "Bearer "+c.token)

	response, err := c.do(request)
	if err != nil {
		return Stats{}, err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return Stats{}, decodeAPIError(response)
	}

	var stats Stats
	if err := json.NewDecoder(response.Body).Decode(&stats); err != nil {
		return Stats{}, err
	}
	return stats, nil
}

func (c *Client) do(request *http.Request) (*http.Response, error) {
	var response *http.Response
	var err error
	for attempt := 0; attempt <= c.retries; attempt++ {
		response, err = c.httpClient.Do(request.Clone(request.Context()))
		if err == nil {
			return response, nil
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
	}
	return nil, err
}

func decodeAPIError(response *http.Response) error {
	var payload struct {
		Code      string `json:"code"`
		Message   string `json:"message"`
		RequestID string `json:"request_id"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return err
	}
	return &APIError{
		Code:       payload.Code,
		Message:    payload.Message,
		RequestID:  payload.RequestID,
		StatusCode: response.StatusCode,
	}
}
