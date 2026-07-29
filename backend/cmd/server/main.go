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

	"github.com/NordCoder/cddm-dashboard/backend/internal/browserbinding"
	"github.com/NordCoder/cddm-dashboard/backend/internal/config"
	"github.com/NordCoder/cddm-dashboard/backend/internal/database"
	"github.com/NordCoder/cddm-dashboard/backend/internal/delivery"
	"github.com/NordCoder/cddm-dashboard/backend/internal/githubauth"
	"github.com/NordCoder/cddm-dashboard/backend/internal/githubclient"
	"github.com/NordCoder/cddm-dashboard/backend/internal/httpapi"
	"github.com/NordCoder/cddm-dashboard/backend/internal/orchestration"
	"github.com/NordCoder/cddm-dashboard/backend/internal/planning"
	"github.com/NordCoder/cddm-dashboard/backend/internal/resourcepack"
	"github.com/NordCoder/cddm-dashboard/backend/internal/supervisor"
	"github.com/NordCoder/cddm-dashboard/backend/internal/workerloop"
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

	resources, err := resourcepack.LoadDefault()
	if err != nil {
		return fmt.Errorf("validate worker resources: %w", err)
	}
	continuousResources, err := resourcepack.Load(resourcepack.V2Profile)
	if err != nil {
		return fmt.Errorf("validate continuous worker resources: %w", err)
	}
	slog.Info("worker resources loaded", "profile", resources.Profile, "digest", resources.Digest, "continuous_profile", continuousResources.Profile, "continuous_digest", continuousResources.Digest)

	startupContext, cancelStartup := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelStartup()

	db, err := database.Open(startupContext, cfg.DatabasePath)
	if err != nil {
		return err
	}
	defer db.Close()

	githubToken, githubAuthSource, err := githubauth.Resolve(startupContext, cfg.GitHubAuthMode, cfg.GitHubToken, githubauth.GHCLI{})
	if err != nil {
		return fmt.Errorf("configure GitHub authentication: %w", err)
	}
	slog.Info("GitHub authentication configured", "source", githubAuthSource)

	client, err := githubclient.New(githubclient.Config{
		Token: githubToken, BaseURL: cfg.GitHubAPIBaseURL,
		RequestTimeout: cfg.GitHubRequestTimeout, MaxPages: cfg.GitHubMaxPages, MaxItems: cfg.GitHubMaxItems,
	})
	if err != nil {
		return err
	}
	store := supervisor.NewStore(db)
	workerStore := workerloop.NewStore(db)
	orchestrationStore := orchestration.NewStore(db)
	scheduler := orchestration.NewScheduler(orchestrationStore)
	provisioningService, err := orchestration.NewProvisioningService(orchestrationStore, continuousResources)
	if err != nil {
		return fmt.Errorf("initialize session provisioning: %w", err)
	}
	actionMaterializer := orchestration.NewMaterializer(orchestrationStore)
	workerResultService := workerloop.NewService(workerStore)
	workerStateService := workerloop.NewStateService(store, workerStore)
	qaBindingRetirer := workerloop.NewQABindingRetirer(db)
	syncService := supervisor.NewService(store, client, cfg.GitHubSyncTimeout, cfg.GitHubMaxSyncConcurrency)
	syncService.SetSnapshotObserver(workerloop.NewSyncObserver(workerResultService, workerStateService, qaBindingRetirer))
	poller := supervisor.NewPoller(store, syncService, cfg.GitHubPollScanInterval)

	projects, err := store.ListProjects(startupContext)
	if err != nil {
		return fmt.Errorf("list projects for worker-loop recovery: %w", err)
	}
	for _, project := range projects {
		if err := workerStateService.RefreshProject(startupContext, project.ID); err != nil {
			return fmt.Errorf("restore worker-loop projection for project %d: %w", project.ID, err)
		}
	}

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
	commandEngine := workerloop.NewCommandEngine(workerStore, resources)
	continuousCommandEngine := workerloop.NewCommandEngine(workerStore, continuousResources)
	deliveryPlanning := workerloop.NewDeliveryPlanningAdapter(planningService, commandEngine)
	continuousDeliveryPlanning := workerloop.NewDeliveryPlanningAdapter(planningService, continuousCommandEngine)
	bindingService := browserbinding.New(db, cfg.BrowserBindingTTL)
	baseBindingResolver := delivery.NewBrowserBindingResolver(bindingService)
	bindingResolver := orchestration.NewDeliveryBindingResolver(db, baseBindingResolver)
	provisioningFinalizer, err := orchestration.NewProvisioningFinalizer(orchestrationStore, bindingService)
	if err != nil {
		return fmt.Errorf("initialize provisioning finalizer: %w", err)
	}
	browserDelivery := delivery.New(db, deliveryPlanning, bindingResolver, delivery.Config{
		Enabled: cfg.BrowserDeliveryEnabled, PendingTTL: cfg.BrowserDeliveryPendingTTL, ClaimTTL: cfg.BrowserDeliveryClaimTTL,
	})
	continuousBrowserDelivery := delivery.New(db, continuousDeliveryPlanning, bindingResolver, delivery.Config{
		Enabled: cfg.BrowserDeliveryEnabled, PendingTTL: cfg.BrowserDeliveryPendingTTL, ClaimTTL: cfg.BrowserDeliveryClaimTTL,
	})
	deliveryService := workerloop.NewDeliveryCoordinator(db, browserDelivery, planningService, commandEngine, workerStateService)
	continuousDeliveryService := workerloop.NewDeliveryCoordinator(db, continuousBrowserDelivery, planningService, continuousCommandEngine, workerStateService)
	autopilot, err := orchestration.NewAutopilotEngine(
		orchestrationStore, scheduler, provisioningService, planningService,
		continuousDeliveryService, baseBindingResolver, store,
	)
	if err != nil {
		return fmt.Errorf("initialize Autopilot: %w", err)
	}
	mergeAutopilot, err := orchestration.NewMergeAutopilotEngine(
		orchestrationStore, scheduler, provisioningService, planningService,
		continuousDeliveryService, baseBindingResolver, store, client,
	)
	if err != nil {
		return fmt.Errorf("initialize merge Autopilot: %w", err)
	}
	resultMux := orchestration.NewCommandResultMux(mergeAutopilot, autopilot)
	workerResultService.SetResultMaterializer(orchestration.NewResultMaterializationPipeline(resultMux, actionMaterializer))
	if err := deliveryService.Reconcile(startupContext); err != nil {
		return fmt.Errorf("reconcile browser and workflow commands: %w", err)
	}
	if err := autopilot.ReconcileAll(startupContext); err != nil {
		return fmt.Errorf("reconcile Autopilot: %w", err)
	}
	if err := mergeAutopilot.ReconcileAll(startupContext); err != nil {
		return fmt.Errorf("reconcile merge Autopilot: %w", err)
	}
	projectionService := workerloop.NewProjectionService(db, store, workerStore, bindingService, resources, planningService)
	baseHandler := httpapi.NewWithPlanningAndBindingServiceAndDelivery(
		db, store, syncService, cfg.GitHubDefaultPollInterval, planningService, bindingService, deliveryService,
	)
	workerHandler := httpapi.WithWorkerLoop(baseHandler, store, projectionService, bindingService)
	provisioningHandler := httpapi.WithProvisioningAndFinalizer(workerHandler, orchestrationStore, provisioningService, provisioningFinalizer)

	server := &http.Server{
		Addr:              cfg.Address,
		Handler:           withMutationRequestGuard(provisioningHandler),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      maxDuration(cfg.GitHubSyncTimeout, cfg.OpenCodeTimeout) + 15*time.Second,
		IdleTimeout:       60 * time.Second,
	}

	applicationContext, cancelApplication := context.WithCancel(context.Background())
	go deliveryService.ReconcilePeriodically(applicationContext, minDuration(cfg.BrowserDeliveryClaimTTL/2, time.Minute), func(err error) {
		slog.Error("reconcile browser and workflow commands", "error", err)
	})
	go autopilot.Run(applicationContext, 5*time.Second, func(err error) {
		slog.Error("reconcile Autopilot", "error", err)
	})
	go mergeAutopilot.Run(applicationContext, 5*time.Second, func(err error) {
		slog.Error("reconcile merge Autopilot", "error", err)
	})
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
		return ctx.Err()
	}
}

func maxDuration(left, right time.Duration) time.Duration {
	if left > right {
		return left
	}
	return right
}

func minDuration(left, right time.Duration) time.Duration {
	if left < right {
		return left
	}
	return right
}
