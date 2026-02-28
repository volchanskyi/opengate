package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/volchanskyi/opengate/server/internal/api"
	"github.com/volchanskyi/opengate/server/internal/auth"
	"github.com/volchanskyi/opengate/server/internal/cert"
	"github.com/volchanskyi/opengate/server/internal/db"
)

func main() {
	listen := flag.String("listen", ":8080", "HTTP listen address")
	dataDir := flag.String("data-dir", "./data", "directory for database and certificates")
	jwtSecret := flag.String("jwt-secret", "", "JWT signing secret (or JWT_SECRET env)")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	secret := *jwtSecret
	if secret == "" {
		secret = os.Getenv("JWT_SECRET")
	}
	if secret == "" {
		logger.Error("jwt secret is required: set --jwt-secret or JWT_SECRET")
		os.Exit(1)
	}

	if err := os.MkdirAll(*dataDir, 0755); err != nil {
		logger.Error("create data dir", "error", err)
		os.Exit(1)
	}

	store, err := db.NewSQLiteStore(filepath.Join(*dataDir, "opengate.db"))
	if err != nil {
		logger.Error("open database", "error", err)
		os.Exit(1)
	}
	defer store.Close()

	certMgr, err := cert.NewManager(*dataDir)
	if err != nil {
		logger.Error("init cert manager", "error", err)
		os.Exit(1)
	}
	_ = certMgr // used in future phases for agent mTLS

	jwtCfg := &auth.JWTConfig{
		Secret:   secret,
		Issuer:   "opengate",
		Duration: 24 * time.Hour,
	}

	srv := api.NewServer(store, jwtCfg, logger)

	httpSrv := &http.Server{
		Addr:         *listen,
		Handler:      srv,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGTERM)

	go func() {
		logger.Info("server starting", "addr", *listen)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	<-done
	logger.Info("shutting down")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := httpSrv.Shutdown(ctx); err != nil {
		logger.Error("shutdown error", "error", err)
	}

	logger.Info("server stopped")
}
