package agent

import (
	"context"
	"runtime"
)

type analysisWorkKey struct{}

// Keep background CPU work below business requests on small Linux hosts. The
// worker owns a locked OS thread until its goroutine exits; Go then destroys
// that thread, so a later business request cannot inherit its lower priority.
// The caller's context, including its existing deadline, is passed unchanged.
func analysisWork[T any](ctx context.Context, work func(context.Context) (T, error)) (T, error) {
	var zero T
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if runtime.GOOS != "linux" || runtime.GOMAXPROCS(0) > 2 || ctx.Value(analysisWorkKey{}) == true {
		return work(ctx)
	}
	type outcome struct {
		value      T
		err        error
		panicValue any
	}
	ready := make(chan outcome, 1)
	go func() {
		result := outcome{}
		defer func() { result.panicValue = recover(); ready <- result }()
		if result.err = ctx.Err(); result.err != nil {
			return
		}
		if result.err = lowerAnalysisThreadPriority(); result.err != nil {
			return
		}
		if result.err = ctx.Err(); result.err != nil {
			return
		}
		result.value, result.err = work(context.WithValue(ctx, analysisWorkKey{}, true))
	}()
	result := <-ready
	if result.panicValue != nil {
		panic(result.panicValue)
	}
	return result.value, result.err
}
