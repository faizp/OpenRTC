package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/openrtc/openrtc/server/internal/admin"
	"github.com/openrtc/openrtc/server/internal/config"
)

func main() {
	cfg, err := config.LoadFromOS()
	if err != nil {
		log.Printf("load config: %v", err)
		os.Exit(1)
	}

	logger := log.New(os.Stdout, "openrtc-admin ", log.LstdFlags)
	service, err := admin.NewService(cfg, logger)
	if err != nil {
		logger.Printf("create admin service: %v", err)
		os.Exit(1)
	}
	defer service.Close()

	server := &http.Server{
		Addr:    fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port),
		Handler: service.Handler(),
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		_ = server.Shutdown(context.Background())
	}()

	logger.Printf("admin server starting: %s", server.Addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Printf("admin server exited: %v", err)
		os.Exit(1)
	}
}
