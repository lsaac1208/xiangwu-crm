package agent

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
)

// The projection is immutable after BuildSnapshot closes its writer. A private
// hard link gives each run independent deletion and connection lifetimes,
// without repeating projection work or copying tens of megabytes. SQLite and
// its authorizer enforce readonly access. No result or query state is shared.
func (s *Snapshot) clone(ctx context.Context, directory string) (_ *Snapshot, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, errors.New("分析快照已关闭")
	}
	file, err := os.CreateTemp(directory, "snapshot-*.sqlite")
	if err != nil {
		return nil, err
	}
	path := file.Name()
	if err = file.Close(); err != nil {
		_ = os.Remove(path)
		return nil, err
	}
	if err = os.Remove(path); err != nil {
		return nil, err
	}
	if err = os.Link(s.path, path); err != nil {
		return nil, err
	}
	copy := &Snapshot{At: s.At, path: path, redactor: s.redactor,
		counts: s.counts, columns: s.columns, missing: s.missing,
		excluded: s.excluded, unmatchedEncrypted: s.unmatchedEncrypted,
		queries: make(map[string]snapshotQuery), sourceReadDuration: s.sourceReadDuration, sourceReadRows: s.sourceReadRows}
	defer func() {
		if err != nil {
			_ = copy.Close()
		}
	}()
	copy.db, err = sql.Open(analysisDriver, "file:"+filepath.ToSlash(path)+"?mode=ro&_busy_timeout=5000&_txlock=deferred")
	if err != nil {
		return nil, err
	}
	copy.db.SetMaxOpenConns(1)
	copy.db.SetMaxIdleConns(1)
	if err = copy.db.PingContext(ctx); err != nil {
		return nil, err
	}
	return copy, nil
}
