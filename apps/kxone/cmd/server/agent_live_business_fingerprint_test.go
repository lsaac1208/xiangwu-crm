package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/lsaac128/kxone/internal/agent"
	"gorm.io/gorm"
)

func TestAgentLiveBusinessFingerprintPreservesRowsAndExcludesAgentState(t *testing.T) {
	fixture, _ := loadLiveFixture(t)
	var db *gorm.DB
	newTestRouterWithOptions(t, testRouterOptions{agentFixtureDB: &db, agentPrepareFixture: func(t *testing.T, db *gorm.DB) { importLiveFixture(t, db, fixture, 1) }})
	before, err := captureLiveBusinessFingerprint(t.Context(), db)
	if err != nil {
		t.Fatal(err)
	}
	if len(before.Tables) != 17 || before.SHA256 == "" {
		t.Fatal("business fingerprint does not cover the explicit seventeen-table allowlist")
	}
	if err := db.Create(&agent.Conversation{ID: "fingerprint-ai-only", AccountID: 1, Title: "must not affect business fingerprint"}).Error; err != nil {
		t.Fatal(err)
	}
	afterAgent, err := captureLiveBusinessFingerprint(t.Context(), db)
	if err != nil {
		t.Fatal(err)
	}
	if !compareLiveBusinessFingerprints(before, afterAgent).Consistent {
		t.Fatal("Agent-only persistence changed business fingerprint")
	}
	// Physical rewrites must not affect the canonical value/primary-key digest.
	if err := db.Exec("VACUUM").Error; err != nil {
		t.Fatal(err)
	}
	afterRewrite, err := captureLiveBusinessFingerprint(t.Context(), db)
	if err != nil || !compareLiveBusinessFingerprints(before, afterRewrite).Consistent {
		t.Fatal("physical SQLite rewrite changed canonical business values")
	}
	if err := db.Exec("UPDATE leads SET star_rating=4 WHERE id=1").Error; err != nil {
		t.Fatal(err)
	}
	afterBusiness, err := captureLiveBusinessFingerprint(t.Context(), db)
	if err != nil {
		t.Fatal(err)
	}
	comparison := compareLiveBusinessFingerprints(before, afterBusiness)
	if comparison.Consistent || len(comparison.ChangedTables) != 1 || comparison.ChangedTables[0] != "leads" {
		t.Fatal("same-row-count business mutation was not detected")
	}
	if before.Tables["leads"].Rows != afterBusiness.Tables["leads"].Rows {
		t.Fatal("counterexample must keep the row count unchanged")
	}
	encoded, err := json.Marshal(comparison)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"MUST_NOT_SEND_PASSWORD_SENTINEL", "13800138001", "测试客户一", "test_parent_0001", "key_ciphertext"} {
		if bytes.Contains(encoded, []byte(forbidden)) {
			t.Fatal("fingerprint evidence leaked a credential or raw fixture value")
		}
	}
	if !strings.Contains(string(encoded), "ai_conversations") || comparison.FullDatabaseProof {
		t.Fatal("excluded tables/full-database proof boundary not explicit")
	}
}

func TestAgentLiveBusinessFingerprintRecordsMissingAllowedTable(t *testing.T) {
	var db *gorm.DB
	newTestRouterWithOptions(t, testRouterOptions{agentFixtureDB: &db})
	if err := db.Exec("DROP TABLE IF EXISTS xiangwu_form_submissions").Error; err != nil {
		t.Fatal(err)
	}
	before, err := captureLiveBusinessFingerprint(t.Context(), db)
	if err != nil {
		t.Fatal(err)
	}
	if before.Tables["xiangwu_form_submissions"].Exists || !strings.Contains(strings.Join(before.MissingTables, ","), "xiangwu_form_submissions") {
		t.Fatal("missing table was presented as an existing empty table")
	}
	if err := db.Exec("CREATE TABLE xiangwu_form_submissions (id INTEGER PRIMARY KEY, lead_id INTEGER)").Error; err != nil {
		t.Fatal(err)
	}
	after, err := captureLiveBusinessFingerprint(t.Context(), db)
	if err != nil {
		t.Fatal(err)
	}
	diff := compareLiveBusinessFingerprints(before, after)
	if diff.Consistent || len(diff.ChangedTables) != 1 || diff.ChangedTables[0] != "xiangwu_form_submissions" || !after.Tables["xiangwu_form_submissions"].Exists {
		t.Fatal("new empty business table did not change structural evidence")
	}
}

func TestAgentLiveBusinessFingerprintRefusesNonFixtureDatabase(t *testing.T) {
	// The evidence helper cannot be pointed at a production database by an env
	// override: only the test router's temporary kxone-test.db is admissible.
	if err := validateLiveBusinessFingerprintPath("/var/lib/kxone/kxone.db"); err == nil {
		t.Fatal("production database path accepted")
	}
	if _, err := captureLiveBusinessFingerprint(context.Background(), nil); err == nil {
		t.Fatal("missing fixture database accepted")
	}
}
