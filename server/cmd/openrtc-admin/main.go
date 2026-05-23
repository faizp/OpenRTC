package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/openrtc/openrtc/server/internal/admin"
	"github.com/openrtc/openrtc/server/internal/config"
)

func main() {
	if err := run(context.Background(), config.LoadFromOS, func(cfg config.RuntimeConfig, logger *log.Logger) (adminService, error) {
		return admin.NewService(cfg, logger)
	}, func(server *http.Server) error {
		return server.ListenAndServe()
	}, os.Stdout); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}

type adminService interface {
	Handler() http.Handler
	Close() error
}

func run(ctx context.Context, loadConfig func() (config.RuntimeConfig, error), newService func(config.RuntimeConfig, *log.Logger) (adminService, error), listenAndServe func(*http.Server) error, logOutput io.Writer) error {
	cfg, err := loadConfig()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	logger := log.New(logOutput, "openrtc-admin ", log.LstdFlags)
	service, err := newService(cfg, logger)
	if err != nil {
		return fmt.Errorf("create admin service: %w", err)
	}
	defer service.Close()

	server := &http.Server{
		Addr:    fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port),
		Handler: service.Handler(),
	}

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		_ = server.Shutdown(context.Background())
	}()

	logger.Printf("admin server starting: %s", server.Addr)
	if err := listenAndServe(server); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("admin server exited: %w", err)
	}
	return nil
}
