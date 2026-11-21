//go:build linux

package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"syscall"
	"testing"
	"time"
)

func TestAnalysisWorkLowersOnlyItsDedicatedThread(t *testing.T) {
	previous := runtime.GOMAXPROCS(2)
	defer runtime.GOMAXPROCS(previous)
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	before, err := syscall.Getpriority(syscall.PRIO_PROCESS, 0)
	if err != nil {
		t.Fatal(err)
	}
	requestThread := syscall.Gettid()
	backgroundThread := 0
	_, err = analysisWork(context.Background(), func(ctx context.Context) (int, error) {
		if syscall.Gettid() == requestThread {
			t.Error("background work reused a request thread")
		}
		priority, priorityErr := syscall.Getpriority(syscall.PRIO_PROCESS, 0)
		if priorityErr != nil || priority != 1 {
			t.Errorf("background nice was not 19: kernel priority=%d err=%v", priority, priorityErr)
		}
		thread := syscall.Gettid()
		backgroundThread = thread
		_, nestedErr := analysisWork(ctx, func(context.Context) (int, error) {
			if syscall.Gettid() != thread {
				t.Error("nested work started another priority thread")
			}
			return 7, nil
		})
		return 7, nestedErr
	})
	if err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(time.Second); ; {
		_, statErr := os.Stat(fmt.Sprintf("/proc/self/task/%d", backgroundThread))
		if os.IsNotExist(statErr) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("low-priority thread was returned to the shared runtime")
		}
		time.Sleep(time.Millisecond)
	}
	after, err := syscall.Getpriority(syscall.PRIO_PROCESS, 0)
	if err != nil || before != after || syscall.Gettid() != requestThread {
		t.Fatalf("business thread priority changed: %d -> %d, %v", before, after, err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = analysisWork(cancelled, func(context.Context) (int, error) { t.Error("cancelled work started"); return 0, nil })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
}
