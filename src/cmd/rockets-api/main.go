package main

import (
	"context"
	"errors"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"lunar-rockets/src/internal/handler"
	"lunar-rockets/src/internal/repository/sqlite"
	"lunar-rockets/src/internal/service"
)

func main() {
	config := loadConfig()

	database, err := sqlite.Open(config.DBPath)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer database.Close()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	ingest := service.NewIngestMessageService(
		database,
		service.RealClock{},
		service.WithLogger(logger),
	)
	query := service.NewRocketQueryService(database)
	health := service.NewHealthService(database)
	api := handler.New(ingest, query, health)

	server := &http.Server{
		Addr:              ":" + config.Port,
		Handler:           api.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Printf("rockets API listening on http://localhost:%s", config.Port)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("listen: %v", err)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("shutdown: %v", err)
	}
}

type config struct {
	Port   string
	DBPath string
}

func loadConfig() config {
	return config{
		Port:   env("PORT", "8088"),
		DBPath: env("DB_PATH", "rockets.db"),
	}
}

func env(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}
