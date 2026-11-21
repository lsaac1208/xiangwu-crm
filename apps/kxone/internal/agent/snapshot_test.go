package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lsaac128/kxone/internal/adapter/repo"
	"github.com/lsaac128/kxone/internal/security"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func snapshotFixtureDB(t *testing.T) *gorm.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "business.sqlite")
	db, err := gorm.Open(sqlite.Open(path+"?_journal_mode=WAL&_busy_timeout=5000&_txlock=immediate"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	err = db.AutoMigrate(&repo.LeadModel{}, &repo.LeadFollowupModel{}, &repo.LeadDispatchRecordModel{}, &repo.LeadAppointmentModel{}, &repo.LeadAppointmentEventModel{}, &repo.CloserAvailabilityModel{}, &repo.LeadPoolRecordModel{}, &repo.LeadActivityRecordModel{}, &repo.LeadReminderRecordModel{}, &repo.WeComUserBindingModel{}, &repo.ChannelIntegrationModel{}, &repo.ChannelIntegrationAuditModel{}, &repo.GroupRobotNotificationModel{}, &repo.WeComCardTaskModel{}, &repo.WeComCallbackLogModel{}, &repo.LeadDeletionEventModel{}, &repo.XiangwuFormSubmission{})
	if err != nil {
		t.Fatal(err)
	}
	pooled, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	pooled.SetMaxOpenConns(4)
	t.Cleanup(func() { pooled.Close() })
	return db
}

func createSnapshotLead(t *testing.T, db *gorm.DB, id int64) {
	t.Helper()
	now := time.Date(2026, 10, 7, 16, 0, 0, 0, time.UTC)
	due := now.Add(time.Hour)
	lead := repo.LeadModel{ID: id, LeadNo: fmt.Sprintf("CRM-%d", id), CustomerName: "张小明", CustomerMobile: "13912345678", CustomerWechat: "parent_abc_123", SourceChannel: "员工丁抖音（员工乙）", Status: "following", BusinessStage: "invitation", OwnershipState: "private", ResultStatus: "active", CurrentOwnerUserID: "inviter-a", CurrentOwnerRole: "inviter", DealAmount: 100.01, RefundAmount: 0.01, PaymentStatus: "paid", CreatedAt: now, UpdatedAt: now, CurrentOwnerStartedAt: &now, CurrentOwnerFirstFollowupAt: &due, NextFollowupDueAt: &due}
	if err := db.Create(&lead).Error; err != nil {
		t.Fatal(err)
	}
}

func TestSnapshotPhysicalProjectionPrivacyAndHistory(t *testing.T) {
	db := snapshotFixtureDB(t)
	createSnapshotLead(t, db, 1)
	now := time.Date(2026, 10, 7, 16, 10, 0, 0, time.UTC)
	if err := db.Create(&repo.WeComUserBindingModel{ID: 1, CRMUserID: 11, WeComUserID: "inviter-a", EmployeeName: "员工乙", Role: "inviter", Status: "active", PasswordHash: "SECRET_HASH_MUST_NOT_COPY", CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&repo.LeadFollowupModel{ID: 1, LeadID: 1, SalesUserID: "inviter-a", Remark: "张小明家长希望下周到访，１３９ １２３４ ５６７８，微信：parent_abc_123。联系人：刘大勇", IsValid: true, CreatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&repo.LeadActivityRecordModel{ID: 1, LeadID: 1, ActivityType: "customer_profile_updated", ActorUserID: "inviter-a", FromValueJSON: `{"customer_name":"张明明","customer_mobile":"13888889999","source_channel":"好学校","password":"should-never-copy"}`, ToValueJSON: `{"customer_name":"张小明","source_channel":"员工丁抖音（员工乙）"}`, MetadataJSON: `{"reason":"张明明此前曾来访","unknown_sensitive":"隐藏秘密"}`, CreatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&repo.ChannelIntegrationModel{ID: 1, Platform: "douyin", AccountName: "官方号", AuthMode: "token", Enabled: true, TokenCiphertext: "SECRET_TOKEN_MUST_NOT_COPY", CreatedBy: "admin", UpdatedBy: "admin", CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	s, err := BuildSnapshot(context.Background(), db, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	physical, err := os.ReadFile(s.path)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"张小明", "张明明", "13912345678", "13888889999", "parent_abc_123", "刘大勇", "SECRET_HASH_MUST_NOT_COPY", "SECRET_TOKEN_MUST_NOT_COPY", "should-never-copy", "隐藏秘密"} {
		if strings.Contains(string(physical), secret) {
			t.Errorf("physical snapshot leaked %q", secret)
		}
	}
	if !strings.Contains(string(physical), "员工丁抖音（员工乙）") || !strings.Contains(string(physical), "员工乙") {
		t.Fatal("business dimensions must be retained")
	}
	result, err := s.Query(context.Background(), "SELECT customer_ref,deal_amount_cents,refund_amount_cents,net_deal_amount_cents,created_at_date_shanghai,owner_first_followup_seconds FROM leads", nil, 100)
	if err != nil {
		t.Fatal(err)
	}
	want := []any{"客户#1", int64(10001), int64(1), int64(10000), "2026-10-08", int64(3600)}
	if fmt.Sprint(result.Rows[0]) != fmt.Sprint(want) {
		t.Fatalf("got %v want %v", result.Rows[0], want)
	}
	search, err := s.Search(context.Background(), SearchRequest{LeadID: 1, From: "2026-10-08", To: "2026-10-08", Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if search.TotalRows < 3 || len(search.Sources) < 3 {
		t.Fatalf("history not indexed: %+v", search)
	}
	encoded, _ := json.Marshal(search)
	if strings.Contains(string(encoded), "张小明") || strings.Contains(string(encoded), "parent_abc_123") {
		t.Fatal("query result leaked personal data")
	}
	if s.excluded < 2 {
		t.Fatal("excluded fragments must be reported")
	}
	path := s.path
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("snapshot file was not cleaned")
	}
}

func TestSnapshotEncryptedContactRegistryAndInputEvasions(t *testing.T) {
	db := snapshotFixtureDB(t)
	createSnapshotLead(t, db, 1)
	protector, err := security.NewAESGCMFieldProtector("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	mobile, _ := protector.Protect("13912345678")
	wechat, _ := protector.Protect("parent_abc_123")
	if err := db.Table("leads").Where("id=1").Updates(map[string]any{"customer_mobile": mobile, "customer_wechat": wechat}).Error; err != nil {
		t.Fatal(err)
	}
	s, err := BuildSnapshot(WithSnapshotFieldProtector(context.Background(), protector), db, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, text := range []string{"张小明的资料", `\u5f20\u5c0f\u660e`, "139%201234%205678", "１３９１２３４５６７８", "139\u200b1234\u200b5678", "parent_abc_123 的情况", "座机 0710-1234567", "微信：stranger_wechat", "姓名：刘大勇", "看看陈大明的跟进", "Bearer abcdefghijklmnopqrstuvwxyz"} {
		clean, _ := s.Sanitize(text)
		for _, secret := range []string{"张小明", "13912345678", "parent_abc_123", "0710-1234567", "stranger_wechat", "刘大勇", "陈大明", "abcdefghijklmnopqrstuvwxyz"} {
			if strings.Contains(clean, secret) {
				t.Errorf("input %q leaked %q -> %q", text, secret, clean)
			}
		}
	}
	physical, _ := os.ReadFile(s.path)
	if strings.Contains(string(physical), mobile) || strings.Contains(string(physical), wechat) {
		t.Fatal("ciphertext must not be copied")
	}
	if s.unmatchedEncrypted != 0 {
		t.Fatal("registry should decrypt locally")
	}
}

func TestSnapshotRejectsUnsafeSQLAndHasTimeout(t *testing.T) {
	db := snapshotFixtureDB(t)
	createSnapshotLead(t, db, 1)
	s, err := BuildSnapshot(context.Background(), db, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	unsafe := []string{
		"DELETE FROM leads", "WITH x AS (SELECT 1) DELETE FROM leads", "SELECT 1; DELETE FROM leads", "SELECT 1;;", "PRAGMA table_info(leads)", "ATTACH DATABASE ':memory:' AS evil", "SELECT * FROM sqlite_master", "SELECT * FROM sqlite_schema", "SELECT * FROM pragma_table_info('leads')", "SELECT load_extension('bad')", "SELECT readfile('/etc/passwd')", "SELECT writefile('/tmp/bad','bad')", "SELECT randomblob(1000000000)", "SELECT printf('%1000000000s','x')", "SELECT * FROM crm_wecom_user_bindings WHERE password_hash='x'", "SELECT * FROM system_settings", "SELECT * FROM cms_entries", "SELECT * FROM leads; /* trailing */ SELECT 1", "SELECT 1\x00;DELETE FROM leads",
	}
	for _, query := range unsafe {
		if _, err := s.Query(context.Background(), query, nil, 100); err == nil {
			t.Errorf("unsafe query accepted: %s", query)
		}
	}
	for _, query := range []string{"SELECT COUNT(*) AS n FROM leads", "WITH x AS (SELECT id FROM leads) SELECT * FROM x", "SELECT ';DELETE FROM leads' AS text", "SELECT id FROM leads; -- tail comment", "SELECT row_number() OVER (ORDER BY id) AS n FROM leads", "SELECT /* ; DELETE FROM leads */ id FROM leads"} {
		if _, err := s.Query(context.Background(), query, nil, 100); err != nil {
			t.Errorf("valid query %s failed: %v", query, err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := s.Query(ctx, "WITH RECURSIVE x(v) AS (SELECT 1 UNION ALL SELECT v+1 FROM x WHERE v<1000000000) SELECT sum(v) FROM x", nil, 100); err == nil {
		t.Fatal("expensive query did not time out")
	}
	result, err := s.Query(context.Background(), "SELECT count(*) FROM leads", nil, 100)
	if err != nil || result.Rows[0][0] != int64(1) {
		t.Fatalf("snapshot must survive rejected queries and timeout: %v %v", result, err)
	}
	var count int64
	db.Table("leads").Count(&count)
	if count != 1 {
		t.Fatal("source business data changed")
	}
}

func TestSnapshotLargeFullAggregationStableReadsAndConcurrentWrites(t *testing.T) {
	if testing.Short() {
		t.Skip("large data regression")
	}
	db := snapshotFixtureDB(t)
	if err := db.Exec(`WITH RECURSIVE n(v) AS (SELECT 1 UNION ALL SELECT v+1 FROM n WHERE v<10001) INSERT INTO leads(id,lead_no,customer_name,status,business_stage,ownership_state,result_status,deal_amount,refund_amount,payment_status,created_at,updated_at) SELECT v,'lead-'||v,'客户姓名'||v,'converted','finished','private','converted',10.01,0.01,'paid','2026-10-08 00:00:00+00:00','2026-10-08 00:00:00+00:00' FROM n`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`WITH RECURSIVE n(v) AS (SELECT 1 UNION ALL SELECT v+1 FROM n WHERE v<50001) INSERT INTO lead_followups(id,lead_id,sales_user_id,followup_result,remark,is_valid,created_at) SELECT v,((v-1)%10001)+1,'employee-1','connected','希望参加校园参观',1,'2026-10-08 00:30:00+00:00' FROM n`).Error; err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	writes := make(chan error, 200)
	stop := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			select {
			case <-stop:
				return
			default:
			}
			err := db.Exec("UPDATE leads SET profile_version=profile_version+1 WHERE id=?", (i%10001)+1).Error
			writes <- err
			time.Sleep(time.Millisecond)
		}
	}()
	start := time.Now()
	s, err := BuildSnapshot(context.Background(), db, t.TempDir())
	close(stop)
	wg.Wait()
	close(writes)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for err := range writes {
		if err != nil {
			t.Fatalf("business write blocked by snapshot: %v", err)
		}
	}
	t.Logf("full snapshot duration: %s", time.Since(start))
	metrics := s.PreparationMetrics()
	t.Logf("preparation metrics: %v", metrics)
	if metrics["source_rows"] != 60002 || metrics["source_read_ms"] >= 10000 {
		t.Fatalf("full source must fit its short read deadline without dropping rows: %v", metrics)
	}
	measured, measureErr := s.Measure(context.Background(), MeasureRequest{Source: "lead_followups", GroupBy: "actor_user_id"})
	if measureErr != nil || integerValue(measured.Rows[0][2]) != 50001 || integerValue(measured.Rows[0][3]) != 10001 {
		t.Fatalf("large full-set aggregation differs from independent fixture: %+v %v", measured, measureErr)
	}
	for query, want := range map[string]int64{"SELECT count(*) FROM leads": 10001, "SELECT count(*) FROM lead_followups": 50001, "SELECT sum(net_deal_amount_cents) FROM leads WHERE EXISTS (SELECT 1 FROM lead_followups f WHERE f.lead_id=leads.id)": 10001000} {
		result, err := s.Query(context.Background(), query, nil, 10)
		if err != nil {
			t.Fatal(err)
		}
		if result.Rows[0][0] != want {
			t.Fatalf("%s: got %v want %v", query, result.Rows, want)
		}
	}
	detail, err := s.Query(context.Background(), "SELECT id FROM leads ORDER BY id", nil, 20)
	if err != nil {
		t.Fatal(err)
	}
	if detail.TotalRows != 10001 || detail.ReturnedRows != 20 || !detail.Truncated {
		t.Fatalf("wrong complete count/pagination: %+v", detail)
	}
	search, err := s.Search(context.Background(), SearchRequest{Keyword: "校园参观", Limit: 25, Offset: 10})
	if err != nil {
		t.Fatal(err)
	}
	if search.TotalRows != 50001 || search.ReturnedRows != 25 || !search.Truncated {
		t.Fatalf("text search silently capped: %+v", search)
	}
	if err := db.Exec("UPDATE leads SET deal_amount=9999 WHERE id=1").Error; err != nil {
		t.Fatal(err)
	}
	result, err := s.Query(context.Background(), "SELECT deal_amount_cents FROM leads WHERE id=1", nil, 10)
	if err != nil {
		t.Fatal(err)
	}
	if result.Rows[0][0] != int64(1001) {
		t.Fatal("snapshot changed after business modification")
	}
	newSnapshot, err := BuildSnapshot(context.Background(), db, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer newSnapshot.Close()
	updated, err := newSnapshot.Query(context.Background(), "SELECT deal_amount_cents FROM leads WHERE id=1", nil, 10)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Rows[0][0] != int64(999900) {
		t.Fatal("next round failed to obtain fresh data")
	}
}

func TestSnapshotStartupCleanupAndMissingHistoricalTimes(t *testing.T) {
	db := snapshotFixtureDB(t)
	createSnapshotLead(t, db, 1)
	if err := db.Exec("UPDATE leads SET current_owner_started_at=NULL,current_owner_first_followup_at=NULL,first_followup_at='2020-01-01 00:00:00' WHERE id=1").Error; err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	s, err := BuildSnapshot(context.Background(), db, dir)
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.Query(context.Background(), "SELECT owner_first_followup_seconds,lifecycle_first_followup_seconds FROM leads", nil, 10)
	if err != nil {
		t.Fatal(err)
	}
	if result.Rows[0][0] != nil || result.Rows[0][1] != nil {
		t.Fatal("missing or negative response duration must not become zero")
	}
	path := s.path
	s.db.Close()
	if err := CleanupSnapshots(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("startup orphan cleanup failed")
	}
	s.Close()
}

func TestSnapshotDeletedHistoryAndWebsiteFormProjection(t *testing.T) {
	db := snapshotFixtureDB(t)
	createSnapshotLead(t, db, 1)
	createSnapshotLead(t, db, 2)
	now := time.Date(2026, 10, 8, 9, 0, 0, 0, time.UTC)
	if err := db.Exec("UPDATE leads SET deleted_at=? WHERE id=2", now).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&repo.LeadFollowupModel{LeadID: 2, SalesUserID: "inviter-a", FollowupResult: "connected", Remark: "历史客户要求改期", IsValid: true, CreatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&repo.LeadDeletionEventModel{LeadID: 2, LeadNo: "CRM-2", Action: "delete", ActorUserID: "admin", Reason: "重复录入", OccurredAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&repo.XiangwuFormSubmission{LeadID: 1, Source: "xiangwu_website_form", ExternalLeadID: "NOT_MODEL_EXTERNAL_ID", PayloadHash: "NOT_MODEL_PAYLOAD_HASH", Details: "当前阶段：高三；家长留言：张小明希望下周到校，13912345678；咨询方向：美术", SourcePath: "/school/", CreatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	s, err := BuildSnapshot(context.Background(), db, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for query, want := range map[string]int64{"SELECT count(*) FROM leads": 1, "SELECT count(*) FROM leads_history": 2, "SELECT count(*) FROM lead_followups": 0, "SELECT count(*) FROM lead_followups_history": 1, "SELECT count(*) FROM lead_deletion_events_history": 1, "SELECT sum(current_followup_eligible) FROM leads_history WHERE deleted_at IS NOT NULL": 0, "SELECT count(*) FROM xiangwu_form_submissions": 1} {
		result, err := s.Query(context.Background(), query, nil, 10)
		if err != nil {
			t.Fatal(err)
		}
		if result.Rows[0][0] != want {
			t.Errorf("%s got %v want %v", query, result.Rows, want)
		}
	}
	search, err := s.Search(context.Background(), SearchRequest{Keyword: "美术", Limit: 100})
	if err != nil || search.TotalRows != 1 {
		t.Fatalf("website detail not included: %v %v", search, err)
	}
	physical, _ := os.ReadFile(s.path)
	for _, secret := range []string{"张小明", "13912345678", "NOT_MODEL_EXTERNAL_ID", "NOT_MODEL_PAYLOAD_HASH"} {
		if strings.Contains(string(physical), secret) {
			t.Errorf("website data leaked %s", secret)
		}
	}
	if !strings.Contains(string(physical), "高三") {
		t.Fatal("website business detail missing")
	}
}

func TestSnapshotPreservesNamedBusinessDimensionsInQueries(t *testing.T) {
	db := snapshotFixtureDB(t)
	createSnapshotLead(t, db, 1)
	s, err := BuildSnapshot(context.Background(), db, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	result, err := s.Query(context.Background(), "SELECT count(*) FROM leads WHERE source_channel=?", []any{"员工丁抖音（员工乙）"}, 10)
	if err != nil || result.Rows[0][0] != int64(1) {
		t.Fatalf("source label was altered: %v %v", result, err)
	}
	result, err = s.Query(context.Background(), "SELECT count(*) FROM leads WHERE source_channel='员工丁抖音（员工乙）'", nil, 10)
	if err != nil || result.Rows[0][0] != int64(1) {
		t.Fatalf("SQL business label was altered: %v %v", result, err)
	}
	question, _ := s.Sanitize("分析员工丁抖音（员工乙）的成交表现")
	if !strings.Contains(question, "员工丁抖音（员工乙）") {
		t.Fatalf("business label lost in user question: %s", question)
	}
}

func TestSnapshotMaterializeResultSeparatesPreviewAndExport(t *testing.T) {
	db := snapshotFixtureDB(t)
	createSnapshotLead(t, db, 1)
	if err := db.Exec(`WITH RECURSIVE n(v) AS (SELECT 1 UNION ALL SELECT v+1 FROM n WHERE v<2501) INSERT INTO lead_followups(id,lead_id,sales_user_id,followup_result,remark,is_valid,created_at) SELECT v,1,'employee-1','connected','希望参加校园参观',1,'2026-10-08 00:30:00+00:00' FROM n`).Error; err != nil {
		t.Fatal(err)
	}
	s, err := BuildSnapshot(context.Background(), db, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	preview, err := s.Query(context.Background(), "SELECT id,lead_id,remark FROM lead_followups ORDER BY id", nil, 20)
	if err != nil {
		t.Fatal(err)
	}
	full, err := s.MaterializeResult(context.Background(), preview, 100000)
	if err != nil {
		t.Fatal(err)
	}
	if full.ID != preview.ID || !full.SnapshotAt.Equal(preview.SnapshotAt) || full.TotalRows != 2501 || full.ReturnedRows != 2501 || full.Truncated || preview.ReturnedRows != 20 || !preview.Truncated {
		t.Fatalf("wrong preview/export separation: preview %+v full rows %d/%d truncated=%v", preview, full.ReturnedRows, full.TotalRows, full.Truncated)
	}
	partial, err := s.MaterializeResult(context.Background(), preview, 1200)
	if err != nil {
		t.Fatal(err)
	}
	if partial.TotalRows != 2501 || partial.ReturnedRows != 1200 || !partial.Truncated {
		t.Fatal("explicit export row cap was not reported")
	}
	search, err := s.Search(context.Background(), SearchRequest{Keyword: "校园参观", Limit: 10, Offset: 100})
	if err != nil {
		t.Fatal(err)
	}
	fullSearch, err := s.MaterializeResult(context.Background(), search, 100000)
	if err != nil {
		t.Fatal(err)
	}
	if fullSearch.TotalRows != 2501 || fullSearch.ReturnedRows != 2501 || fullSearch.Truncated {
		t.Fatalf("search preview offset changed materialized export: %d/%d", fullSearch.ReturnedRows, fullSearch.TotalRows)
	}
	forged := *preview
	forged.ID = "not-executed"
	if _, err := s.MaterializeResult(context.Background(), &forged, 100000); err == nil {
		t.Fatal("unexecuted result was materialized")
	}
	forged = *preview
	forged.SnapshotAt = forged.SnapshotAt.Add(time.Second)
	if _, err := s.MaterializeResult(context.Background(), &forged, 100000); err == nil {
		t.Fatal("foreign snapshot result was materialized")
	}
}

func TestSnapshotAmbiguousCustomerAndPrivateStaffIdentifiers(t *testing.T) {
	db := snapshotFixtureDB(t)
	createSnapshotLead(t, db, 1)
	createSnapshotLead(t, db, 2)
	now := time.Now().UTC()
	if err := db.Create(&repo.WeComUserBindingModel{ID: 99, CRMUserID: 99, WeComUserID: "13833334444", EmployeeName: "员工乙", Role: "inviter", Status: "active", CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&repo.LeadFollowupModel{ID: 1, LeadID: 1, SalesUserID: "13833334444", FollowupResult: "connected", Remark: "李强愿意来校，电话一三九一二三四五六七八", IsValid: true, CreatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	s, err := BuildSnapshot(context.Background(), db, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	clean, _ := s.Sanitize("张小明的跟进，电话13912345678")
	if !strings.Contains(clean, "客户#1") || !strings.Contains(clean, "客户#2") {
		t.Fatalf("same-name or shared-contact matches silently picked one lead: %s", clean)
	}
	result, err := s.Query(context.Background(), "SELECT s.employee_name,f.sales_user_id FROM lead_followups f JOIN crm_wecom_user_bindings s ON s.wecom_userid=f.sales_user_id", nil, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Rows) != 1 || result.Rows[0][0] != "员工乙" || result.Rows[0][1] != "员工#99" {
		t.Fatalf("private staff IDs lost relationship: %v", result.Rows)
	}
	physical, _ := os.ReadFile(s.path)
	for _, secret := range []string{"13833334444", "李强", "一三九一二三四五六七八"} {
		if strings.Contains(string(physical), secret) {
			t.Errorf("private prose or identifier leaked: %s", secret)
		}
	}
}
