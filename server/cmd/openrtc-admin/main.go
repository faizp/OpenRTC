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
	"time"

	"github.com/openrtc/openrtc/server/internal/admin"
	"github.com/openrtc/openrtc/server/internal/config"
)

var (
	exit            = os.Exit
	loadConfig      = config.LoadFromOS
	newAdminService = func(cfg config.RuntimeConfig, logger *log.Logger) (adminService, error) {
		return admin.NewService(cfg, logger)
	}
	listenAndServe = func(server *http.Server) error {
		return server.ListenAndServe()
	}
)

const (
	readHeaderTimeout = 5 * time.Second
	idleTimeout       = 60 * time.Second
	shutdownTimeout   = 10 * time.Second
)

func main() {
	mainWithDeps(loadConfig, newAdminService, listenAndServe, os.Stdout)
}

func mainWithDeps(loadConfig func() (config.RuntimeConfig, error), newService func(config.RuntimeConfig, *log.Logger) (adminService, error), listenAndServe func(*http.Server) error, logOutput io.Writer) {
	if err := run(context.Background(), loadConfig, newService, listenAndServe, logOutput); err != nil {
		log.Print(err)
		exit(1)
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
		Addr:              fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port),
		Handler:           service.Handler(),
		ReadHeaderTimeout: readHeaderTimeout,
		IdleTimeout:       idleTimeout,
	}

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	logger.Printf("admin server starting: %s", server.Addr)
	if err := listenAndServe(server); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("admin server exited: %w", err)
	}
	return nil
}
