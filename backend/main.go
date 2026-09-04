package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"agents-controllers/backend/agents"
	"agents-controllers/backend/config"
	"agents-controllers/backend/events"
	"agents-controllers/backend/handlers"
	"agents-controllers/backend/store"
	"github.com/joho/godotenv"
)

func main() {
	flagAddr := flag.String("addr", "", "override ADDR (e.g. :8080)")
	flagData := flag.String("data", "", "override DATA_DIR")
	flag.Parse()

	// godotenv — только для dev; в проде окружение приходит из системы.
	if _, err := os.Stat(".env"); err == nil {
		if err := godotenv.Load(".env"); err != nil {
			log.Printf("[Config] failed to load .env: %v", err)
		}
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("[Config] %v", err)
	}
	if *flagAddr != "" {
		cfg.Addr = *flagAddr
	}
	if *flagData != "" {
		cfg.DataDir = *flagData
	}
	log.Printf("[Config] addr=%s data=%s aider=%s python=%s runner=%s",
		cfg.Addr, cfg.DataDir, cfg.AiderBin, cfg.PythonBin, cfg.RunnerPath)

	st, err := store.New(cfg.DataDir)
	if err != nil {
		log.Fatalf("[Config] store: %v", err)
	}
	hub := events.NewHub(cfg.LogTail)
	sup := agents.NewSupervisor(cfg, st, hub)

	r := handlers.NewRouter(cfg, st, sup, hub)
	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("[Config] listening on %s", cfg.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("[Config] server: %v", err)
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	log.Println("[Config] shutdown signal received")

	sup.StopAll()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("[Config] forced shutdown: %v", err)
	}
	log.Println("[Config] bye")
}
