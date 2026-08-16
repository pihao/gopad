// Command gopad runs the collaborative editor server.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"gopad/internal/server"
	"gopad/internal/store"
)

func main() {
	if err := run(); err != nil {
		slog.Error("gopad exited", "err", err)
		os.Exit(1)
	}
}

func run() error {
	port := envOr("PORT", "3030")
	sqlitePath := envOr("SQLITE_PATH", "gopad.db")

	cfg := server.Config{
		AdminUser:     os.Getenv("ADMIN_USER"),
		AdminPassword: os.Getenv("ADMIN_PASSWORD"),
		BasePath:      os.Getenv("BASE_PATH"),
	}
	if cfg.AdminUser == "" || cfg.AdminPassword == "" {
		slog.Info("admin console disabled (set ADMIN_USER and ADMIN_PASSWORD to enable)")
	}
	if v := os.Getenv("DEFAULT_TTL"); v != "" {
		ttl, err := time.ParseDuration(v)
		if err != nil {
			return err
		}
		cfg.DefaultTTL = ttl
	}
	if v := os.Getenv("MAX_DOC_SIZE"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return err
		}
		cfg.MaxDocSize = n
	}
	if sqlitePath != "" && sqlitePath != "off" {
		st, err := store.Open(sqlitePath)
		if err != nil {
			return err
		}
		defer st.Close()
		cfg.Store = st
		slog.Info("persistence enabled", "path", sqlitePath)
	} else {
		slog.Warn("persistence disabled (SQLITE_PATH=off)")
	}

	srv := server.New(cfg)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	background := make(chan struct{})
	go func() {
		defer close(background)
		srv.Run(ctx) // flush + sweep loops; final flush on cancel
	}()

	httpServer := &http.Server{Addr: ":" + port, Handler: srv}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		httpServer.Shutdown(shutdownCtx)
	}()

	if bp := srv.BasePath(); bp != "" {
		slog.Info("mounted under base path", "path", bp+"/")
	}
	slog.Info("gopad listening", "port", port)
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	<-background // wait for the final flush before closing the store
	slog.Info("gopad stopped")
	return nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
