package agent

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestAnalysisWorkPreservesDeadlineErrorAndPanic(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	deadline, _ := ctx.Deadline()
	failure := errors.New("analysis failed")
	_, err := analysisWork(ctx, func(workCtx context.Context) (int, error) {
		actual, ok := workCtx.Deadline()
		if !ok || actual != deadline {
			t.Error("work received a fresh deadline")
		}
		return 0, failure
	})
	if !errors.Is(err, failure) {
		t.Fatalf("error was replaced: %v", err)
	}
	defer func() {
		if recover() != "analysis panic" {
			t.Error("panic did not reach the request recovery boundary")
		}
	}()
	_, _ = analysisWork(ctx, func(context.Context) (int, error) { panic("analysis panic") })
}
