package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/pmcgn/smtp2paperless/internal/config"
	"github.com/pmcgn/smtp2paperless/internal/mailparse"
	"github.com/pmcgn/smtp2paperless/internal/metrics"
	"github.com/pmcgn/smtp2paperless/internal/paperless"
	"github.com/pmcgn/smtp2paperless/internal/smtpserver"
)

func main() {
	// `smtp2paperless healthcheck` is used by the container HEALTHCHECK, since
	// the distroless image has no curl/wget.
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(healthcheck())
	}
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	// Optional local-development convenience; real environment variables win.
	if err := config.LoadDotEnv(envOr("ENV_FILE", ".env")); err != nil {
		return fmt.Errorf("load env file: %w", err)
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	hopts := &slog.HandlerOptions{Level: cfg.LogLevel}
	var h slog.Handler = slog.NewTextHandler(os.Stderr, hopts)
	if cfg.LogJSON {
		h = slog.NewJSONHandler(os.Stderr, hopts)
	}
	log := slog.New(h)
	slog.SetDefault(log)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	m := metrics.New()
	client := paperless.New(cfg.PaperlessURL, cfg.PaperlessToken, cfg.HTTPTimeout, cfg.UploadRetries,
		paperless.Options{Tags: cfg.Tags, Correspondent: cfg.Correspondent, DocumentType: cfg.DocumentType}, m, log)

	smtpSrv := smtpserver.New(smtpserver.Options{
		Addr:      fmt.Sprintf(":%d", cfg.SMTPPort),
		Domain:    cfg.SMTPDomain,
		MaxBytes:  cfg.SMTPMaxBytes,
		MaxRcpts:  cfg.SMTPMaxRcpts,
		Timeout:   cfg.SMTPTimeout,
		Whitelist: mailparse.NewWhitelist(cfg.Whitelist),
		UploadCtx: context.WithoutCancel(ctx), // let in-flight uploads finish on shutdown
		Uploader:  client,
		Metrics:   m,
		Logger:    log,
	})

	mux := http.NewServeMux()
	mux.Handle("GET /metrics", m.Handler())
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { fmt.Fprintln(w, "ok") })
	httpSrv := &http.Server{Addr: fmt.Sprintf(":%d", cfg.MetricsPort), Handler: mux, ReadHeaderTimeout: 10 * time.Second}

	errc := make(chan error, 2)
	go func() {
		log.Info("smtp server listening", "port", cfg.SMTPPort, "whitelist", cfg.Whitelist, "paperless", cfg.PaperlessURL)
		if err := smtpSrv.ListenAndServe(); err != nil && !smtpserver.IsClosed(err) {
			errc <- fmt.Errorf("smtp server: %w", err)
		}
	}()
	go func() {
		log.Info("metrics server listening", "port", cfg.MetricsPort)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errc <- fmt.Errorf("metrics server: %w", err)
		}
	}()

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
		log.Info("shutting down")
	}

	sctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(sctx)
	if err := smtpSrv.Shutdown(sctx); err != nil {
		return smtpSrv.Close()
	}
	return nil
}

func healthcheck() int {
	addr := "127.0.0.1:" + envOr("METRICS_PORT", "9090")
	c := http.Client{Timeout: 3 * time.Second}
	resp, err := c.Get("http://" + addr + "/healthz")
	if err != nil {
		return 1
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
