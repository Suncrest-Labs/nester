package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/Suncrest-Labs/nester/internal/config"
	"github.com/Suncrest-Labs/nester/internal/handler"
	"github.com/Suncrest-Labs/nester/internal/service"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	serverCfg := cfg.Server()

	health := handler.NewHealthHandler()
	prometheus := service.NewPrometheusClient(cfg.Prometheus())
	intelligence := handler.NewIntelligenceHandler(prometheus)

	router := handler.NewRouter(cfg, health, intelligence)

	server := &http.Server{
		Addr:              serverCfg.Address(),
		Handler:           router,
		ReadTimeout:       serverCfg.ReadTimeout(),
		ReadHeaderTimeout: serverCfg.ReadHeaderTimeout(),
		WriteTimeout:      serverCfg.WriteTimeout(),
		IdleTimeout:       serverCfg.IdleTimeout(),
		MaxHeaderBytes:    serverCfg.MaxHeaderBytes(),
	}

	log.Printf("server starting on %s", serverCfg.Address())

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server failed: %v", err)
		}
	}()

	health.SetReady(true)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Println("shutdown signal received, gracefully stopping server")

	ctx, cancel := context.WithTimeout(context.Background(), serverCfg.GracefulShutdown())
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Fatalf("server shutdown failed: %v", err)
	}

	log.Println("server stopped")
}
