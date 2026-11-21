package main

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/lsaac128/kxone/internal/adapter/repo"
	"github.com/lsaac128/kxone/internal/adapter/wecom"
	"github.com/lsaac128/kxone/internal/config"
	"github.com/lsaac128/kxone/internal/runtime"
	"github.com/lsaac128/kxone/internal/security"
	"github.com/lsaac128/kxone/internal/transport/worker"
	"github.com/lsaac128/kxone/internal/usecase"
	"github.com/lsaac128/kxone/pkg/logger"
	"github.com/lsaac128/kxone/pkg/metrics"
)

func main() {
	cfg := config.Load()
	logger.Init(cfg.Server.Env)
	slog.Info("starting KXOne worker", "env", cfg.Server.Env, "db", cfg.Database.Driver)

	if err := cfg.ValidateProductionSecrets(); err != nil {
		slog.Error("invalid production config", "err", err)
		os.Exit(1)
	}

	db, err := repo.NewDB(&cfg.Database)
	if err != nil {
		slog.Error("failed to initialize database", "err", err)
		os.Exit(1)
	}
	slog.Info("database connected", "driver", cfg.Database.Driver)

	fieldProtector, err := security.BuildFieldProtector(cfg.Server.Env, cfg.Security.FieldEncryptionKey)
	if err != nil {
		slog.Error("invalid field encryption config", "err", err)
		os.Exit(1)
	}
	mobileHasher, err := security.BuildMobileHasher(cfg.MobileHashKeyOrFallback())
	if err != nil {
		slog.Error("invalid mobile hash config", "err", err)
		os.Exit(1)
	}

	leadRepo := repo.NewGormLeadRepository(db).
		WithFieldProtector(fieldProtector).
		WithMobileHasher(mobileHasher)
	settingRepo := repo.NewGormSystemSettingRepository(db).WithFieldProtector(fieldProtector)
	userBindingRepo := repo.NewGormUserBindingRepository(db)
	leadPoolRepo := repo.NewGormLeadPoolRepository(db)
	leadReminderRepo := repo.NewGormLeadReminderRepository(db)
	notificationRepo := repo.NewGormGroupRobotNotificationRepository(db)
	txManager := repo.NewGormTransactionManager(db).WithLeadSecurity(fieldProtector, mobileHasher)
	runtimeSettings := runtime.NewDatabaseRuntimeSettingsWithWeComConfig(settingRepo, wecom.Config{})
	leadPoolUseCase := usecase.NewLeadPoolUseCase(leadRepo, leadPoolRepo, runtimeSettings, runtimeSettings).
		WithDependencies(userBindingRepo, txManager).
		WithGroupRobotNotifications(notificationRepo)
	groupRobotSender := wecom.NewDatabaseGroupRobotNotifier(settingRepo).
		WithNotificationRepository(notificationRepo)
	groupRobotRuleHandler := worker.NewGroupRobotRuleHandler(
		leadRepo,
		leadReminderRepo,
		notificationRepo,
		userBindingRepo,
		runtimeSettings,
		groupRobotSender,
		leadPoolUseCase,
		cfg.Server.PublicBaseURL,
	)

	workerServer := worker.NewServer(cfg, nil).
		WithGroupRobotRules(groupRobotRuleHandler).WithSecurityCleanup(repo.NewGormAccessAuditRepository(db))
	metricsServer, err := startWorkerMetricsServer(cfg.Server.WorkerMetricsAddr)
	if err != nil {
		slog.Error("failed to start worker metrics server", "addr", cfg.Server.WorkerMetricsAddr, "err", err)
		os.Exit(1)
	}

	if err := workerServer.RegisterCronJobs(); err != nil {
		slog.Error("failed to register cron jobs", "err", err)
		os.Exit(1)
	}

	slog.Info("worker server starting")

	errChan := make(chan error, 1)
	go func() {
		if err := workerServer.Start(); err != nil {
			errChan <- err
		}
	}()

	slog.Info("worker server started")

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	workerFailed := false
	select {
	case <-quit:
		slog.Info("shutting down worker")
	case err := <-errChan:
		slog.Error("worker error", "err", err)
		workerFailed = true
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := workerServer.Shutdown(ctx); err != nil {
		slog.Error("worker forced shutdown", "err", err)
		os.Exit(1)
	}
	if metricsServer != nil {
		if err := metricsServer.Shutdown(ctx); err != nil {
			slog.Error("worker metrics server forced shutdown", "err", err)
		}
	}

	slog.Info("worker exited")
	if workerFailed {
		os.Exit(1)
	}
}

func startWorkerMetricsServer(addr string) (*http.Server, error) {
	if addr == "" {
		return nil, nil
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	mux.Handle("/metrics", metrics.GetGlobalMetrics().HTTPHandler())
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	srv := &http.Server{
		Addr:              listener.Addr().String(),
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
	go func() {
		if err := srv.Serve(listener); err != nil && err != http.ErrServerClosed {
			slog.Error("worker metrics server failed", "addr", srv.Addr, "err", err)
		}
	}()
	slog.Info("worker metrics server started", "addr", srv.Addr)
	return srv, nil
}
