// Package logger 提供基于 log/slog 的结构化日志基础设施。
//
// 使用方式：
//   - 进程启动时调用 Init 初始化（dev=文本格式，prod=JSON 格式）
//   - 直接使用 slog.Info / slog.Warn / slog.Error + 键值对字段
//   - 现有 log.Printf 调用在未迁移前继续有效，输出仍走 stderr
package logger

import (
	"log"
	"log/slog"
	"os"
)

// Init 根据环境初始化全局 slog handler。
// 应在 main() 最开始调用，在任何 log/slog 使用之前。
//
// env=="prod" 使用 JSON handler（结构化，适合日志采集系统解析）；
// 其他值使用 text handler（可读，适合本地开发）。
func Init(env string) {
	var handler slog.Handler
	opts := &slog.HandlerOptions{
		Level:     slog.LevelInfo,
		AddSource: env == "prod", // 生产日志附带调用源文件信息
	}
	if env == "prod" {
		handler = slog.NewJSONHandler(os.Stderr, opts)
	} else {
		handler = slog.NewTextHandler(os.Stderr, opts)
	}
	slog.SetDefault(slog.New(handler))

	// 将标准 log 包重定向到 slog，使未迁移的 log.Printf 调用也能被采集
	log.SetFlags(0)
	log.SetOutput(&slogWriter{logger: slog.Default()})
}

// slogWriter 将 log.Writer 接口桥接到 slog.Logger，使 log.Printf 走 slog 输出路径。
type slogWriter struct {
	logger *slog.Logger
}

func (w *slogWriter) Write(p []byte) (n int, err error) {
	msg := string(p)
	// 去掉末尾的换行符
	if len(msg) > 0 && msg[len(msg)-1] == '\n' {
		msg = msg[:len(msg)-1]
	}
	w.logger.Info(msg)
	return len(p), nil
}
