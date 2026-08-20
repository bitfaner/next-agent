package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"nextx/backend/internal/codex"
	"nextx/backend/internal/server"
	"nextx/backend/internal/store"
)

func main() {
	addr := flag.String("addr", env("NEXTX_ADDR", "127.0.0.1:34121"), "HTTP listen address")
	dbPath := flag.String("db", env("NEXTX_DB", defaultDBPath()), "SQLite database path")
	flag.Parse()

	if err := os.MkdirAll(filepath.Dir(*dbPath), 0o755); err != nil {
		slog.Error("create data dir", "error", err)
		os.Exit(1)
	}

	db, err := store.Open(*dbPath)
	if err != nil {
		slog.Error("open database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	app := server.New(db, codex.NewManager(codex.ConfigFromEnv()))
	srv := &http.Server{
		Addr:              *addr,
		Handler:           app.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	slog.Info("nextx backend listening", "addr", *addr, "db", *dbPath)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("server failed", "error", err)
		os.Exit(1)
	}

	_ = srv.Shutdown(context.Background())
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func defaultDBPath() string {
	if dir, err := os.UserConfigDir(); err == nil {
		return filepath.Join(dir, "nextx-agent", "nextx.db")
	}
	return fmt.Sprintf("%s/nextx-agent.db", os.TempDir())
}
