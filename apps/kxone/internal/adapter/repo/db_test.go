package repo

import (
	"path/filepath"
	"testing"

	"github.com/lsaac128/kxone/internal/config"
)

func TestNewDBConfiguresSQLiteForConcurrentWorkloads(t *testing.T) {
	db, err := NewDB(&config.DatabaseConfig{
		Driver:   "sqlite",
		Database: filepath.Join(t.TempDir(), "pragmas.db"),
		MaxConns: 20,
	})
	if err != nil {
		t.Fatalf("new db: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("sql db: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	if got := sqlDB.Stats().MaxOpenConnections; got != 4 {
		t.Fatalf("max open connections = %d, want 4", got)
	}
	var journalMode string
	if err := db.Raw("PRAGMA journal_mode").Scan(&journalMode).Error; err != nil {
		t.Fatalf("journal mode: %v", err)
	}
	if journalMode != "wal" {
		t.Fatalf("journal mode = %q, want wal", journalMode)
	}
	var busyTimeout, foreignKeys int
	if err := db.Raw("PRAGMA busy_timeout").Scan(&busyTimeout).Error; err != nil {
		t.Fatalf("busy timeout: %v", err)
	}
	if err := db.Raw("PRAGMA foreign_keys").Scan(&foreignKeys).Error; err != nil {
		t.Fatalf("foreign keys: %v", err)
	}
	if busyTimeout != 5000 || foreignKeys != 1 {
		t.Fatalf("busy_timeout=%d foreign_keys=%d, want 5000/1", busyTimeout, foreignKeys)
	}
}
