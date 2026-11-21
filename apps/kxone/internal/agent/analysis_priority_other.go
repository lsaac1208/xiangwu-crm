//go:build !linux

package agent

func lowerAnalysisThreadPriority() error { return nil }
