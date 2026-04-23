package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/lucientong/argus/internal/config"
	"github.com/lucientong/argus/internal/integrations"
	"github.com/lucientong/argus/internal/types"
	"github.com/lucientong/argus/internal/web"
	"github.com/lucientong/argus/internal/webhook"
)

func main() {
	// Structured logger.
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	// Config path from env, default to configs/config.yaml.
	cfgPath := envOr("ARGUS_CONFIG", "configs/config.yaml")
	cfg, err := config.Load(cfgPath)
	if err != nil {
		slog.Error("failed to load config", "path", cfgPath, "error", err)
		os.Exit(1)
	}

	// Incident store + dashboard.
	store := web.NewStore()
	dashboard := web.NewServer(store)

	// Build integration clients (mock or real depending on config).
	clients, err := integrations.Build(cfg)
	if err != nil {
		slog.Error("failed to build integration clients", "error", err)
		os.Exit(1)
	}
	slog.Info("integration clients initialised", "mode", cfg.Integrations.Mode)
	_ = clients // will be consumed by pipeline agents in Phase 13+

	mux := http.NewServeMux()
	mux.HandleFunc("/health", handleHealth)

	// Mount dashboard routes.
	mux.Handle("/dashboard", dashboard.Handler())
	mux.Handle("/api/incidents", dashboard.Handler())
	mux.Handle("/api/incidents/", dashboard.Handler())
	mux.Handle("/api/events", dashboard.Handler())

	// Alert handler — runs the pipeline and stores the resulting IncidentReport.
	// The full pipeline wiring (with real LLM providers) is deferred to Phase 12;
	// for now we store a stub report so the dashboard has data to display.
	alertHandler := func(alert types.Alert) {
		slog.Info("alert received", "id", alert.ID, "source", alert.Source, "severity", alert.Severity, "title", alert.Title)

		// Create an in-progress report immediately so it shows in the dashboard.
		report := types.IncidentReport{
			ID:        alert.ID,
			Alert:     types.ClassifiedAlert{Alert: alert},
			Status:    types.IncidentStatusInProgress,
			StartedAt: time.Now(),
		}
		store.Upsert(report)

		// Emit a start step to connected SSE clients.
		obs := dashboard.NewPipelineObserver(alert.ID)
		_ = obs // pipeline agents will call obs.OnStep(...) once wired in Phase 12
	}

	mux.HandleFunc("/webhook/grafana", webhook.GrafanaHandler(alertHandler))
	mux.HandleFunc("/webhook/pagerduty", webhook.PagerDutyHandler(alertHandler))

	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	srv := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Graceful shutdown.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		slog.Info("argus listening", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	slog.Info("shutting down")

	shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutCtx); err != nil {
		slog.Error("shutdown error", "error", err)
	}
	slog.Info("stopped")
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintln(w, `{"status":"ok"}`)
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
