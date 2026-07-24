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

	"github.com/NordCoder/cddm-dashboard/backend/internal/config"
	"github.com/NordCoder/cddm-dashboard/backend/internal/database"
	"github.com/NordCoder/cddm-dashboard/backend/internal/githubclient"
	"github.com/NordCoder/cddm-dashboard/backend/internal/httpapi"
	"github.com/NordCoder/cddm-dashboard/backend/internal/planning"
	"github.com/NordCoder/cddm-dashboard/backend/internal/supervisor"
)

func main() {
	if err := run(); err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	startupContext, cancelStartup := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelStartup()

	db, err := database.Open(startupContext, cfg.DatabasePath)
	if err != nil {
		return err
	}
	defer db.Close()

	client, err := githubclient.New(githubclient.Config{
		Token: cfg.GitHubToken, BaseURL: cfg.GitHubAPIBaseURL,
		RequestTimeout: cfg.GitHubRequestTimeout, MaxPages: cfg.GitHubMaxPages, MaxItems: cfg.GitHubMaxItems,
	})
	if err != nil {
		return err
	}
	store := supervisor.NewStore(db)
	syncService := supervisor.NewService(store, client, cfg.GitHubSyncTimeout, cfg.GitHubMaxSyncConcurrency)
	poller := supervisor.NewPoller(store, syncService, cfg.GitHubPollScanInterval)

	opencodePlanner, err := planning.NewOpenCodePlanner(planning.OpenCodeConfig{
		Enabled: cfg.OpenCodeEnabled, Endpoint: cfg.OpenCodeEndpoint,
		Provider: cfg.OpenCodeProvider, Model: cfg.OpenCodeModel, Agent: cfg.OpenCodeAgent,
		Username: cfg.OpenCodeUsername, Password: cfg.OpenCodePassword,
		Timeout: cfg.OpenCodeTimeout, MaxRequestBytes: cfg.OpenCodeMaxRequestBytes,
	})
	if err != nil {
		return err
	}
	planningService := planning.NewService(store, planning.NewAuditStore(db), opencodePlanner, planning.ServiceConfig{
		ContextOptions: planning.ContextOptions{
			EvidenceLimit: cfg.PromptEvidenceLimit,
			EvidenceChars: cfg.PromptEvidenceChars,
		},
		FallbackEnabled: cfg.PromptFallbackEnabled,
	})

	server := &http.Server{
		Addr: cfg.Address,
		Handler: httpapi.NewWithPlanning(
			db, store, syncService, cfg.GitHubDefaultPollInterval, planningService,
		),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      maxDuration(cfg.GitHubSyncTimeout, cfg.OpenCodeTimeout) + 15*time.Second,
		IdleTimeout:       60 * time.Second,
	}

	applicationContext, cancelApplication := context.WithCancel(context.Background())
	pollerDone := make(chan struct{})
	go func() {
		defer close(pollerDone)
		poller.Run(applicationContext)
	}()

	serverErrors := make(chan error, 1)
	go func() {
		slog.Info("API listening", "address", cfg.Address, "database", cfg.DatabasePath)
		serverErrors <- server.ListenAndServe()
	}()

	signalContext, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	select {
	case err := <-serverErrors:
		cancelApplication()
		waitContext, cancelWait := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancelWait()
		if waitErr := waitForPoller(waitContext, pollerDone); waitErr != nil {
			return fmt.Errorf("server stopped with %v; %w", err, waitErr)
		}
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-signalContext.Done():
		slog.Info("shutdown requested")
	}

	cancelApplication()
	shutdownContext, cancelShutdown := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancelShutdown()
	if err := server.Shutdown(shutdownContext); err != nil {
		return err
	}
	if err := waitForPoller(shutdownContext, pollerDone); err != nil {
		return err
	}

	err = <-serverErrors
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func waitForPoller(ctx context.Context, done <-chan struct{}) error {
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("stop polling coordinator: %w", ctx.Err())
	}
}

func maxDuration(left, right time.Duration) time.Duration {
	if right > left {
		return right
	}
	return left
}
