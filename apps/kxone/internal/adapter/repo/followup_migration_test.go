package repo

import (
	"github.com/lsaac128/kxone/pkg/types"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFollowupMigrationKeepsOldRuntimeAndBusinessRowsCompatible(t *testing.T) {
	baseline := t.TempDir()
	files, err := filepath.Glob("../../../migrations/*.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if strings.HasPrefix(filepath.Base(file), "000036_") {
			continue
		}
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(baseline, filepath.Base(file)), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	db := newMigrationTestDB(t)
	if err := RunMigrations(db, baseline); err != nil {
		t.Fatal(err)
	}
	leads := NewGormLeadRepository(db)
	lead := &types.Lead{LeadNo: "migration-retains-business-row", CustomerName: "迁移验证样例", Status: types.LeadStatusPendingDispatch, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := leads.Create(t.Context(), lead); err != nil {
		t.Fatal(err)
	}
	before, err := leads.GetByID(t.Context(), lead.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := RunMigrations(db, "../../../migrations"); err != nil {
		t.Fatal(err)
	}
	after, err := leads.GetByID(t.Context(), lead.ID)
	if err != nil {
		t.Fatal(err)
	}
	if before.LeadNo != after.LeadNo || before.Version != after.Version || before.CustomerName != after.CustomerName {
		t.Fatal("additive migration changed business row")
	}
	var count int64
	if err := db.Model(&SchemaMigrationModel{}).Count(&count).Error; err != nil || count != 37 {
		t.Fatalf("migration count=%d err=%v", count, err)
	}
	if !db.Migrator().HasTable(&FollowupRequest{}) {
		t.Fatal("receipt table missing")
	}
	// The current migration runner explicitly permits newer additive migrations
	// to remain when a previous application's migration directory is restored.
	if err := RunMigrations(db, baseline); err != nil {
		t.Fatalf("application-only rollback blocked: %v", err)
	}
	if _, err := leads.GetByID(t.Context(), lead.ID); err != nil {
		t.Fatalf("old business read blocked: %v", err)
	}
}
