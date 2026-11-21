package agent

import (
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// Public service runs must keep private results/lifetimes while avoiding a
// second copy of preparation work for concurrently arriving analyses.
func TestConcurrentAnalysesPrepareOnceAndKeepIndependentLifetimes(t *testing.T) {
	modelEntered, releaseModel := make(chan struct{}, 1), make(chan struct{})
	t.Cleanup(func() {
		select {
		case <-releaseModel:
		default:
			close(releaseModel)
		}
	})
	f := newAgentSecurityFixture(t, func(w http.ResponseWriter, r *http.Request) {
		modelEntered <- struct{}{}
		select {
		case <-releaseModel:
		case <-r.Context().Done():
			return
		}
		finalResultFixtureReply(w, "", metricToolCall("select_metrics", map[string]any{"selections": []MetricSelection{{MetricID: "followup_by_actor"}}}))
	})
	f.service.cfg.FixedMetrics = true
	seedOwnerInventory(t, f)
	if err := f.db.Exec(`WITH RECURSIVE n(v) AS (SELECT 1 UNION ALL SELECT v+1 FROM n WHERE v<6000)
INSERT INTO lead_followups(id,lead_id,sales_user_id,followup_result,remark,is_valid,created_at)
SELECT v,800,'staff-zhan','connected','测试跟进',1,'2026-10-08 00:30:00+00:00' FROM n`).Error; err != nil {
		t.Fatal(err)
	}
	admin := securityCreateConversation(t, f, securityAdmin)
	manager := securityCreateConversation(t, f, securityManager)
	first, err := f.service.Start(t.Context(), securityAdmin, admin.ID, "占伟手上有多少线索？", uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	second, err := f.service.Start(t.Context(), securityManager, manager.ID, "各员工实际记录了多少条跟进？", uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-modelEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("concurrent run did not reach provider")
	}
	completedFirst := securityWaitRun(t, f, first.ID)
	close(releaseModel)
	completedSecond := securityWaitRun(t, f, second.ID)
	if completedFirst.Status != "completed" || completedSecond.Status != "completed" {
		t.Fatalf("one run closed the other's snapshot: %s, %s", completedFirst.Status, completedSecond.Status)
	}
	if completedFirst.SnapshotAt == nil || completedSecond.SnapshotAt == nil || !completedFirst.SnapshotAt.Equal(*completedSecond.SnapshotAt) {
		t.Fatalf("overlapping runs duplicated preparation: %v != %v", completedFirst.SnapshotAt, completedSecond.SnapshotAt)
	}
	if response := securityRequest(t, f, http.MethodGet, "/api/agent/runs/"+first.ID, nil, securityManager); response.Code != 403 {
		t.Fatalf("another account read a private result: %d", response.Code)
	}
	files, err := filepath.Glob(filepath.Join(f.service.cfg.Directory, "snapshot-*.sqlite"))
	if err != nil || len(files) != 0 {
		t.Fatalf("prepared snapshots leaked after both runs: %v %v", files, err)
	}
	// A later question must observe a fresh committed state, never a cache.
	if err := f.db.Exec(`UPDATE leads SET current_owner_user_id='staff-li' WHERE id=800`).Error; err != nil {
		t.Fatal(err)
	}
	answer, _ := askScopeQuestion(t, f, "占伟手上有多少线索？")
	if !strings.Contains(answer.Content, "3 条") {
		t.Fatal("completed preparation was reused for a later question:", answer.Content)
	}
}

func TestCancellingOneConcurrentAnalysisKeepsTheOtherWorking(t *testing.T) {
	f := newAgentSecurityFixture(t, nil)
	f.service.cfg.FixedMetrics = true
	seedOwnerInventory(t, f)
	if err := f.db.Exec(`WITH RECURSIVE n(v) AS (SELECT 1 UNION ALL SELECT v+1 FROM n WHERE v<6000)
INSERT INTO lead_followups(id,lead_id,sales_user_id,followup_result,remark,created_at)
SELECT v,800,'staff-zhan','connected','测试跟进','2026-10-08 00:30:00+00:00' FROM n`).Error; err != nil {
		t.Fatal(err)
	}
	admin := securityCreateConversation(t, f, securityAdmin)
	manager := securityCreateConversation(t, f, securityManager)
	first, err := f.service.Start(t.Context(), securityAdmin, admin.ID, "占伟手上有多少线索？", uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	second, err := f.service.Start(t.Context(), securityManager, manager.ID, "占伟手上有多少线索？", uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	if err := f.service.Cancel(t.Context(), securityAdmin, first.ID); err != nil {
		t.Fatal(err)
	}
	if done := securityWaitRun(t, f, second.ID); done.Status != "completed" {
		t.Fatal("one account's cancel stopped another's analysis:", done)
	}
	if done := securityWaitRun(t, f, first.ID); done.Status != "cancelled" {
		t.Fatal(done)
	}
}
