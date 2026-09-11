package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/coreos/go-systemd/v22/daemon"
	"github.com/freshost/pve-snapshot-api/pkg/api"
	"github.com/freshost/pve-snapshot-api/pkg/auth"
	"github.com/freshost/pve-snapshot-api/pkg/cluster"
	"github.com/freshost/pve-snapshot-api/pkg/config"
	"github.com/freshost/pve-snapshot-api/pkg/pool"
	"github.com/freshost/pve-snapshot-api/pkg/proxy"
	"github.com/freshost/pve-snapshot-api/pkg/pvetls"
	"github.com/freshost/pve-snapshot-api/pkg/storage/zfs"
	"github.com/freshost/pve-snapshot-api/pkg/task"
)

func main() {
	configPath := flag.String("config", config.DefaultConfigPath, "path to config file")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
		os.Exit(1)
	}

	setupLogging(cfg.LogLevel)

	// Command runner using os/exec
	cmdRunner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return exec.CommandContext(ctx, name, args...).CombinedOutput()
	}

	// Initialize cluster discovery
	cs := cluster.New(cfg.PveshTimeout, cmdRunner)
	if err := cs.Discover(context.Background()); err != nil {
		slog.Warn("initial cluster discovery failed (standalone mode)", "error", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go cs.StartPeriodicRefresh(ctx, 30*time.Second)

	// Create storage backend (default to ZFS)
	backend := zfs.New(cfg, cmdRunner)

	// Create authenticator and proxy
	authenticator := auth.New(cfg.PveshTimeout, cfg.ProxmoxAPIURL, cfg.AuthCacheTTL, cfg.CAFile)
	useTLS := cfg.TLS != nil && cfg.TLS.CertFile != "" && cfg.TLS.KeyFile != ""
	prx := proxy.New(cs, cfg.ListenPort, useTLS, cfg.CAFile)

	// Create task store and pool resolver
	taskStore, err := task.OpenStore(cfg.TaskStateFile)
	if err != nil {
		slog.Error("open task store", "error", err)
		os.Exit(1)
	}
	if _, err := pvetls.Config(cfg.CAFile); err != nil {
		slog.Error("TLS trust configuration", "error", err)
		os.Exit(1)
	}
	poolResolver := pool.New(cfg.PveshTimeout, cmdRunner)

	// Create handler
	handler := api.NewServer(backend, authenticator, prx, cs, cfg, taskStore, poolResolver)

	// Start HTTP server
	addr := fmt.Sprintf(":%d", cfg.ListenPort)
	server := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       10 * time.Minute,
		IdleTimeout:       2 * time.Minute,
	}

	if useTLS {
		if _, err := tls.LoadX509KeyPair(cfg.TLS.CertFile, cfg.TLS.KeyFile); err != nil {
			slog.Error("load server certificate", "error", err)
			os.Exit(1)
		}
		server.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12, GetCertificate: pvetls.ReloadCertificate(cfg.TLS.CertFile, cfg.TLS.KeyFile)}
	}
	// Listen
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		slog.Error("failed to listen", "addr", addr, "error", err)
		os.Exit(1)
	}

	slog.Info("server starting", "addr", addr, "proxmox_api_url", cfg.ProxmoxAPIURL)

	// Notify systemd we're ready
	daemon.SdNotify(false, daemon.SdNotifyReady)

	// Handle graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	go func() {
		if cfg.TLS != nil && cfg.TLS.CertFile != "" && cfg.TLS.KeyFile != "" {
			if err := server.ServeTLS(ln, "", ""); err != nil && err != http.ErrServerClosed {
				slog.Error("server error", "error", err)
				os.Exit(1)
			}
		} else {
			if err := server.Serve(ln); err != nil && err != http.ErrServerClosed {
				slog.Error("server error", "error", err)
				os.Exit(1)
			}
		}
	}()

	sig := <-sigCh
	slog.Info("shutting down", "signal", sig)

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		slog.Error("shutdown error", "error", err)
	}

	cancel() // stop cluster refresh
	slog.Info("server stopped")
}

func setupLogging(level string) {
	var logLevel slog.Level
	switch level {
	case "debug":
		logLevel = slog.LevelDebug
	case "warn":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	default:
		logLevel = slog.LevelInfo
	}

	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: logLevel,
	})
	slog.SetDefault(slog.New(handler))
}
