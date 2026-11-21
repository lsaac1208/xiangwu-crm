package main

// Test-only, deterministic large fixture. No model request is issued here and
// no production business connection is accepted as its import destination.
import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/lsaac128/kxone/internal/agent"
	"gorm.io/gorm"
)

const liveStressVersion = "agent-stress-fixture-v1"

func loadLiveStressFixture(t *testing.T) (liveFixture, string) {
	t.Helper()
	path := os.Getenv("KXONE_AGENT_LIVE_STRESS_FIXTURE")
	if path == "" {
		path = "../../docs/Agent-Stress-Fixture.json"
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var fixture liveFixture
	var manifest struct {
		Dataset     string `json:"dataset"`
		Fingerprint struct {
			ContentSHA256 string `json:"content_sha256"`
		} `json:"fingerprint"`
	}
	if json.Unmarshal(raw, &fixture) != nil || json.Unmarshal(raw, &manifest) != nil || fixture.Version != liveStressVersion || manifest.Dataset != "stress-v1" || len(manifest.Fingerprint.ContentSHA256) != 64 {
		t.Fatal("unrecognized stress fixture; no cloud call is allowed")
	}
	fixture.ContentHash = manifest.Fingerprint.ContentSHA256
	fixture.Tables = map[string][]map[string]any{"crm_wecom_user_bindings": {{"wecom_userid": "stress-sales-a", "employee_name": "压力验收员工", "role": "sales", "status": "active", "password_hash": "MUST_NOT_SEND_PASSWORD_SENTINEL"}}}
	hash := sha256.Sum256(raw)
	return fixture, hex.EncodeToString(hash[:])
}

func importLiveStressFixture(t *testing.T, db *gorm.DB, fixture liveFixture, adminID int64) {
	t.Helper()
	if fixture.Version != liveStressVersion {
		t.Fatal("stress import requires its explicit version")
	}
	for _, table := range []string{"leads", "lead_followups", "lead_dispatch_records", "lead_appointments", "lead_activity_records", "lead_reminder_records"} {
		var count int64
		if err := db.Table(table).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("stress import requires an empty isolated %s table", table)
		}
	}
	importLiveFixture(t, db, fixture, adminID)
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`WITH RECURSIVE n(v) AS (SELECT 1 UNION ALL SELECT v+1 FROM n WHERE v<10001)
INSERT INTO leads(id,lead_no,customer_name,source_channel,status,business_stage,ownership_state,result_status,current_owner_user_id,current_owner_role,owner_user_id,star_rating,deal_amount,refund_amount,payment_status,created_at,updated_at,current_owner_started_at,first_followup_at,current_owner_first_followup_at,last_followup_at,converted_at)
SELECT v,printf('STRESS-%05d',v),'压力测试客户'||v,CASE WHEN v%2=1 THEN 'A' ELSE 'B' END,CASE WHEN v%2=1 THEN 'converted' ELSE 'following' END,CASE WHEN v%2=1 THEN 'finished' ELSE 'invitation' END,'private',CASE WHEN v%2=1 THEN 'converted' ELSE 'active' END,'stress-sales-a','inviter','stress-sales-a',3,v%2,0,'unpaid','2026-10-08T09:00:00+08:00','2026-10-08T10:00:00+08:00','2026-10-08T09:00:00+08:00','2026-10-08T09:10:00+08:00','2026-10-08T09:10:00+08:00','2026-10-08T09:14:00+08:00',CASE WHEN v%2=1 THEN '2026-10-08T10:00:00+08:00' ELSE NULL END FROM n`).Error; err != nil {
			return err
		}
		return tx.Exec(`WITH RECURSIVE n(v) AS (SELECT 1 UNION ALL SELECT v+1 FROM n WHERE v<50005)
INSERT INTO lead_followups(id,lead_id,sales_user_id,followup_result,business_stage,actor_role,star_rating,remark,is_valid,created_at)
SELECT v,((v-1)/5)+1,'stress-sales-a','connected','invitation','inviter',3,'压力测试跟进',1,printf('2026-10-08T09:%02d:00+08:00',10+((v-1)%5)) FROM n`).Error
	}); err != nil {
		t.Fatal(err)
	}
	verifyLiveStressFixture(t, db, fixture)
}

func verifyLiveStressFixture(t *testing.T, db *gorm.DB, fixture liveFixture) {
	t.Helper()
	checks := []struct {
		query string
		want  int64
	}{
		{"SELECT count(*) FROM leads", 10001},
		{"SELECT count(*) FROM lead_followups", 50005},
		{"SELECT CAST(ROUND(SUM(deal_amount-refund_amount)*100) AS INTEGER) FROM leads", 500100},
		{"SELECT count(*) FROM leads WHERE source_channel='A' AND status='converted' AND business_stage='finished' AND result_status='converted' AND deal_amount=1", 5001},
		{"SELECT count(*) FROM leads WHERE source_channel='B' AND status='following' AND business_stage='invitation' AND result_status='active' AND deal_amount=0", 5000},
		{"SELECT count(*) FROM (SELECT lead_id FROM lead_followups GROUP BY lead_id HAVING count(*)<>5)", 0},
		{"SELECT count(*) FROM lead_followups f LEFT JOIN leads l ON l.id=f.lead_id WHERE l.id IS NULL", 0},
		{"SELECT count(*) FROM leads WHERE deleted_at IS NOT NULL OR COALESCE(customer_mobile,'')<>'' OR COALESCE(customer_wechat,'')<>''", 0},
	}
	for _, check := range checks {
		var got int64
		if err := db.Raw(check.query).Scan(&got).Error; err != nil || got != check.want {
			t.Fatalf("stress independent oracle got=%d want=%d err=%v", got, check.want, err)
		}
	}
	hash := sha256.New()
	fmt.Fprintln(hash, liveStressVersion)
	rows, err := db.Raw("SELECT id,lead_no,customer_name,source_channel,status,CAST(ROUND(deal_amount*100) AS INTEGER),CAST(ROUND(refund_amount*100) AS INTEGER),current_owner_user_id,created_at FROM leads ORDER BY id").Rows()
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id, deal, refund int64
		var number, name, source, status, owner string
		var created time.Time
		if err := rows.Scan(&id, &number, &name, &source, &status, &deal, &refund, &owner, &created); err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(hash, "L\t%d\t%s\t%s\t%s\t%s\t%d\t%d\t%s\t%s\n", id, number, name, source, status, deal, refund, owner, created.Format(time.RFC3339))
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		t.Fatal(err)
	}
	rows, err = db.Raw("SELECT id,lead_id,sales_user_id,remark,created_at FROM lead_followups ORDER BY id").Rows()
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id, leadID int64
		var actor, remark string
		var created time.Time
		if err := rows.Scan(&id, &leadID, &actor, &remark, &created); err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(hash, "F\t%d\t%d\t%s\t%s\t%s\n", id, leadID, actor, remark, created.Format(time.RFC3339))
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		t.Fatal(err)
	}
	if hex.EncodeToString(hash.Sum(nil)) != fixture.ContentHash {
		t.Fatal("actual stress data does not match the versioned content fingerprint")
	}
}

func TestAgentLiveStressFixtureIndependentExpectations(t *testing.T) {
	fixture, _ := loadLiveStressFixture(t)
	var db *gorm.DB
	newTestRouterWithOptions(t, testRouterOptions{agentFixtureDB: &db, agentPrepareFixture: func(t *testing.T, db *gorm.DB) { importLiveStressFixture(t, db, fixture, 1) }})
	snapshot, err := agent.BuildSnapshot(t.Context(), db, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.Close()
	result, err := snapshot.Query(t.Context(), "SELECT (SELECT count(*) FROM leads),(SELECT count(*) FROM lead_followups),(SELECT sum(net_deal_amount_cents) FROM leads)", nil, 20)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(result.Rows)
	if string(raw) != "[[10001,50005,500100]]" {
		t.Fatalf("stress aggregate: %s", raw)
	}
	page, err := snapshot.Query(t.Context(), "SELECT id,customer_ref,source_channel FROM leads ORDER BY id", nil, 20)
	if err != nil {
		t.Fatal(err)
	}
	if page.TotalRows != 10001 || page.ReturnedRows != 20 || !page.Truncated {
		t.Fatalf("stress pagination: total=%d returned=%d truncated=%v", page.TotalRows, page.ReturnedRows, page.Truncated)
	}
	raw, _ = json.Marshal(page)
	if strings.Contains(string(raw), "压力测试客户") {
		t.Fatal("stress customer names leaked")
	}
}
