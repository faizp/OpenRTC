package main

import (
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
	"testing"

	"github.com/openrtc/openrtc/server/internal/config"
)

func TestRunStartsAdminServerAndClosesService(t *testing.T) {
	service := &stubAdminService{handler: http.NewServeMux()}
	cfg := adminCommandTestConfig()

	err := run(context.Background(),
		func() (config.RuntimeConfig, error) {
			return cfg, nil
		},
		func(got config.RuntimeConfig, logger *log.Logger) (adminService, error) {
			if got.Server.Host != cfg.Server.Host || got.Server.Port != cfg.Server.Port {
				t.Fatalf("unexpected config passed to service: %+v", got.Server)
			}
			if logger == nil {
				t.Fatalf("expected logger")
			}
			return service, nil
		},
		func(server *http.Server) error {
			if server.Addr != "127.0.0.1:19001" {
				t.Fatalf("unexpected server addr: %s", server.Addr)
			}
			if server.Handler == nil {
				t.Fatalf("expected handler")
			}
			return http.ErrServerClosed
		},
		io.Discard,
	)
	if err != nil {
		t.Fatalf("run admin server: %v", err)
	}
	if !service.closed {
		t.Fatalf("expected service close")
	}
}

func TestRunAdminServerFailures(t *testing.T) {
	tests := []struct {
		name string
		run  func() error
		want string
	}{
		{
			name: "load config",
			run: func() error {
				return run(context.Background(),
					func() (config.RuntimeConfig, error) { return config.RuntimeConfig{}, errors.New("bad env") },
					func(config.RuntimeConfig, *log.Logger) (adminService, error) { return &stubAdminService{}, nil },
					func(*http.Server) error { return nil },
					io.Discard,
				)
			},
			want: "load config",
		},
		{
			name: "create service",
			run: func() error {
				return run(context.Background(),
					func() (config.RuntimeConfig, error) { return adminCommandTestConfig(), nil },
					func(config.RuntimeConfig, *log.Logger) (adminService, error) { return nil, errors.New("bad service") },
					func(*http.Server) error { return nil },
					io.Discard,
				)
			},
			want: "create admin service",
		},
		{
			name: "listen",
			run: func() error {
				return run(context.Background(),
					func() (config.RuntimeConfig, error) { return adminCommandTestConfig(), nil },
					func(config.RuntimeConfig, *log.Logger) (adminService, error) {
						return &stubAdminService{handler: http.NewServeMux()}, nil
					},
					func(*http.Server) error { return errors.New("bind failed") },
					io.Discard,
				)
			},
			want: "admin server exited",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.run()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected error containing %q, got %v", tc.want, err)
			}
		})
	}
}

func adminCommandTestConfig() config.RuntimeConfig {
	cfg := config.RuntimeConfig{}
	cfg.Server.Host = "127.0.0.1"
	cfg.Server.Port = 19001
	return cfg
}

type stubAdminService struct {
	handler http.Handler
	closed  bool
}

func (s *stubAdminService) Handler() http.Handler {
	if s.handler != nil {
		return s.handler
	}
	return http.NewServeMux()
}

func (s *stubAdminService) Close() error {
	s.closed = true
	return nil
}
