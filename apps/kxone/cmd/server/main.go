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

	"github.com/gin-gonic/gin"
	"github.com/lsaac128/kxone/internal/adapter/cache"
	"github.com/lsaac128/kxone/internal/adapter/repo"
	"github.com/lsaac128/kxone/internal/adapter/wecom"
	"github.com/lsaac128/kxone/internal/agent"
	"github.com/lsaac128/kxone/internal/config"
	"github.com/lsaac128/kxone/internal/runtime"
	"github.com/lsaac128/kxone/internal/security"
	httpTransport "github.com/lsaac128/kxone/internal/transport/http"
	workerTransport "github.com/lsaac128/kxone/internal/transport/worker"
	"github.com/lsaac128/kxone/internal/usecase"
	"github.com/lsaac128/kxone/pkg/logger"
	"github.com/lsaac128/kxone/pkg/metrics"
	"gorm.io/gorm"
)

func main() {
	// 加载配置
	cfg := config.Load()

	// 初始化结构化日志（在配置加载后立即执行，让后续所有日志走 slog）
	logger.Init(cfg.Server.Env)
	slog.Info("starting KXOne server", "port", cfg.Server.Port, "env", cfg.Server.Env, "db", cfg.Database.Driver)

	if err := cfg.ValidateProductionSecrets(); err != nil {
		slog.Error("invalid production config", "err", err)
		os.Exit(1)
	}

	// 初始化数据库连接
	db, err := repo.NewDB(&cfg.Database)
	if err != nil {
		slog.Error("failed to initialize database", "err", err)
		os.Exit(1)
	}
	slog.Info("database connected", "driver", cfg.Database.Driver)

	if cfg.Database.AutoMigrate {
		if err := repo.AutoMigrate(db); err != nil {
			slog.Error("database AutoMigrate failed", "err", err)
			os.Exit(1)
		}
		slog.Info("database AutoMigrate completed")
	} else {
		slog.Info("database AutoMigrate disabled")
	}
	if err := repo.RunMigrations(db, cfg.Database.MigrationsDir); err != nil {
		slog.Error("versioned database migrations failed", "err", err)
		os.Exit(1)
	}
	slog.Info("versioned database migrations completed")

	// 初始化 Redis 连接
	redisAddr := fmt.Sprintf("%s:%d", cfg.Redis.Host, cfg.Redis.Port)
	redisCache := cache.NewRedisCache(redisAddr, cfg.Redis.Password, cfg.Redis.DB)
	if err := redisCache.Ping(context.Background()); err != nil {
		if cfg.Server.Env == "prod" {
			slog.Error("Redis connection failed", "err", err)
			os.Exit(1)
		}
		slog.Warn("Redis connection failed; idempotency features may be degraded", "err", err)
	} else {
		slog.Info("Redis connected")
	}

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

	// 初始化仓储层
	leadRepo := repo.NewGormLeadRepository(db).
		WithFieldProtector(fieldProtector).
		WithMobileHasher(mobileHasher)
	dispatchRepo := repo.NewGormDispatchRecordRepository(db)
	followupRepo := repo.NewGormFollowupRepository(db)
	userBindingRepo := repo.NewGormUserBindingRepository(db)
	leadUIPreferenceRepo := repo.NewGormLeadUIPreferenceRepository(db)
	if err := repo.EnsureBootstrapAdmin(context.Background(), db, cfg.Account.BootstrapUsername, cfg.Account.BootstrapPassword, cfg.Server.Env); err != nil {
		slog.Error("failed to prepare CRM administrator account", "err", err)
		os.Exit(1)
	}
	settingRepo := repo.NewGormSystemSettingRepository(db).WithFieldProtector(fieldProtector)
	channelIntegrationRepo := repo.NewGormChannelIntegrationRepository(db).WithFieldProtector(fieldProtector)
	leadPoolRepo := repo.NewGormLeadPoolRepository(db)
	leadReminderRepo := repo.NewGormLeadReminderRepository(db)
	groupRobotNotificationRepo := repo.NewGormGroupRobotNotificationRepository(db)
	leadActivityRepo := repo.NewGormLeadActivityRepository(db)
	appointmentRepo := repo.NewGormAppointmentRepository(db)
	closerAvailabilityRepo := repo.NewGormCloserAvailabilityRepository(db)
	txManager := repo.NewGormTransactionManager(db).WithLeadSecurity(fieldProtector, mobileHasher)
	runtimeSettings := runtime.NewDatabaseRuntimeSettingsWithWeComConfig(settingRepo, wecom.Config{})

	// 初始化 Worker 任务客户端
	workerClient := workerTransport.NewClient(redisAddr, cfg.Redis.Password, cfg.Redis.DB)
	slog.Info("Worker client initialized")

	// 初始化用例层（注入 worker client）
	createLeadUseCase := usecase.NewCreateLeadUseCase(leadRepo, nil).WithIdempotencySecret(cfg.MobileHashKeyOrFallback()).
		WithRuntimeSettings(runtimeSettings).
		WithRoleAwareCreation(userBindingRepo, dispatchRepo, leadReminderRepo, txManager)
	assignUseCase := usecase.NewAssignLeadUseCase(leadRepo, dispatchRepo, nil, nil, redisCache, nil).
		WithRuntimeSettings(runtimeSettings).
		WithUserBindingRepository(userBindingRepo).
		WithGroupRobotNotifications(groupRobotNotificationRepo).
		WithTransactionManager(txManager)
	reassignUseCase := usecase.NewReassignLeadUseCase(leadRepo, dispatchRepo, nil, nil).
		WithRuntimeSettings(runtimeSettings).
		WithUserBindingRepository(userBindingRepo).
		WithTransactionManager(txManager)
	claimUseCase := usecase.NewClaimLeadUseCase(leadRepo, nil, nil, redisCache, nil).
		WithDispatchHistory(dispatchRepo, txManager).
		WithStage3Workflow(runtimeSettings, runtimeSettings, leadReminderRepo)
	completeLeadUseCase := usecase.NewCompleteLeadUseCase(leadRepo).
		WithWorkflow(runtimeSettings).
		WithAudit(leadActivityRepo, txManager)
	createFollowupUseCase := usecase.NewCreateFollowupUseCase(leadRepo, followupRepo).
		WithIdempotencySecret(cfg.MobileHashKeyOrFallback()).
		WithTransactionManager(txManager).
		WithStage3Workflow(runtimeSettings, runtimeSettings, leadPoolRepo, leadReminderRepo)
	leadPoolUseCase := usecase.NewLeadPoolUseCase(leadRepo, leadPoolRepo, runtimeSettings, runtimeSettings).
		WithDependencies(userBindingRepo, txManager).
		WithGroupRobotNotifications(groupRobotNotificationRepo)
	appointmentUseCase := usecase.NewAppointmentUseCase(appointmentRepo, closerAvailabilityRepo, leadRepo, userBindingRepo, txManager, runtimeSettings, runtimeSettings).
		WithStage5Delivery(runtimeSettings, leadActivityRepo, leadReminderRepo)
	availabilityUseCase := usecase.NewAvailabilityUseCase(closerAvailabilityRepo, userBindingRepo, runtimeSettings)
	negotiationUseCase := usecase.NewNegotiationUseCase(leadRepo, userBindingRepo, leadActivityRepo, txManager)
	batchLeadsUseCase := usecase.NewBatchLeadsUseCase(txManager)
	profileUseCase := usecase.NewUpdateLeadProfileUseCase(leadRepo, leadActivityRepo).WithTransactionManager(txManager)
	getLeadUseCase := usecase.NewGetLeadUseCase(leadRepo, followupRepo, dispatchRepo).
		WithUserBindingRepository(userBindingRepo).
		WithActivityRepository(leadActivityRepo)
	listLeadsUseCase := usecase.NewListLeadsUseCase(leadRepo).
		WithDispatchRecordRepository(dispatchRepo).
		WithUserBindingRepository(userBindingRepo)
	getSLAStatsUseCase := usecase.NewGetSLAStatsUseCase(leadRepo)
	getDashboardStatsUseCase := usecase.NewGetDashboardStatsUseCase(leadRepo, followupRepo).
		WithUserBindingRepository(userBindingRepo).
		WithRuntimeSettings(runtimeSettings).
		WithDualStageRepositories(appointmentRepo, leadPoolRepo)
	getWorkbenchUseCase := usecase.NewGetWorkbenchUseCase(leadRepo, appointmentRepo)
	recommendSalesUseCase := usecase.NewRecommendSalesUseCase(leadRepo, followupRepo).
		WithUserBindingRepository(userBindingRepo)
	autoAssignLeadUseCase := usecase.NewAutoAssignLeadUseCase(leadRepo, followupRepo, recommendSalesUseCase, assignUseCase)
	adminSettingsUseCase := usecase.NewAdminSettingsUseCase(settingRepo).
		WithGroupRobotNotifications(groupRobotNotificationRepo).
		WithLeadReminders(leadReminderRepo).
		WithTransactionManager(txManager)
	channelIntegrationsUseCase := usecase.NewChannelIntegrationsUseCase(channelIntegrationRepo, cfg.AdLead.GatewayPublicBaseURL)
	adminUserBindingsUseCase := usecase.NewAdminUserBindingsUseCase(userBindingRepo)
	leadUIPreferencesUseCase := usecase.NewLeadUIPreferencesUseCase(leadUIPreferenceRepo)
	slog.Info("use cases initialized")

	// 初始化 HTTP 处理器
	groupRobotNotifier := wecom.NewDatabaseGroupRobotNotifier(settingRepo).
		WithNotificationRepository(groupRobotNotificationRepo).
		WithScanWaker(workerClient)
	leadHandler := httpTransport.NewLeadHandler(createLeadUseCase, assignUseCase, reassignUseCase, claimUseCase, completeLeadUseCase, getLeadUseCase, listLeadsUseCase).
		WithNegotiationUseCase(negotiationUseCase).
		WithProfileUpdateUseCase(profileUseCase).
		WithBatchUseCase(batchLeadsUseCase).
		WithGroupRobotNotifier(groupRobotNotifier)
	leadLifecycleHandler := httpTransport.NewLeadLifecycleHandler(repo.NewGormLeadLifecycleRepository(db, runtimeSettings))
	leadPoolHandler := httpTransport.NewLeadPoolHandler(leadPoolUseCase)
	appointmentHandler := httpTransport.NewAppointmentHandler(appointmentUseCase, availabilityUseCase)
	workbenchHandler := httpTransport.NewWorkbenchHandler(getWorkbenchUseCase)
	adLeadHandler := httpTransport.NewAdLeadHandler(createLeadUseCase, leadRepo, cfg.AdLead.DefaultManagerUserID).
		WithXiangwuForms(os.Getenv("XIANGWU_FORM_KEY"), repo.NewGormXiangwuFormSubmissionRepository(db)).
		WithWebhookSecret(cfg.AdLead.WebhookSecret, time.Duration(cfg.AdLead.SignatureTolerance)*time.Second).
		WithTrustedGatewayIPs(cfg.AdLead.TrustedGatewayIPs).
		WithGatewayHMAC(cfg.AdLead.GatewayHMACSecret, time.Duration(cfg.AdLead.GatewayTolerance)*time.Second).
		WithPublicFormOrigins(append([]string{cfg.Server.PublicBaseURL}, cfg.Security.CORSAllowedOrigins...)...).
		WithGroupRobotNotifier(groupRobotNotifier).
		WithChannelIntegrations(channelIntegrationRepo)
	followupHandler := httpTransport.NewFollowupHandler(createFollowupUseCase, getLeadUseCase)
	authHandler := httpTransport.NewAuthHandler(
		userBindingRepo,
		cfg.JWT.Secret,
		cfg.JWT.AccessTokenExpiry,
		cfg.JWT.RefreshTokenExpiry,
	).WithLoginProtection(redisCache, cfg.JWT.Secret)
	slaHandler := httpTransport.NewSLAHandler(getSLAStatsUseCase, getDashboardStatsUseCase).WithManagementStats(usecase.NewManagementStatsUseCase(leadRepo, userBindingRepo))
	recommendationHandler := httpTransport.NewRecommendationHandler(recommendSalesUseCase)
	autoAssignHandler := httpTransport.NewAutoAssignHandler(autoAssignLeadUseCase)
	adminHandler := httpTransport.NewAdminHandler(adminSettingsUseCase, adminUserBindingsUseCase).WithGroupRobotSender(groupRobotNotifier).WithGroupRobotDiagnostics(groupRobotNotificationRepo)
	getLeadUseCase.WithNotificationDiagnostics(groupRobotNotificationRepo)
	leadUIPreferencesHandler := httpTransport.NewLeadUIPreferencesHandler(leadUIPreferencesUseCase)
	channelIntegrationHandler := httpTransport.NewChannelIntegrationHandler(channelIntegrationsUseCase)
	slog.Info("HTTP handlers initialized")
	agentConfig := agent.DefaultConfig()
	agentConfig.Enabled = cfg.Agent.Enabled
	agentConfig.BaseURL = cfg.Agent.BaseURL
	agentConfig.APIKey = cfg.Agent.APIKey
	agentConfig.Model = cfg.Agent.Model
	agentConfig.Directory = cfg.Agent.Directory
	// Phase two: fixed server metrics; free SQL analysis only by explicit opt-in.
	agentConfig.FixedMetrics = os.Getenv("KXONE_AGENT_FREE_ANALYSIS") != "1"
	agentConfig.MetricSelectPreferFallback = os.Getenv("KXONE_AGENT_METRIC_SELECT_MODEL") == "fallback"
	agentService, err := agent.NewService(db, agentConfig, userBindingRepo, fieldProtector, false)
	if err != nil {
		slog.Error("AI analysis initialization failed", "error", err)
		os.Exit(1)
	}
	defer agentService.Close()

	// 创建 HTTP 路由
	container := &serverContainer{
		cfg:             cfg,
		userBindingRepo: userBindingRepo,
		handlers: handlerContainer{
			agent:              agent.NewHandler(agentService).WithHistoryAccess(httpTransport.StreamAccessAudit(repo.NewGormAccessAuditRepository(db)), httpTransport.StreamSessionValid(userBindingRepo)),
			lead:               leadHandler,
			leadLifecycle:      leadLifecycleHandler,
			leadPool:           leadPoolHandler,
			appointment:        appointmentHandler,
			workbench:          workbenchHandler,
			adLead:             adLeadHandler,
			followup:           followupHandler,
			auth:               authHandler,
			sla:                slaHandler,
			recommendation:     recommendationHandler,
			autoAssign:         autoAssignHandler,
			admin:              adminHandler,
			leadUIPreferences:  leadUIPreferencesHandler,
			channelIntegration: channelIntegrationHandler,
		},
		accessAudits:   repo.NewGormAccessAuditRepository(db),
		healthChecker:  buildHealthChecker(db, redisCache),
		rateLimitCache: redisCache,
		workflowFlags:  runtimeSettings,
	}
	router := setupRouter(container)

	// 创建 HTTP 服务器
	addr := fmt.Sprintf("%s:%s", cfg.Server.Host, cfg.Server.Port)
	srv := &http.Server{
		Addr:              addr,
		Handler:           router,
		ReadTimeout:       cfg.Server.ReadTimeout,
		ReadHeaderTimeout: cfg.Server.HeaderTimeout,
		WriteTimeout:      cfg.Server.WriteTimeout,
		IdleTimeout:       cfg.Server.IdleTimeout,
		MaxHeaderBytes:    cfg.Server.MaxHeaderBytes,
	}

	// 启动服务器（goroutine）
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server listen failed", "err", err)
			os.Exit(1)
		}
	}()

	slog.Info("server started", "addr", addr)

	// 优雅关闭
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("shutting down server")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("server forced shutdown", "err", err)
		os.Exit(1)
	}

	slog.Info("server exited")
}

type serverContainer struct {
	accessAudits    *repo.GormAccessAuditRepository
	cfg             *config.Config
	userBindingRepo repo.UserBindingRepository
	handlers        handlerContainer
	healthChecker   healthChecker
	rateLimitCache  cache.Cache
	workflowFlags   runtime.WorkflowFeatureFlags
}

type handlerContainer struct {
	agent              *agent.Handler
	lead               *httpTransport.LeadHandler
	leadLifecycle      *httpTransport.LeadLifecycleHandler
	leadPool           *httpTransport.LeadPoolHandler
	appointment        *httpTransport.AppointmentHandler
	workbench          *httpTransport.WorkbenchHandler
	adLead             *httpTransport.AdLeadHandler
	followup           *httpTransport.FollowupHandler
	auth               *httpTransport.AuthHandler
	sla                *httpTransport.SLAHandler
	recommendation     *httpTransport.RecommendationHandler
	autoAssign         *httpTransport.AutoAssignHandler
	admin              *httpTransport.AdminHandler
	leadUIPreferences  *httpTransport.LeadUIPreferencesHandler
	channelIntegration *httpTransport.ChannelIntegrationHandler
}

type routeLimiters struct {
	adLead  gin.HandlerFunc
	auth    gin.HandlerFunc
	refresh gin.HandlerFunc
	public  gin.HandlerFunc
}

type healthChecker func(context.Context) (gin.H, bool)

func buildHealthChecker(db *gorm.DB, redisCache *cache.RedisCache) healthChecker {
	return func(ctx context.Context) (gin.H, bool) {
		details := gin.H{}
		healthy := true

		sqlDB, err := db.DB()
		if err != nil {
			details["database"] = "unavailable"
			healthy = false
		} else if err := sqlDB.PingContext(ctx); err != nil {
			details["database"] = "unhealthy"
			healthy = false
		} else {
			details["database"] = "ok"
		}

		if err := redisCache.Ping(ctx); err != nil {
			details["redis"] = "unhealthy"
			healthy = false
		} else {
			details["redis"] = "ok"
		}

		return details, healthy
	}
}

// setupRouter 配置路由
func setupRouter(container *serverContainer) *gin.Engine {
	cfg := container.cfg
	if cfg.Server.Env == "prod" {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.New()
	r.Use(httpTransport.RequestLogging(slog.Default()))
	if err := r.SetTrustedProxies(cfg.Server.TrustedProxies); err != nil {
		slog.Warn("invalid trusted proxies config", "err", err)
	}
	r.Use(httpTransport.SecurityHeaders(cfg.Security.CORSAllowedOrigins))
	r.Use(httpTransport.LimitRequestBody(cfg.Server.MaxBodyBytes))
	r.Use(httpTransport.LeadIntakeBodyLimit())
	r.Use(httpTransport.AccessAuditing(container.accessAudits))

	registerHealthRoute(r, container.healthChecker)
	registerMetricsRoutes(r, container)

	api := r.Group("/api")
	registerAPIRoutes(api, container)

	return r
}

func registerHealthRoute(r *gin.Engine, checker healthChecker) {
	r.GET("/health", func(c *gin.Context) {
		response := gin.H{
			"status": "ok",
			"time":   time.Now().Unix(),
		}
		status := http.StatusOK
		if checker != nil {
			ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
			defer cancel()
			checks, healthy := checker(ctx)
			response["checks"] = checks
			if !healthy {
				response["status"] = "degraded"
				status = http.StatusServiceUnavailable
			}
		}
		c.JSON(status, response)
	})
}

func authMiddleware(container *serverContainer) gin.HandlerFunc {
	if container.userBindingRepo != nil {
		return httpTransport.AuthMiddleware(container.cfg.JWT.Secret, container.userBindingRepo)
	}
	return httpTransport.AuthMiddleware(container.cfg.JWT.Secret)
}

func registerMetricsRoutes(r *gin.Engine, container *serverContainer) {
	r.GET("/metrics",
		authMiddleware(container),
		httpTransport.RequireRole("admin"),
		serveMetrics)
}

func serveMetrics(c *gin.Context) {
	globalMetrics := metrics.GetGlobalMetrics()
	if c.Query("format") == "prometheus" {
		// Delegate to the prometheus exposition handler; it sets Content-Type.
		globalMetrics.HTTPHandler().ServeHTTP(c.Writer, c.Request)
		return
	}
	c.JSON(http.StatusOK, globalMetrics.GetStats())
}

func registerAPIRoutes(api *gin.RouterGroup, container *serverContainer) {
	limiters := routeLimiters{
		adLead:  httpTransport.RedisRateLimit(container.rateLimitCache, 120, time.Minute, "ad_leads"),
		auth:    httpTransport.RedisRateLimit(container.rateLimitCache, 60, time.Minute, "auth_login"),
		refresh: httpTransport.RedisRateLimit(container.rateLimitCache, 300, time.Minute, "auth_refresh"),
		public:  httpTransport.RedisRateLimit(container.rateLimitCache, 60, time.Minute, "public"),
	}

	registerAdLeadRoutes(api, container.handlers.adLead, limiters.adLead)
	if container.cfg.AdLead.GatewayEnabled {
		registerGatewayLeadRoutes(api, container.handlers.adLead, limiters.adLead)
	}
	registerPublicLeadRoutes(api, container.handlers.adLead, limiters.public)
	registerAuthRoutes(api, container.handlers.auth, limiters.auth, limiters.refresh)
	registerUserRoutes(api, container)
	registerSalesUserRoutes(api, container)
	registerLeadRoutes(api, container)
	registerLeadPoolRoutes(api, container)
	registerAppointmentRoutes(api, container)
	registerWorkbenchRoutes(api, container)
	registerStatsRoutes(api, container)
	if container.handlers.agent != nil {
		group := api.Group("/agent")
		group.Use(authMiddleware(container))
		group.Use(httpTransport.RequireRole("manager", "admin"))
		container.handlers.agent.Register(group)
	}
	registerRecommendRoutes(api, container)
	registerAdminRoutes(api, container)
	registerChannelIntegrationRoutes(api, container)
}

func registerChannelIntegrationRoutes(api *gin.RouterGroup, container *serverContainer) {
	channels := api.Group("/channel-integrations")
	channels.Use(authMiddleware(container))
	{
		channels.GET("", httpTransport.RequireRole("manager", "admin"), container.handlers.channelIntegration.List)
		channels.POST("", httpTransport.RequireRole("admin"), container.handlers.channelIntegration.Create)
		channels.PUT("/:id", httpTransport.RequireRole("admin"), container.handlers.channelIntegration.Update)
		channels.DELETE("/:id", httpTransport.RequireRole("admin"), container.handlers.channelIntegration.Delete)
	}
}

func registerWorkbenchRoutes(api *gin.RouterGroup, container *serverContainer) {
	workbench := api.Group("/workbench")
	workbench.Use(authMiddleware(container))
	workbench.Use(httpTransport.RequireRole("sales", "inviter", "closer", "dispatcher", "manager", "admin"))
	workbench.GET("", container.handlers.workbench.Get)
}

func registerLeadPoolRoutes(api *gin.RouterGroup, container *serverContainer) {
	pool := api.Group("/public-pool")
	pool.Use(authMiddleware(container))
	{
		pool.GET("", httpTransport.RequireRole("sales", "inviter", "manager", "admin"), container.handlers.leadPool.List)
		pool.POST("/:id/claim", httpTransport.RequireRole("sales", "inviter", "manager"), container.handlers.leadPool.Claim)
		pool.POST("/:id/enter", httpTransport.RequireRole("manager", "admin"), container.handlers.leadPool.Enter)
	}
}

func registerAppointmentRoutes(api *gin.RouterGroup, container *serverContainer) {
	appointments := api.Group("/appointments")
	appointments.Use(authMiddleware(container))
	appointments.Use(httpTransport.RequireRole("sales", "inviter", "closer", "manager", "admin"))
	{
		appointments.POST("", container.handlers.appointment.Create)
		appointments.GET("", container.handlers.appointment.List)
		appointments.GET("/conflicts", httpTransport.RequireRole("manager", "admin"), container.handlers.appointment.Conflicts)
		appointments.GET("/:id", container.handlers.appointment.Get)
		appointments.POST("/:id/assign-closer", httpTransport.RequireRole("manager", "admin"), container.handlers.appointment.AssignCloser)
		appointments.POST("/:id/reschedule", container.handlers.appointment.Reschedule)
		appointments.POST("/:id/cancel", container.handlers.appointment.Cancel)
		appointments.POST("/:id/no-show", container.handlers.appointment.NoShow)
		appointments.POST("/:id/confirm-arrival", httpTransport.RequireRole("sales", "inviter", "manager", "admin"), container.handlers.appointment.ConfirmArrival)
	}
	availability := api.Group("/closer-availability")
	availability.Use(authMiddleware(container))
	availability.Use(httpTransport.RequireRole("closer", "manager", "admin"))
	{
		availability.GET("", container.handlers.appointment.ListAvailability)
		availability.POST("", httpTransport.RequireRole("manager", "admin"), container.handlers.appointment.CreateAvailability)
		availability.DELETE("/:id", httpTransport.RequireRole("manager", "admin"), container.handlers.appointment.DeleteAvailability)
	}
}

func registerGatewayLeadRoutes(api *gin.RouterGroup, handler *httpTransport.AdLeadHandler, limiter gin.HandlerFunc) {
	internal := api.Group("/internal/leads")
	internal.POST("/gateway", limiter, handler.ReceiveGateway)
	internal.POST("/xiaohongshu/verify-signature", limiter, handler.VerifyXiaohongshuSignature)
}

func registerPublicLeadRoutes(api *gin.RouterGroup, handler *httpTransport.AdLeadHandler, limiter gin.HandlerFunc) {
	public := api.Group("/public")
	public.POST("/leads/form", limiter, handler.ReceivePublicForm)
	forms := api.Group("/integrations/xiangwu")
	forms.POST("/website-form", limiter, handler.ReceiveXiangwuWebsiteForm)
	forms.POST("/lead-form", limiter, handler.ReceiveXiangwuLeadForm)
}

func registerAdLeadRoutes(api *gin.RouterGroup, handler *httpTransport.AdLeadHandler, limiter gin.HandlerFunc) {
	api.POST("/leads/baidu", limiter, handler.ReceiveBaidu)
	api.POST("/leads/tencent", limiter, handler.ReceiveTencent)
	api.POST("/leads/xiaohongshu", limiter, handler.ReceiveXiaohongshu)
	api.POST("/leads/douyin", limiter, handler.ReceiveDouyin)
	api.POST("/leads/form", limiter, handler.ReceiveForm)
	api.POST("/leads/manual", limiter, handler.ReceiveManual)
}

func registerAuthRoutes(api *gin.RouterGroup, handler *httpTransport.AuthHandler, limiter gin.HandlerFunc, refreshLimiters ...gin.HandlerFunc) {
	auth := api.Group("/auth")
	refreshLimiter := limiter
	if len(refreshLimiters) > 0 {
		refreshLimiter = refreshLimiters[0]
	}
	{
		auth.POST("/login", limiter, handler.HandleLogin)
		auth.POST("/refresh", refreshLimiter, handler.HandleRefreshToken)
		auth.POST("/logout", handler.HandleLogout)
	}
}

func registerUserRoutes(api *gin.RouterGroup, container *serverContainer) {
	user := api.Group("/user")
	user.Use(authMiddleware(container))
	{
		user.GET("/info", func(c *gin.Context) {
			userID, _ := c.Get("user_id")
			username, _ := c.Get("username")
			employeeName, _ := c.Get("employee_name")
			wecomUserID, _ := c.Get("wecom_userid")
			role, _ := c.Get("role")
			departmentID, _ := c.Get("department_id")
			workflowV2Enabled := container.workflowFlags != nil && container.workflowFlags.WorkflowV2Enabled(c.Request.Context())
			appointmentEnabled := container.workflowFlags != nil && container.workflowFlags.AppointmentEnabled(c.Request.Context())
			poolEnabled := container.workflowFlags != nil && container.workflowFlags.PoolEnabled(c.Request.Context())
			poolAutoReturnEnabled := container.workflowFlags != nil && container.workflowFlags.PoolAutoReturnEnabled(c.Request.Context())
			c.JSON(http.StatusOK, gin.H{
				"user_id":       userID,
				"username":      username,
				"employee_name": employeeName,
				"display_name":  employeeName,
				"account_id":    wecomUserID,
				"wecom_userid":  wecomUserID,
				"role":          role,
				"department_id": departmentID,
				"features": gin.H{
					"workflow_v2_enabled":      workflowV2Enabled,
					"appointment_enabled":      appointmentEnabled,
					"pool_enabled":             poolEnabled,
					"pool_auto_return_enabled": poolAutoReturnEnabled,
				},
			})
		})
		user.PUT("/password", container.handlers.auth.HandleChangePassword)
	}
	me := api.Group("/me")
	me.Use(authMiddleware(container))
	{
		me.GET("/lead-list-preferences", container.handlers.leadUIPreferences.GetPreference)
		me.PUT("/lead-list-preferences", container.handlers.leadUIPreferences.PutPreference)
		me.GET("/lead-views", container.handlers.leadUIPreferences.ListViews)
		me.POST("/lead-views", container.handlers.leadUIPreferences.CreateView)
		me.PATCH("/lead-views/:id", container.handlers.leadUIPreferences.UpdateView)
		me.DELETE("/lead-views/:id", container.handlers.leadUIPreferences.DeleteView)
	}
}

func registerSalesUserRoutes(api *gin.RouterGroup, container *serverContainer) {
	users := api.Group("/users")
	users.Use(authMiddleware(container))
	{
		users.GET("/sales", httpTransport.RequireRole("manager", "admin"), container.handlers.admin.ListSalesUsers)
		users.GET("/inviters", httpTransport.RequireRole("closer", "dispatcher", "manager", "admin"), container.handlers.admin.ListSalesUsers)
		users.GET("/lead-owner-filters", httpTransport.RequireRole("manager", "admin"), container.handlers.admin.ListLeadOwnerFilters)
		users.GET("/closers", httpTransport.RequireRole("manager", "admin"), container.handlers.admin.ListCloserUsers)
	}
}

func registerLeadRoutes(api *gin.RouterGroup, container *serverContainer) {
	handlers := container.handlers
	leads := api.Group("/leads")
	leads.Use(authMiddleware(container))
	{
		leads.POST("",
			httpTransport.RequireRole("sales", "inviter", "closer", "dispatcher", "manager", "admin"),
			handlers.lead.CreateLead)
		leads.POST("/:id/self-claim",
			httpTransport.RequireRole("manager"),
			handlers.lead.SelfClaimLead)
		leads.POST("/:id/assign",
			httpTransport.RequireRole("dispatcher", "manager", "admin"),
			handlers.lead.AssignLead)
		leads.POST("/batch-assign",
			httpTransport.RequireRole("dispatcher", "manager", "admin"),
			handlers.lead.BatchAssign)
		leads.POST("/batch-reassign",
			httpTransport.RequireRole("manager", "admin"),
			handlers.lead.BatchReassign)
		leads.POST("/:id/reassign",
			httpTransport.RequireRole("manager", "admin"),
			handlers.lead.ReassignLead)
		leads.POST("/:id/claim",
			httpTransport.RequireRole("sales", "inviter", "manager", "admin"),
			handlers.lead.ClaimLead)
		leads.POST("/:id/convert",
			httpTransport.RequireRole("sales", "inviter", "closer", "manager", "admin"),
			handlers.lead.ConvertLead)
		leads.PATCH("/:id/deal", httpTransport.RequireRole("sales", "inviter", "closer", "manager", "admin"), handlers.lead.UpdateDeal)
		leads.POST("/:id/restore-invalid", httpTransport.RequireRole("sales", "inviter", "closer", "manager", "admin"), handlers.lead.RestoreInvalid)
		leads.POST("/:id/close",
			httpTransport.RequireRole("sales", "inviter", "closer", "manager", "admin"),
			handlers.lead.CloseLead)
		leads.POST("/:id/reassign-closer",
			httpTransport.RequireRole("manager", "admin"),
			handlers.lead.ReassignCloser)
		leads.POST("/:id/followups",
			httpTransport.RequireRole("sales", "inviter", "closer", "manager", "admin"),
			handlers.followup.CreateFollowup)
		leads.PATCH("/:id/profile",
			httpTransport.RequireRole("sales", "inviter", "closer", "manager", "admin"),
			handlers.lead.UpdateProfile)
		leads.GET("", handlers.lead.ListLeads)
		leads.GET("/:id/recommend-sales",
			httpTransport.RequireRole("manager", "admin"),
			handlers.recommendation.RecommendForLead)
		leads.POST("/:id/auto-assign",
			httpTransport.RequireRole("manager", "admin"),
			handlers.autoAssign.AutoAssignLead)
		leads.GET("/:id", handlers.lead.GetLead)
		leads.DELETE("/:id", httpTransport.RequireRole("admin"), handlers.leadLifecycle.Delete)
	}
}

func registerStatsRoutes(api *gin.RouterGroup, container *serverContainer) {
	stats := api.Group("/stats")
	stats.Use(authMiddleware(container))
	stats.Use(httpTransport.RequireRole("manager", "admin"))
	{
		stats.GET("/dashboard", container.handlers.sla.GetDashboardStats)
		stats.GET("/management", container.handlers.sla.GetManagementStats)
		stats.GET("/supervision", container.handlers.sla.GetFollowupSupervision)
		stats.GET("/sla", container.handlers.sla.GetSLAStats)
	}
}

func registerRecommendRoutes(api *gin.RouterGroup, container *serverContainer) {
	recommend := api.Group("/recommend")
	recommend.Use(authMiddleware(container))
	recommend.Use(httpTransport.RequireRole("manager", "admin"))
	{
		recommend.POST("/sales", container.handlers.recommendation.RecommendSales)
	}
}

func registerAdminRoutes(api *gin.RouterGroup, container *serverContainer) {
	admin := api.Group("/admin")
	admin.Use(authMiddleware(container))
	admin.Use(httpTransport.RequireRole("admin"))
	{
		admin.GET("/access-audits", httpTransport.AccessAuditList(container.accessAudits))
		admin.GET("/metrics", serveMetrics)
		admin.GET("/settings", container.handlers.admin.ListSettings)
		admin.PUT("/settings", container.handlers.admin.UpdateSettings)
		admin.GET("/users", container.handlers.admin.ListUserBindings)
		admin.POST("/users", container.handlers.admin.CreateUserBinding)
		admin.PUT("/users/:id", container.handlers.admin.UpdateUserBinding)
		admin.POST("/group-robot/test", container.handlers.admin.SendGroupRobotTest)
		admin.GET("/group-robot/health", container.handlers.admin.GroupRobotHealth)
		admin.GET("/deleted-leads", container.handlers.leadLifecycle.ListDeleted)
		admin.POST("/deleted-leads/:id/restore", container.handlers.leadLifecycle.Restore)
	}
}
