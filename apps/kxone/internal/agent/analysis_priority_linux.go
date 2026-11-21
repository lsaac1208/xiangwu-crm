//go:build linux

package agent

import (
	"fmt"
	"runtime"
	"syscall"
)

func lowerAnalysisThreadPriority() error {
	runtime.LockOSThread()
	// Do not unlock: returning this nice-19 thread to Go's shared scheduler would
	// let business requests inherit its priority. The owning goroutine exits and
	// runtime terminates the thread, including when setting priority fails.
	if err := syscall.Setpriority(syscall.PRIO_PROCESS, 0, 19); err != nil {
		return fmt.Errorf("后台分析优先级设置失败: %w", err)
	}
	return nil
}
