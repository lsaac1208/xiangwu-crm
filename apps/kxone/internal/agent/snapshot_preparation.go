package agent

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"runtime"
	"time"
)

type snapshotPreparationKey struct {
	directory   string
	credentials [32]byte
}

type snapshotPreparation struct {
	ready    chan struct{}
	cancel   context.CancelFunc
	waiters  int
	done     bool
	snapshot *Snapshot
	err      error
}

// Coalesce preparation only while it is in flight. Each run receives a private
// readonly handle, file name and query registry. Completed snapshots are never
// cached for a later request. Different credential redaction scopes cannot join.
func (s *Service) buildSnapshot(ctx context.Context, directory string) (*Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	credentials, _ := ctx.Value(snapshotCredentialsKey{}).([]string)
	raw, _ := json.Marshal(credentials)
	key := snapshotPreparationKey{directory: directory, credentials: sha256.Sum256(raw)}
	s.preparationMu.Lock()
	if s.preparations == nil {
		s.preparations = make(map[snapshotPreparationKey]*snapshotPreparation)
	}
	if s.querySlot == nil {
		capacity := 1
		if runtime.GOMAXPROCS(0) > 2 {
			capacity = 2
		}
		s.querySlot = make(chan struct{}, capacity)
	}
	job := s.preparations[key]
	if job == nil {
		// A single caller's cancellation must not cancel another caller. The
		// final departing waiter cancels the job; this independent deadline is
		// an additional bound if a caller disappears unexpectedly.
		limit := s.cfg.Timeout
		if limit <= 0 {
			limit = 180 * time.Second
		}
		buildCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), limit)
		job = &snapshotPreparation{ready: make(chan struct{}), cancel: cancel}
		s.preparations[key] = job
		go s.prepareSnapshot(buildCtx, directory, key, job)
	}
	job.waiters++
	s.preparationMu.Unlock()
	select {
	case <-ctx.Done():
	case <-job.ready:
	}
	s.preparationMu.Lock()
	defer s.preparationMu.Unlock()
	defer func() {
		job.waiters--
		if job.waiters == 0 {
			job.cancel()
			if s.preparations[key] == job {
				delete(s.preparations, key)
			}
			if job.done && job.snapshot != nil {
				_ = job.snapshot.Close()
			}
		}
	}()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if job.err != nil {
		return nil, job.err
	}
	snapshot, err := job.snapshot.clone(ctx, directory)
	if err == nil {
		snapshot.querySlot = s.querySlot
	}
	return snapshot, err
}

func (s *Service) prepareSnapshot(ctx context.Context, directory string, key snapshotPreparationKey, job *snapshotPreparation) {
	snapshot, err := s.buildPreparedSnapshot(ctx, directory)
	s.preparationMu.Lock()
	defer s.preparationMu.Unlock()
	job.snapshot, job.err, job.done = snapshot, err, true
	if s.preparations[key] == job {
		delete(s.preparations, key)
	}
	close(job.ready)
	job.cancel()
	if job.waiters == 0 && snapshot != nil {
		_ = snapshot.Close()
	}
}

func (s *Service) buildPreparedSnapshot(ctx context.Context, directory string) (snapshot *Snapshot, err error) {
	// Preparation now runs outside the request goroutine's panic boundary.
	// Preserve that boundary and always release the serialized build slot.
	defer func() {
		if recover() != nil {
			err = errors.New("分析快照构建异常，请重试")
		}
	}()
	select {
	case s.snapshotSlot <- struct{}{}:
		defer func() { <-s.snapshotSlot }()
		return analysisWork(ctx, func(ctx context.Context) (*Snapshot, error) {
			return BuildSnapshot(ctx, s.db, directory)
		})
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
