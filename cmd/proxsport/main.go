package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/blink-zero/proxsport/internal/collector"
	"github.com/blink-zero/proxsport/internal/config"
	"github.com/blink-zero/proxsport/internal/proxmox"
	"github.com/blink-zero/proxsport/internal/version"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	cfg, err := config.Load()
	if err != nil {
		slog.Error("config load failed", "error", err)
		os.Exit(2)
	}

	slog.Info("starting proxsport",
		"version", version.Version,
		"commit", version.Commit,
		"date", version.Date,
		"proxmox_url", cfg.ProxmoxURL,
		"listen", cfg.ListenAddr,
		"poll_interval", cfg.PollInterval,
	)

	clientOpts := []proxmox.Option{
		proxmox.WithInsecureSkipVerify(cfg.InsecureSkipVerify),
		proxmox.WithTimeout(cfg.RequestTimeout),
	}
	switch {
	case cfg.TokenID != "" && cfg.TokenSecret != "":
		clientOpts = append(clientOpts, proxmox.WithToken(cfg.TokenID, cfg.TokenSecret))
	case cfg.Username != "" && cfg.Password != "":
		clientOpts = append(clientOpts, proxmox.WithPassword(cfg.Username, cfg.Password))
	}
	client := proxmox.New(cfg.ProxmoxURL, clientOpts...)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if cfg.Username != "" {
		if err := client.Login(ctx); err != nil {
			slog.Error("Proxmox login failed", "error", err)
			os.Exit(3)
		}
	}

	reg := prometheus.NewRegistry()
	coll := collector.New(client, cfg)
	if err := reg.Register(coll); err != nil {
		slog.Error("register collector failed", "error", err)
		os.Exit(4)
	}

	go coll.Run(ctx)

	mux := http.NewServeMux()
	mux.Handle(cfg.MetricsPath, promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!doctype html><html><head><title>proxsport</title></head><body>
<h1>proxsport</h1>
<p>Prometheus exporter for Proxmox VE.</p>
<ul>
<li><a href="` + cfg.MetricsPath + `">` + cfg.MetricsPath + `</a></li>
<li><a href="/healthz">/healthz</a></li>
</ul>
</body></html>`))
	})

	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
	}

	srvErr := make(chan error, 1)
	go func() {
		slog.Info("HTTP server listening", "addr", cfg.ListenAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			srvErr <- err
		}
	}()

	select {
	case <-ctx.Done():
		slog.Info("shutdown signal received")
	case err := <-srvErr:
		slog.Error("HTTP server error", "error", err)
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("HTTP server shutdown failed", "error", err)
	}
	slog.Info("proxsport stopped")
}
