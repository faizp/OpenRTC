package config

import (
	"errors"
	"testing"
)

func TestLoadFromMapSingleModeDefaults(t *testing.T) {
	cfg, err := LoadFromMap(baseEnv())
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if cfg.Mode != ModeSingle {
		t.Fatalf("expected single mode, got %s", cfg.Mode)
	}
	if cfg.Redis != nil {
		t.Fatalf("expected nil redis config in single mode")
	}
	if cfg.Server.Port != 8080 {
		t.Fatalf("unexpected default port: %d", cfg.Server.Port)
	}
}

func TestLoadFromMapRequiresRedisInClusterMode(t *testing.T) {
	env := baseEnv()
	env["OPENRTC_MODE"] = "cluster"

	_, err := LoadFromMap(env)
	if err == nil {
		t.Fatalf("expected error")
	}

	var cfgErr *Error
	if !errors.As(err, &cfgErr) {
		t.Fatalf("expected config error, got %T", err)
	}
	if cfgErr.Message != "OPENRTC_REDIS_URL is required when OPENRTC_MODE=cluster" {
		t.Fatalf("unexpected error: %s", cfgErr.Message)
	}
}

func TestLoadFromMapClusterMode(t *testing.T) {
	env := baseEnv()
	env["OPENRTC_MODE"] = "cluster"
	env["OPENRTC_REDIS_URL"] = "redis://localhost:6379/0"

	cfg, err := LoadFromMap(env)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if cfg.Mode != ModeCluster {
		t.Fatalf("expected cluster mode, got %s", cfg.Mode)
	}
	if cfg.Redis == nil || cfg.Redis.URL != "redis://localhost:6379/0" {
		t.Fatalf("unexpected redis config: %+v", cfg.Redis)
	}
}

func TestLoadFromMapRejectsBadEnvelopeLimit(t *testing.T) {
	env := baseEnv()
	env["OPENRTC_LIMIT_PAYLOAD_MAX_BYTES"] = "200"
	env["OPENRTC_LIMIT_ENVELOPE_MAX_BYTES"] = "100"

	_, err := LoadFromMap(env)
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestLoadFromMapRejectsInvalidIntegers(t *testing.T) {
	env := baseEnv()
	env["OPENRTC_SERVER_PORT"] = "zero"

	_, err := LoadFromMap(env)
	if err == nil {
		t.Fatalf("expected error")
	}
}

func baseEnv() map[string]string {
	return map[string]string{
		"OPENRTC_NODE_ID":             "node-a",
		"OPENRTC_AUTH_ISSUER":         "https://issuer.example.com",
		"OPENRTC_AUTH_AUDIENCE":       "openrtc-clients",
		"OPENRTC_AUTH_JWKS_URL":       "https://issuer.example.com/jwks.json",
		"OPENRTC_ADMIN_AUTH_ISSUER":   "https://issuer.example.com",
		"OPENRTC_ADMIN_AUTH_AUDIENCE": "openrtc-admin",
	}
}
