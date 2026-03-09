package config

import (
	"fmt"
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
	RoomsPerConnection int
	EmitsPerSecond     int
	OutboundQueueDepth int
}

type RuntimeConfig struct {
	Mode   RuntimeMode
	NodeID string
	Server struct {
		Host   string
		Port   int
		WSPath string
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
	}
	Tenant struct {
		EnforcePrefix bool
		Separator     string
	}
	Limits LimitsConfig
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
		RoomsPerConnection: roomsPerConnection,
		EmitsPerSecond:     emitsPerSecond,
		OutboundQueueDepth: outboundQueueDepth,
	}
	cfg.Server.Host = defaultString(readString(env, "OPENRTC_SERVER_HOST"), "0.0.0.0")
	cfg.Server.Port = serverPort
	cfg.Server.WSPath = defaultString(readString(env, "OPENRTC_WS_PATH"), "/ws")
	cfg.Auth.Issuer = authIssuer
	cfg.Auth.Audience = authAudience
	cfg.Auth.JWKSURL = authJWKSURL
	cfg.Tenant.EnforcePrefix = enforcePrefix
	cfg.Tenant.Separator = separator

	adminIssuer := readString(env, "OPENRTC_ADMIN_AUTH_ISSUER")
	adminAudience := readString(env, "OPENRTC_ADMIN_AUTH_AUDIENCE")
	if adminIssuer != "" || adminAudience != "" {
		if adminIssuer == "" || adminAudience == "" {
			return RuntimeConfig{}, &Error{Message: "OPENRTC_ADMIN_AUTH_ISSUER and OPENRTC_ADMIN_AUTH_AUDIENCE must both be set"}
		}
		cfg.AdminAuth = &struct {
			Issuer   string
			Audience string
		}{
			Issuer:   adminIssuer,
			Audience: adminAudience,
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
