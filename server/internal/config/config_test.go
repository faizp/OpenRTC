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
	env["OPENRTC_ALLOWED_ORIGINS"] = "https://app.example.com, https://admin.example.com"
	env["OPENRTC_ADMIN_AUTH_JWKS_URL"] = "https://admin-issuer.example.com/jwks.json"
	env["OPENRTC_LIMIT_YJS_MAX_BYTES"] = "2048"
	env["OPENRTC_SERVER_HOST"] = "127.0.0.1"
	env["OPENRTC_SERVER_PORT"] = "9000"
	env["OPENRTC_WS_PATH"] = "/rt"
	env["OPENRTC_TENANT_ENFORCE_PREFIX"] = "false"

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
	if len(cfg.Server.AllowedOrigins) != 2 || cfg.Server.AllowedOrigins[0] != "https://app.example.com" {
		t.Fatalf("unexpected allowed origins: %#v", cfg.Server.AllowedOrigins)
	}
	if cfg.Server.Host != "127.0.0.1" || cfg.Server.Port != 9000 || cfg.Server.WSPath != "/rt" {
		t.Fatalf("unexpected server config: %+v", cfg.Server)
	}
	if cfg.AdminAuth == nil || cfg.AdminAuth.JWKSURL != "https://admin-issuer.example.com/jwks.json" {
		t.Fatalf("unexpected admin auth config: %+v", cfg.AdminAuth)
	}
	if cfg.Tenant.EnforcePrefix {
		t.Fatalf("expected tenant prefix enforcement to be disabled")
	}
	if cfg.Limits.YJSMaxBytes != 2048 {
		t.Fatalf("unexpected yjs max bytes: %d", cfg.Limits.YJSMaxBytes)
	}
}

func TestLoadFromMapWebhooks(t *testing.T) {
	env := baseEnv()
	env["OPENRTC_WEBHOOK_URL"] = "https://hooks.example.com/openrtc"
	env["OPENRTC_WEBHOOK_URLS"] = "https://hooks-2.example.com/openrtc, http://localhost:9001/hooks"
	env["OPENRTC_WEBHOOK_SECRET"] = "whsec_test"
	env["OPENRTC_WEBHOOK_TIMEOUT_MS"] = "1500"

	cfg, err := LoadFromMap(env)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.Webhooks == nil {
		t.Fatalf("expected webhook config")
	}
	if cfg.Webhooks.Secret != "whsec_test" || cfg.Webhooks.TimeoutMS != 1500 {
		t.Fatalf("unexpected webhook config: %+v", cfg.Webhooks)
	}
	if len(cfg.Webhooks.URLs) != 3 || cfg.Webhooks.URLs[0] != "https://hooks.example.com/openrtc" || cfg.Webhooks.URLs[2] != "http://localhost:9001/hooks" {
		t.Fatalf("unexpected webhook URLs: %#v", cfg.Webhooks.URLs)
	}
}

func TestLoadFromOS(t *testing.T) {
	for key, value := range baseEnv() {
		t.Setenv(key, value)
	}
	t.Setenv("OPENRTC_ALLOWED_ORIGINS", " https://app.example.com, ,https://admin.example.com ")
	t.Setenv("OPENRTC_TENANT_ENFORCE_PREFIX", "true")

	cfg, err := LoadFromOS()
	if err != nil {
		t.Fatalf("load from os: %v", err)
	}
	if cfg.Mode != ModeSingle || !cfg.Tenant.EnforcePrefix {
		t.Fatalf("unexpected OS config: %+v", cfg)
	}
	if len(cfg.Server.AllowedOrigins) != 2 || cfg.Server.AllowedOrigins[1] != "https://admin.example.com" {
		t.Fatalf("unexpected OS origins: %#v", cfg.Server.AllowedOrigins)
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

func TestLoadFromMapRejectsInvalidEnvironment(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(map[string]string)
		message string
	}{
		{
			name: "bad mode",
			mutate: func(env map[string]string) {
				env["OPENRTC_MODE"] = "invalid"
			},
			message: "OPENRTC_MODE must be either single or cluster",
		},
		{
			name: "missing node id",
			mutate: func(env map[string]string) {
				delete(env, "OPENRTC_NODE_ID")
			},
			message: "OPENRTC_NODE_ID is required",
		},
		{
			name: "missing auth issuer",
			mutate: func(env map[string]string) {
				delete(env, "OPENRTC_AUTH_ISSUER")
			},
			message: "OPENRTC_AUTH_ISSUER is required",
		},
		{
			name: "missing auth audience",
			mutate: func(env map[string]string) {
				delete(env, "OPENRTC_AUTH_AUDIENCE")
			},
			message: "OPENRTC_AUTH_AUDIENCE is required",
		},
		{
			name: "missing auth jwks url",
			mutate: func(env map[string]string) {
				delete(env, "OPENRTC_AUTH_JWKS_URL")
			},
			message: "OPENRTC_AUTH_JWKS_URL is required",
		},
		{
			name: "bad separator",
			mutate: func(env map[string]string) {
				env["OPENRTC_TENANT_SEPARATOR"] = "::"
			},
			message: "OPENRTC_TENANT_SEPARATOR must be a single character",
		},
		{
			name: "invalid bool",
			mutate: func(env map[string]string) {
				env["OPENRTC_TENANT_ENFORCE_PREFIX"] = "maybe"
			},
			message: "OPENRTC_TENANT_ENFORCE_PREFIX must be true or false",
		},
		{
			name: "partial admin auth",
			mutate: func(env map[string]string) {
				env["OPENRTC_ADMIN_AUTH_ISSUER"] = "https://issuer.example.com"
				delete(env, "OPENRTC_ADMIN_AUTH_AUDIENCE")
			},
			message: "OPENRTC_ADMIN_AUTH_ISSUER and OPENRTC_ADMIN_AUTH_AUDIENCE must both be set",
		},
		{
			name: "nonpositive limit",
			mutate: func(env map[string]string) {
				env["OPENRTC_LIMIT_ROOMS_PER_CONNECTION"] = "0"
			},
			message: "OPENRTC_LIMIT_ROOMS_PER_CONNECTION must be a positive integer",
		},
		{
			name: "bad payload limit",
			mutate: func(env map[string]string) {
				env["OPENRTC_LIMIT_PAYLOAD_MAX_BYTES"] = "bad"
			},
			message: "OPENRTC_LIMIT_PAYLOAD_MAX_BYTES must be a positive integer",
		},
		{
			name: "bad envelope limit",
			mutate: func(env map[string]string) {
				env["OPENRTC_LIMIT_ENVELOPE_MAX_BYTES"] = "bad"
			},
			message: "OPENRTC_LIMIT_ENVELOPE_MAX_BYTES must be a positive integer",
		},
		{
			name: "bad yjs limit",
			mutate: func(env map[string]string) {
				env["OPENRTC_LIMIT_YJS_MAX_BYTES"] = "bad"
			},
			message: "OPENRTC_LIMIT_YJS_MAX_BYTES must be a positive integer",
		},
		{
			name: "bad emits limit",
			mutate: func(env map[string]string) {
				env["OPENRTC_LIMIT_EMITS_PER_SECOND"] = "bad"
			},
			message: "OPENRTC_LIMIT_EMITS_PER_SECOND must be a positive integer",
		},
		{
			name: "bad outbound queue depth",
			mutate: func(env map[string]string) {
				env["OPENRTC_LIMIT_OUTBOUND_QUEUE_DEPTH"] = "bad"
			},
			message: "OPENRTC_LIMIT_OUTBOUND_QUEUE_DEPTH must be a positive integer",
		},
		{
			name: "webhook missing secret",
			mutate: func(env map[string]string) {
				env["OPENRTC_WEBHOOK_URL"] = "https://hooks.example.com/openrtc"
			},
			message: "OPENRTC_WEBHOOK_SECRET is required when webhooks are configured",
		},
		{
			name: "webhook bad url",
			mutate: func(env map[string]string) {
				env["OPENRTC_WEBHOOK_URL"] = "ftp://hooks.example.com/openrtc"
				env["OPENRTC_WEBHOOK_SECRET"] = "whsec_test"
			},
			message: "OPENRTC_WEBHOOK_URLS must contain absolute http(s) URLs",
		},
		{
			name: "webhook bad timeout",
			mutate: func(env map[string]string) {
				env["OPENRTC_WEBHOOK_URL"] = "https://hooks.example.com/openrtc"
				env["OPENRTC_WEBHOOK_SECRET"] = "whsec_test"
				env["OPENRTC_WEBHOOK_TIMEOUT_MS"] = "0"
			},
			message: "OPENRTC_WEBHOOK_TIMEOUT_MS must be a positive integer",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			env := baseEnv()
			tc.mutate(env)
			_, err := LoadFromMap(env)
			if err == nil {
				t.Fatalf("expected config error")
			}
			var cfgErr *Error
			if !errors.As(err, &cfgErr) || cfgErr.Message != tc.message {
				t.Fatalf("expected %q, got %v", tc.message, err)
			}
		})
	}
}

func TestMapFromEnvironAndErrorString(t *testing.T) {
	env := mapFromEnviron([]string{"OPENRTC_NODE_ID=node-a", "ignored", "OPENRTC_AUTH_ISSUER=https://issuer.example.com"})
	if env["OPENRTC_NODE_ID"] != "node-a" || env["OPENRTC_AUTH_ISSUER"] != "https://issuer.example.com" {
		t.Fatalf("unexpected env map: %#v", env)
	}
	if _, ok := env["ignored"]; ok {
		t.Fatalf("expected malformed environment item to be ignored")
	}

	err := (&Error{Message: "boom"}).Error()
	if err != "boom" {
		t.Fatalf("unexpected error string: %s", err)
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
