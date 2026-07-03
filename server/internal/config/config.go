package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
)

type RuntimeMode string

const (
	ModeSingle  RuntimeMode = "single"
	ModeCluster RuntimeMode = "cluster"
	ServerName              = "openrtc-server"
)

type LimitsConfig struct {
	PayloadMaxBytes    int
	EnvelopeMaxBytes   int
	YJSMaxBytes        int
	RoomsPerConnection int
	EmitsPerSecond     int
	OutboundQueueDepth int
}

type WebhooksConfig struct {
	URLs      []string
	Secret    string
	TimeoutMS int
}

type RuntimeConfig struct {
	Mode   RuntimeMode
	NodeID string
	Server struct {
		Host           string
		Port           int
		WSPath         string
		AllowedOrigins []string
	}
	Redis *struct {
		URL           string
		ChannelPrefix string
	}
	Auth struct {
		Issuer   string
		Audience string
		JWKSURL  string
	}
	AdminAuth *struct {
		Issuer   string
		Audience string
		JWKSURL  string
	}
	Tenant struct {
		EnforcePrefix bool
		Separator     string
	}
	Limits   LimitsConfig
	Webhooks *WebhooksConfig
}

type Error struct {
	Message string
}

func (e *Error) Error() string {
	return e.Message
}

func LoadFromOS() (RuntimeConfig, error) {
	return LoadFromMap(mapFromEnviron(os.Environ()))
}

func LoadFromMap(env map[string]string) (RuntimeConfig, error) {
	modeRaw := readString(env, "OPENRTC_MODE")
	if modeRaw == "" {
		modeRaw = string(ModeSingle)
	}
	mode := RuntimeMode(modeRaw)
	if mode != ModeSingle && mode != ModeCluster {
		return RuntimeConfig{}, &Error{Message: "OPENRTC_MODE must be either single or cluster"}
	}

	nodeID, err := requireString(env, "OPENRTC_NODE_ID")
	if err != nil {
		return RuntimeConfig{}, err
	}

	authIssuer, err := requireString(env, "OPENRTC_AUTH_ISSUER")
	if err != nil {
		return RuntimeConfig{}, err
	}
	authAudience, err := requireString(env, "OPENRTC_AUTH_AUDIENCE")
	if err != nil {
		return RuntimeConfig{}, err
	}
	authJWKSURL, err := requireString(env, "OPENRTC_AUTH_JWKS_URL")
	if err != nil {
		return RuntimeConfig{}, err
	}

	separator := readString(env, "OPENRTC_TENANT_SEPARATOR")
	if separator == "" {
		separator = ":"
	}
	if len(separator) != 1 {
		return RuntimeConfig{}, &Error{Message: "OPENRTC_TENANT_SEPARATOR must be a single character"}
	}

	cfg := RuntimeConfig{
		Mode:   mode,
		NodeID: nodeID,
	}
	payloadMaxBytes, err := readInt(env, "OPENRTC_LIMIT_PAYLOAD_MAX_BYTES", 16*1024)
	if err != nil {
		return RuntimeConfig{}, err
	}
	envelopeMaxBytes, err := readInt(env, "OPENRTC_LIMIT_ENVELOPE_MAX_BYTES", 20*1024)
	if err != nil {
		return RuntimeConfig{}, err
	}
	yjsMaxBytes, err := readInt(env, "OPENRTC_LIMIT_YJS_MAX_BYTES", 1024*1024)
	if err != nil {
		return RuntimeConfig{}, err
	}
	roomsPerConnection, err := readInt(env, "OPENRTC_LIMIT_ROOMS_PER_CONNECTION", 50)
	if err != nil {
		return RuntimeConfig{}, err
	}
	emitsPerSecond, err := readInt(env, "OPENRTC_LIMIT_EMITS_PER_SECOND", 100)
	if err != nil {
		return RuntimeConfig{}, err
	}
	outboundQueueDepth, err := readInt(env, "OPENRTC_LIMIT_OUTBOUND_QUEUE_DEPTH", 256)
	if err != nil {
		return RuntimeConfig{}, err
	}
	serverPort, err := readInt(env, "OPENRTC_SERVER_PORT", 8080)
	if err != nil {
		return RuntimeConfig{}, err
	}
	enforcePrefix, err := readBool(env, "OPENRTC_TENANT_ENFORCE_PREFIX", true)
	if err != nil {
		return RuntimeConfig{}, err
	}

	cfg.Limits = LimitsConfig{
		PayloadMaxBytes:    payloadMaxBytes,
		EnvelopeMaxBytes:   envelopeMaxBytes,
		YJSMaxBytes:        yjsMaxBytes,
		RoomsPerConnection: roomsPerConnection,
		EmitsPerSecond:     emitsPerSecond,
		OutboundQueueDepth: outboundQueueDepth,
	}
	cfg.Server.Host = defaultString(readString(env, "OPENRTC_SERVER_HOST"), "0.0.0.0")
	cfg.Server.Port = serverPort
	cfg.Server.WSPath = defaultString(readString(env, "OPENRTC_WS_PATH"), "/ws")
	cfg.Server.AllowedOrigins = readCSV(env, "OPENRTC_ALLOWED_ORIGINS")
	cfg.Auth.Issuer = authIssuer
	cfg.Auth.Audience = authAudience
	cfg.Auth.JWKSURL = authJWKSURL
	cfg.Tenant.EnforcePrefix = enforcePrefix
	cfg.Tenant.Separator = separator

	adminIssuer := readString(env, "OPENRTC_ADMIN_AUTH_ISSUER")
	adminAudience := readString(env, "OPENRTC_ADMIN_AUTH_AUDIENCE")
	adminJWKSURL := readString(env, "OPENRTC_ADMIN_AUTH_JWKS_URL")
	if adminIssuer != "" || adminAudience != "" {
		if adminIssuer == "" || adminAudience == "" {
			return RuntimeConfig{}, &Error{Message: "OPENRTC_ADMIN_AUTH_ISSUER and OPENRTC_ADMIN_AUTH_AUDIENCE must both be set"}
		}
		cfg.AdminAuth = &struct {
			Issuer   string
			Audience string
			JWKSURL  string
		}{
			Issuer:   adminIssuer,
			Audience: adminAudience,
			JWKSURL:  defaultString(adminJWKSURL, authJWKSURL),
		}
	}

	webhookURLs := readCSV(env, "OPENRTC_WEBHOOK_URLS")
	if webhookURL := readString(env, "OPENRTC_WEBHOOK_URL"); webhookURL != "" {
		webhookURLs = append([]string{webhookURL}, webhookURLs...)
	}
	if len(webhookURLs) > 0 {
		webhookSecret := readString(env, "OPENRTC_WEBHOOK_SECRET")
		if webhookSecret == "" {
			return RuntimeConfig{}, &Error{Message: "OPENRTC_WEBHOOK_SECRET is required when webhooks are configured"}
		}
		webhookTimeoutMS, err := readInt(env, "OPENRTC_WEBHOOK_TIMEOUT_MS", 2000)
		if err != nil {
			return RuntimeConfig{}, err
		}
		for _, rawURL := range webhookURLs {
			if !validWebhookURL(rawURL) {
				return RuntimeConfig{}, &Error{Message: "OPENRTC_WEBHOOK_URLS must contain absolute http(s) URLs"}
			}
		}
		cfg.Webhooks = &WebhooksConfig{
			URLs:      webhookURLs,
			Secret:    webhookSecret,
			TimeoutMS: webhookTimeoutMS,
		}
	}

	if cfg.Mode == ModeCluster {
		redisURL := readString(env, "OPENRTC_REDIS_URL")
		if redisURL == "" {
			return RuntimeConfig{}, &Error{Message: "OPENRTC_REDIS_URL is required when OPENRTC_MODE=cluster"}
		}
		cfg.Redis = &struct {
			URL           string
			ChannelPrefix string
		}{
			URL:           redisURL,
			ChannelPrefix: defaultString(readString(env, "OPENRTC_REDIS_CHANNEL_PREFIX"), "room:"),
		}
	}

	if cfg.Limits.EnvelopeMaxBytes < cfg.Limits.PayloadMaxBytes {
		return RuntimeConfig{}, &Error{Message: "OPENRTC_LIMIT_ENVELOPE_MAX_BYTES must be >= OPENRTC_LIMIT_PAYLOAD_MAX_BYTES"}
	}

	return cfg, nil
}

func mapFromEnviron(items []string) map[string]string {
	env := make(map[string]string, len(items))
	for _, item := range items {
		key, value, found := strings.Cut(item, "=")
		if !found {
			continue
		}
		env[key] = value
	}
	return env
}

func requireString(env map[string]string, key string) (string, error) {
	value := readString(env, key)
	if value == "" {
		return "", &Error{Message: fmt.Sprintf("%s is required", key)}
	}
	return value, nil
}

func readString(env map[string]string, key string) string {
	return strings.TrimSpace(env[key])
}

func readCSV(env map[string]string, key string) []string {
	raw := readString(env, key)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		value := strings.TrimSpace(part)
		if value != "" {
			values = append(values, value)
		}
	}
	return values
}

func readInt(env map[string]string, key string, defaultValue int) (int, error) {
	value := readString(env, key)
	if value == "" {
		return defaultValue, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return 0, &Error{Message: fmt.Sprintf("%s must be a positive integer", key)}
	}
	return parsed, nil
}

func readBool(env map[string]string, key string, defaultValue bool) (bool, error) {
	value := strings.ToLower(readString(env, key))
	if value == "" {
		return defaultValue, nil
	}
	switch value {
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, &Error{Message: fmt.Sprintf("%s must be true or false", key)}
	}
}

func defaultString(value string, defaultValue string) string {
	if value == "" {
		return defaultValue
	}
	return value
}

func validWebhookURL(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Host == "" {
		return false
	}
	scheme := strings.ToLower(parsed.Scheme)
	return scheme == "http" || scheme == "https"
}
