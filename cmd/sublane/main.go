package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/murongg/SubLane/internal/accounts"
	"github.com/murongg/SubLane/internal/alerts"
	"github.com/murongg/SubLane/internal/apikey"
	"github.com/murongg/SubLane/internal/auth"
	"github.com/murongg/SubLane/internal/config"
	"github.com/murongg/SubLane/internal/content"
	"github.com/murongg/SubLane/internal/demo"
	"github.com/murongg/SubLane/internal/gateway"
	"github.com/murongg/SubLane/internal/metrics"
	"github.com/murongg/SubLane/internal/pricing"
	"github.com/murongg/SubLane/internal/server"
	"github.com/murongg/SubLane/internal/storage"
	"github.com/murongg/SubLane/internal/tenants"
	"github.com/murongg/SubLane/internal/timezone"
	"github.com/murongg/SubLane/internal/turnstile"
	"github.com/murongg/SubLane/internal/updates"
	"github.com/murongg/SubLane/internal/upstream"
	"github.com/murongg/SubLane/internal/versions"
	"github.com/murongg/SubLane/web"
)

var version = "0.1.0-dev"

func main() {
	if err := run(); err != nil {
		slog.Error("SubLane stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		fmt.Println(version)
		return nil
	}
	if len(os.Args) > 1 && (os.Args[1] == "--help" || os.Args[1] == "-h") {
		fmt.Print(commandHelp)
		return nil
	}
	if len(os.Args) > 1 && (os.Args[1] == "backup" || os.Args[1] == "restore") {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		return runMaintenance(ctx, os.Args[1:], os.Getenv("SUBLANE_DATA_DIR"), os.Getenv("SUBLANE_CREDENTIALS_KEY_FILE"), os.Stdin, os.Stdout)
	}
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}
	if len(os.Args) > 1 {
		if len(os.Args) != 3 || os.Args[1] != "reset-admin-password" || os.Args[2] != "--password-stdin" {
			return errors.New(commandHelp)
		}
		info, err := os.Stdin.Stat()
		if err != nil || info.Mode()&os.ModeCharDevice != 0 {
			return errors.New("supply the new password through a pipe or redirected file; terminal input is not accepted")
		}
		if err := recoverPassword(context.Background(), cfg.DataDir, os.Stdin); err != nil {
			return err
		}
		fmt.Println("Administrator password reset. Two-factor authentication and passkeys were cleared; all administrator browser sessions were revoked. API keys are unchanged.")
		return nil
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	slog.SetDefault(logger)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if cfg.Demo {
		instance, err := demo.New(ctx, demo.Options{Assets: web.Assets(), Version: version, PublicURL: cfg.PublicURL, TrustedProxies: cfg.TrustedProxies})
		if err != nil {
			return fmt.Errorf("start demo: %w", err)
		}
		defer instance.Close()
		logger.Info("Demo mode enabled: disposable sample data, read-only, no live gateway")
		return serve(ctx, logger, cfg.Addr, instance.Handler)
	}
	db, err := storage.Open(ctx, filepath.Join(cfg.DataDir, "sublane.db"))
	if err != nil {
		return err
	}
	defer db.Close()
	cipher, err := openVault(ctx, db, cfg.DataDir, cfg.CredentialsKeyFile)
	if err != nil {
		return err
	}
	authentication, err := auth.NewWithVault(db, cipher)
	if err != nil {
		return err
	}
	if err := auth.VerifyFactors(ctx, db, cipher); err != nil {
		return fmt.Errorf("verify authentication factors: %w", err)
	}
	if err := content.New(db, cipher, 1).Verify(ctx); err != nil {
		return fmt.Errorf("verify workspace content rules: %w", err)
	}
	keys := apikey.New(db, cipher)
	notifications := alerts.New(db, cipher, nil)
	if err := notifications.Verify(ctx); err != nil {
		return fmt.Errorf("verify workspace webhook credentials: %w", err)
	}
	defer notifications.Close()
	if err := keys.Verify(ctx); err != nil {
		return fmt.Errorf("verify API key secrets: %w", err)
	}
	tenancy := tenants.New(db)
	ids, err := tenancy.AllIDs(ctx)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := accounts.NewForTenant(db, cipher, id).Verify(ctx); err != nil {
			return fmt.Errorf("verify upstream credentials for workspace %d: %w", id, err)
		}
	}
	releases := versions.NewReleases(nil)
	defer releases.Close()
	codexVersions, err := versions.New(ctx, db, upstream.DefaultCodexVersion, releases.Latest)
	if err != nil {
		return err
	}
	defer codexVersions.Close()
	grokReleases := versions.NewGrokReleases(nil)
	defer grokReleases.Close()
	grokVersions, err := versions.NewForProvider(ctx, db, versions.Grok, upstream.DefaultGrokVersion, grokReleases.Latest)
	if err != nil {
		return err
	}
	defer grokVersions.Close()
	claudeReleases := versions.NewClaudeReleases(nil)
	defer claudeReleases.Close()
	claudeVersions, err := versions.NewForProvider(ctx, db, versions.Claude, upstream.DefaultClaudeVersion, claudeReleases.Latest)
	if err != nil {
		return err
	}
	defer claudeVersions.Close()
	timeZone, err := timezone.New(ctx, db)
	if err != nil {
		return fmt.Errorf("load instance time zone: %w", err)
	}
	loginVerification, err := turnstile.New(ctx, db, cipher, nil)
	if err != nil {
		return fmt.Errorf("load login verification: %w", err)
	}
	codexVersions.Start()
	grokVersions.Start()
	claudeVersions.Start()
	provider := upstream.NewWithOptions(upstream.Options{Version: codexVersions.Current, GrokVersion: grokVersions.Current, ClaudeVersion: claudeVersions.Current, MaxRequestBody: cfg.MaxRequestBody, InferenceHeaderTimeout: &cfg.InferenceHeaderTimeout})
	defer provider.Close()
	if err := provider.Start(ctx); err != nil {
		return err
	}
	pricingCache := cfg.PricingCache
	if pricingCache == "" {
		pricingCache = filepath.Join(cfg.DataDir, "model-prices.json")
	}
	priceCatalog, err := pricing.New(pricing.Config{URL: cfg.PricingURL, HashURL: cfg.PricingHashURL, CachePath: pricingCache, HashPath: pricingCache + ".sha256", FallbackPath: cfg.PricingFallback, OverridePath: cfg.PricingOverride, Interval: cfg.PricingInterval})
	if err != nil {
		return fmt.Errorf("load model pricing: %w", err)
	}
	priceCatalog.Start(ctx)
	defer priceCatalog.Close()
	var telemetry *metrics.Collector
	if cfg.MetricsToken != "" {
		telemetry = &metrics.Collector{}
	}
	softwareUpdates := updates.New(version, cfg.UpdateDir, nil)
	defer softwareUpdates.Close()
	registry := &tenantRegistry{updates: softwareUpdates, metrics: telemetry, ctx: ctx, db: db, vault: cipher, auth: authentication,
		refreshLead: cfg.CodexRefreshLead, refreshInterval: cfg.AccountRefreshInterval,
		turnstile: loginVerification,
		alerts:    notifications,
		tenants:   tenancy, provider: provider, pricing: priceCatalog, versions: codexVersions, grokVersions: grokVersions, claudeVersions: claudeVersions, timeZone: timeZone,
		assets: web.Assets(), dataDir: cfg.DataDir, keyFile: cfg.CredentialsKeyFile, publicURL: cfg.PublicURL, trustedProxies: cfg.TrustedProxies,
		version: version, started: time.Now(), gatewayPolicy: cfg.Gateway}
	defer registry.Close()
	registry.Start()
	maintenanceCtx, stopMaintenance := context.WithCancel(ctx)
	maintenanceDone := make(chan struct{})
	go func() {
		defer close(maintenanceDone)
		gateway.MaintainHistory(maintenanceCtx, db)
	}()
	defer func() { stopMaintenance(); <-maintenanceDone }()
	if cfg.Backup.Directory != "" {
		backupDone := make(chan struct{})
		backupCtx, stopBackup := context.WithCancel(ctx)
		go func() {
			defer close(backupDone)
			var onFailure func(context.Context)
			if cfg.BackupWebhookURL != "" {
				onFailure = func(ctx context.Context) {
					if err := notifications.BackupFailure(ctx, cfg.BackupWebhookURL); err != nil {
						logger.Error("Scheduled backup failure notification could not be delivered")
					}
				}
			}
			cfg.Backup.Maintain(backupCtx, cfg.DataDir, version, cfg.CredentialsKeyFile, logger, onFailure)
		}()
		defer func() { stopBackup(); <-backupDone }()
	}
	notifications.SetCapacityObserver(registry.Capacity)
	notifications.SetAllowanceObserver(registry.AllowanceAlerts)
	notifications.Start(ctx)
	return serve(ctx, logger, cfg.Addr, server.WithMetrics(server.NewMulti(db, authentication, tenancy, cfg.PublicURL, registry.Handler), cfg.MetricsToken, telemetry, registry.Pressure))
}

func serve(ctx context.Context, logger *slog.Logger, addr string, handler http.Handler) error {
	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		BaseContext:       func(net.Listener) context.Context { return ctx },
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
	failed := make(chan error, 1)
	go func() { failed <- srv.ListenAndServe() }()
	logger.Info("SubLane starting", "address", addr, "version", version)
	select {
	case err := <-failed:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdown); err != nil {
			_ = srv.Close()
			return err
		}
	}
	logger.Info("SubLane stopped")
	return nil
}
