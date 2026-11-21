package worker

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/hibiken/asynq"
	"github.com/lsaac128/kxone/internal/config"
)

// Server 定时任务服务器
type Server struct {
	securityCleanup       bool
	server                *asynq.Server
	mux                   *asynq.ServeMux
	scheduler             *asynq.Scheduler
	groupRobotRuleHandler *GroupRobotRuleHandler
}

// NewServer 创建定时任务服务器
func NewServer(cfg *config.Config, handler *TaskHandler) *Server {
	redisAddr := fmt.Sprintf("%s:%d", cfg.Redis.Host, cfg.Redis.Port)

	redisOpt := asynq.RedisClientOpt{
		Addr:     redisAddr,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	}

	concurrency := cfg.Server.WorkerConcurrency
	if concurrency <= 0 {
		concurrency = 10
	}

	queues := map[string]int{"cards": 6, "default": 3, "low": 1}
	// 创建服务器
	srv := asynq.NewServer(
		redisOpt,
		asynq.Config{
			Concurrency: concurrency,
			Queues:      queues,
		},
	)

	// 创建路由（注册任务处理器）
	mux := asynq.NewServeMux()
	if handler != nil {
		mux.HandleFunc(TypeScanFollowupSLA, handler.HandleScanFollowupSLA)
	}

	// 创建定时调度器
	scheduler := asynq.NewScheduler(
		redisOpt,
		&asynq.SchedulerOpts{
			Location: nil, // UTC
		},
	)

	return &Server{
		server:    srv,
		mux:       mux,
		scheduler: scheduler,
	}
}

// WithGroupRobotRules enables the durable group-robot rule scanner. Legacy
// application-card handlers stay unregistered: all restored rule notices are
// delivered through the shared enterprise-WeChat group webhook.
func (s *Server) WithGroupRobotRules(handler *GroupRobotRuleHandler) *Server {
	if handler != nil {
		s.groupRobotRuleHandler = handler
		s.mux.HandleFunc(TypeScanGroupRobotRules, handler.HandleScan)
	}
	return s
}

// RegisterCronJobs 注册定时任务
func (s *Server) RegisterCronJobs() error {
	if s.securityCleanup {
		if _, err := s.scheduler.Register("0 3 * * *", asynq.NewTask(TypeSecurityCleanup, nil), asynq.Queue("low")); err != nil {
			return fmt.Errorf("register security metadata cleanup: %w", err)
		}
	}
	if s.groupRobotRuleHandler != nil {
		if _, err := s.scheduler.Register("@every 1m", asynq.NewTask(TypeScanGroupRobotRules, nil), asynq.Queue("default")); err != nil {
			return fmt.Errorf("register group robot rule scan: %w", err)
		}
	}
	slog.Info("worker cron jobs registered", "wecom_card_jobs", false, "group_robot_rules", s.groupRobotRuleHandler != nil)

	return nil
}

// Start 启动服务器
func (s *Server) Start() error {
	// 启动调度器
	if err := s.scheduler.Start(); err != nil {
		return fmt.Errorf("启动调度器失败: %w", err)
	}

	slog.Info("worker scheduler started")

	// 启动任务处理服务器（阻塞）
	return s.server.Run(s.mux)
}

// Shutdown 优雅关闭
func (s *Server) Shutdown(ctx context.Context) error {
	s.scheduler.Shutdown()
	s.server.Shutdown()
	return nil
}
