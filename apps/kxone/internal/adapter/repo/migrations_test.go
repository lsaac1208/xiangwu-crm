package repo

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/lsaac128/kxone/internal/config"
	"gorm.io/gorm"
)

func TestRunMigrationsAppliesSQLAndIsIdempotent(t *testing.T) {
	db := newMigrationTestDB(t)
	dir := t.TempDir()
	writeMigration(t, dir, "000001_create_widgets.sql", `
CREATE TABLE migration_widgets (
	id INTEGER PRIMARY KEY,
	name TEXT NOT NULL
);
INSERT INTO migration_widgets (id, name) VALUES (1, 'alpha;beta');
`)
	writeMigration(t, dir, "000002_insert_widget.sql", `
-- comments are allowed
INSERT INTO migration_widgets (id, name) VALUES (2, 'gamma');
`)

	if err := RunMigrations(db, dir); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	if err := RunMigrations(db, dir); err != nil {
		t.Fatalf("run migrations twice: %v", err)
	}

	var count int64
	if err := db.Table("migration_widgets").Count(&count).Error; err != nil {
		t.Fatalf("count widgets: %v", err)
	}
	if count != 2 {
		t.Fatalf("widget count = %d", count)
	}

	var applied int64
	if err := db.Model(&SchemaMigrationModel{}).Count(&applied).Error; err != nil {
		t.Fatalf("count applied migrations: %v", err)
	}
	if applied != 2 {
		t.Fatalf("applied migrations = %d", applied)
	}
}

func TestRunMigrationsRejectsChangedAppliedFile(t *testing.T) {
	db := newMigrationTestDB(t)
	dir := t.TempDir()
	path := writeMigration(t, dir, "000001_create_widgets.sql", `
CREATE TABLE migration_widgets (
	id INTEGER PRIMARY KEY
);
`)

	if err := RunMigrations(db, dir); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	if err := os.WriteFile(path, []byte("CREATE TABLE migration_widgets_changed (id INTEGER PRIMARY KEY);"), 0o644); err != nil {
		t.Fatalf("rewrite migration: %v", err)
	}
	if err := RunMigrations(db, dir); err == nil {
		t.Fatalf("expected checksum mismatch")
	}
}

func TestRunMigrationsRejectsNonPaddedVersion(t *testing.T) {
	db := newMigrationTestDB(t)
	dir := t.TempDir()
	writeMigration(t, dir, "1_create_widgets.sql", `
CREATE TABLE migration_widgets (
	id INTEGER PRIMARY KEY
);
`)

	if err := RunMigrations(db, dir); err == nil {
		t.Fatalf("expected invalid migration version")
	}
}

func TestRunProjectMigrationsOnEmptySQLiteDatabase(t *testing.T) {
	db := newMigrationTestDB(t)

	if err := RunMigrations(db, "../../../migrations"); err != nil {
		t.Fatalf("run project migrations on empty sqlite db: %v", err)
	}

	requiredTables := []string{
		"leads",
		"lead_dispatch_records",
		"lead_followups",
		"wecom_card_tasks",
		"wecom_callback_logs",
		"crm_wecom_user_bindings",
		"crm_wecom_user_binding_requests",
		"system_settings",
		"lead_appointments",
		"lead_appointment_events",
		"closer_availability",
		"lead_pool_records",
		"lead_activity_records",
		"lead_reminder_records",
		"group_robot_notifications",
		"crm_user_ui_preferences",
		"crm_lead_saved_views",
		"cms_entries",
		"cms_entry_drafts",
		"cms_revisions",
		"cms_review_records",
		"cms_user_roles",
		"cms_audit_logs",
		"cms_releases",
		"cms_release_items",
		"cms_media",
		"cms_media_variants",
		"cms_media_references",
		"cms_terms",
		"cms_draft_terms",
		"cms_revision_terms",
		"cms_draft_relations",
		"cms_revision_relations",
		"cms_release_relation_items",
		"cms_release_plans",
		"cms_release_plan_items",
	}
	for _, table := range requiredTables {
		var count int64
		if err := db.Raw("SELECT count(*) FROM sqlite_master WHERE type='table' AND name = ?", table).Scan(&count).Error; err != nil {
			t.Fatalf("query table %s: %v", table, err)
		}
		if count != 1 {
			t.Fatalf("table %s count = %d, want 1", table, count)
		}
	}

	if !db.Migrator().HasColumn(&LeadModel{}, "customer_mobile_hash") {
		t.Fatalf("leads.customer_mobile_hash missing after migrations")
	}
	if !db.Migrator().HasColumn(&LeadModel{}, "profile_version") {
		t.Fatalf("leads.profile_version missing after migrations")
	}
	for _, column := range []string{
		"business_stage",
		"ownership_state",
		"result_status",
		"assigned_inviter_user_id",
		"closer_user_id",
		"current_owner_user_id",
		"next_followup_due_at",
	} {
		if !db.Migrator().HasColumn(&LeadModel{}, column) {
			t.Fatalf("leads.%s missing after migrations", column)
		}
	}
	if !db.Migrator().HasColumn("cms_entry_drafts", "author_name") || !db.Migrator().HasColumn("cms_revisions", "published_at") {
		t.Fatalf("historical CMS article metadata columns missing after migrations")
	}
	if !db.Migrator().HasColumn("cms_releases", "plan_id") {
		t.Fatalf("cms_releases.plan_id missing after migrations")
	}

	if err := RunMigrations(db, "../../../migrations"); err != nil {
		t.Fatalf("rerun project migrations: %v", err)
	}
}

func TestDevStartupSchemaPathOnEmptySQLiteDatabase(t *testing.T) {
	db := newMigrationTestDB(t)

	if err := AutoMigrate(db); err != nil {
		t.Fatalf("automigrate empty sqlite db: %v", err)
	}
	if err := RunMigrations(db, "../../../migrations"); err != nil {
		t.Fatalf("run project migrations after automigrate: %v", err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("automigrate migrated sqlite db on second startup: %v", err)
	}

	if !db.Migrator().HasTable(&LeadModel{}) {
		t.Fatalf("leads table missing")
	}
	if !db.Migrator().HasColumn(&LeadModel{}, "customer_mobile_hash") {
		t.Fatalf("leads.customer_mobile_hash missing")
	}
	if !db.Migrator().HasTable("cms_entries") || !db.Migrator().HasTable("cms_revisions") {
		t.Fatalf("historical CMS tables missing")
	}
}

func newMigrationTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := NewDB(&config.DatabaseConfig{
		Driver:   "sqlite",
		Database: filepath.Join(t.TempDir(), "migration-test.db"),
		MaxConns: 4,
	})
	if err != nil {
		t.Fatalf("new db: %v", err)
	}
	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func writeMigration(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write migration %s: %v", name, err)
	}
	return path
}

func TestPaymentMigrationPreservesHistoricalConversions(t *testing.T) {
	db := newMigrationTestDB(t)
	if err := db.Exec("CREATE TABLE leads (id integer PRIMARY KEY, status text, deal_amount real)").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO leads VALUES (1, 'converted', 1234.56)").Error; err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile("../../../migrations/000025_lead_payment_details.sql")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeMigration(t, dir, "000025_lead_payment_details.sql", string(contents))
	if err := RunMigrations(db, dir); err != nil {
		t.Fatal(err)
	}
	if err := RunMigrations(db, dir); err != nil {
		t.Fatal(err)
	}
	var value struct {
		Status        string
		DealAmount    float64
		PaymentStatus string
		RefundAmount  float64
	}
	if err := db.Table("leads").First(&value).Error; err != nil {
		t.Fatal(err)
	}
	if value.Status != "converted" || value.DealAmount != 1234.56 || value.PaymentStatus != "" || value.RefundAmount != 0 {
		t.Fatalf("history changed: %+v", value)
	}
}

func TestRunMigrationsRejectsOutOfOrderBeforeWriting(t *testing.T) {
	db := newMigrationTestDB(t)
	dir := t.TempDir()
	writeMigration(t, dir, "000002_second.sql", "CREATE TABLE ordered_test (id INTEGER PRIMARY KEY);")
	if err := RunMigrations(db, dir); err != nil {
		t.Fatal(err)
	}
	writeMigration(t, dir, "000001_first.sql", "CREATE TABLE must_not_exist (id INTEGER PRIMARY KEY);")
	if err := RunMigrations(db, dir); err == nil {
		t.Fatal("accepted out-of-order migration")
	}
	if db.Migrator().HasTable("must_not_exist") {
		t.Fatal("wrote before validating migration order")
	}
}
