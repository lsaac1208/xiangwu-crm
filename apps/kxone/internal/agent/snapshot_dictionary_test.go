package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/lsaac128/kxone/internal/adapter/repo"
	"github.com/lsaac128/kxone/pkg/types"
)

func TestAgentDeliversStructuredBusinessDictionaryToToolLoop(t *testing.T) {
	var dictionarySeen, policySeen atomic.Bool
	var enumSeen, relationshipSeen, metricSeen bool
	f := newAgentSecurityFixture(t, func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		var request ModelRequest
		if err := json.Unmarshal(body, &request); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		for _, message := range request.Messages {
			if message.Role == "system" {
				for _, required := range []string{"crm_wecom_user_bindings.wecom_userid", "status='converted'", "绑定结果的全部行一致", "计入或排除某月实收"} {
					if !strings.Contains(message.Content, required) {
						t.Errorf("model request lost a business boundary: %s", required)
					}
				}
				policySeen.Store(true)
			}
			if message.Role == "tool" {
				var page struct {
					Items []struct {
						Kind, Key string
						Value     json.RawMessage
					}
					NextOffset int  `json:"next_offset"`
					HasMore    bool `json:"has_more"`
				}
				if json.Unmarshal([]byte(message.Content), &page) == nil {
					for _, item := range page.Items {
						switch item.Kind {
						case "enum":
							enumSeen = true
						case "relationship":
							relationshipSeen = true
						case "metric":
							if item.Key == "current_converted_leads" && strings.Contains(string(item.Value), "converted") {
								metricSeen = true
							}
						}
					}
				}
			}
		}
		lastTool := ""
		for _, message := range request.Messages {
			for _, call := range message.ToolCalls {
				lastTool = call.Function.Name
			}
		}
		if lastTool == "" {
			finalResultFixtureReply(w, "", finalResultTool("describe_data", map[string]any{"tables": []string{"leads", "crm_wecom_user_bindings"}}))
			return
		}
		if lastTool == "describe_data" {
			var page struct {
				NextOffset int  `json:"next_offset"`
				HasMore    bool `json:"has_more"`
			}
			_ = json.Unmarshal([]byte(request.Messages[len(request.Messages)-1].Content), &page)
			if page.HasMore {
				finalResultFixtureReply(w, "", finalResultTool("describe_data", map[string]any{"tables": []string{"leads", "crm_wecom_user_bindings"}, "offset": page.NextOffset}))
				return
			}
			dictionarySeen.Store(enumSeen && relationshipSeen && metricSeen)
			finalResultFixtureReply(w, "", finalResultTool("query_data", map[string]any{"sql": "SELECT source_channel,COUNT(*) AS n FROM leads GROUP BY source_channel"}))
			return
		}
		var result Result
		for _, message := range request.Messages {
			if message.Role == "tool" {
				var item Result
				if json.Unmarshal([]byte(message.Content), &item) == nil && item.ID != "" {
					result = item
				}
			}
		}
		finalResultFixtureReply(w, "", finalResultTool("select_final_results", map[string]any{"result_ids": []string{result.ID}}), finalResultTool("finalize_answer", FinalizeRequest{Markdown: "已核对本轮来源数据。", References: []AnswerReference{}}))
	})
	conversation := securityCreateConversation(t, f, securityAdmin)
	run, err := f.service.Start(context.Background(), securityAdmin, conversation.ID, "查询来源线索数量", uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	if finished := securityWaitRun(t, f, run.ID); finished.Status != "completed" || !dictionarySeen.Load() || !policySeen.Load() {
		t.Fatalf("dictionary contract did not survive the controlled tool loop: %+v", finished)
	}
}

func TestSnapshotDictionaryCurrentConversionSurvivesHistoricalCounterexamples(t *testing.T) {
	db := snapshotFixtureDB(t)
	registered := time.Date(2026, 9, 6, 8, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		id        int64
		status    string
		result    string
		converted *time.Time
	}{
		{1, "converted", "converted", nil},      // Current conversion with an unknown date.
		{2, "following", "active", &registered}, // A timestamp is not current state.
		{3, "converted", "", &registered},       // Legacy outcome projection is still empty in source.
		{4, "invalid", "invalid", &registered},  // Finished does not imply converted.
		{5, "converted", "active", &registered}, // Contradictory fields must remain visible.
		{6, "following", "converted", nil},
	} {
		stage := "finished"
		if test.status == "following" {
			stage = "invitation"
		}
		row := repo.LeadModel{ID: test.id, LeadNo: fmt.Sprintf("SEMANTIC-%d", test.id), CustomerName: "隔离客户", Status: test.status, ResultStatus: test.result, BusinessStage: stage, ConvertedAt: test.converted, DealAmount: float64(test.id * 10), PaymentStatus: "paid", CreatedAt: registered, UpdatedAt: registered.AddDate(0, 1, 0)}
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	snapshot, err := BuildSnapshot(context.Background(), db, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.Close()
	dictionary := snapshot.Describe().(SnapshotDescription)
	definition := dictionary.MetricDefinitions["current_converted_leads"]
	result, err := snapshot.Query(context.Background(), "SELECT id FROM "+definition.Table+" WHERE "+definition.FilterSQL+" ORDER BY id", nil, 100)
	if err != nil || fmt.Sprint(result.Rows) != "[[1] [3] [5]]" {
		t.Fatalf("current conversion definition excluded legacy/missing dates or accepted timestamp-only conversion: %+v err=%v", result, err)
	}
	count, err := snapshot.Query(context.Background(), "SELECT "+definition.ValueSQL+", SUM(CASE WHEN "+definition.DateColumn+" IS NULL THEN 1 ELSE 0 END) FROM "+definition.Table+" WHERE "+definition.FilterSQL, nil, 100)
	if err != nil || fmt.Sprint(count.Rows) != "[[3 1]]" {
		t.Fatalf("conversion date coverage was lost: %+v err=%v", count, err)
	}
	net := dictionary.MetricDefinitions["current_net_converted_amount"]
	amount, err := snapshot.Query(context.Background(), "SELECT "+net.ValueSQL+" FROM "+net.Table+" WHERE "+net.FilterSQL, nil, 100)
	if err != nil || fmt.Sprint(amount.Rows) != "[[9000]]" {
		t.Fatalf("net amount selected a different population: %+v err=%v", amount, err)
	}
	conflicts, err := snapshot.Query(context.Background(), "SELECT id FROM leads WHERE result_status <> '' AND ((status='converted') <> (result_status='converted')) ORDER BY id", nil, 100)
	if err != nil || fmt.Sprint(conflicts.Rows) != "[[5] [6]]" {
		t.Fatalf("source conflicts were silently normalized: %+v err=%v", conflicts, err)
	}
	if dictionary.Enums["leads.status"][string(types.LeadStatusConverted)] == "" || dictionary.Enums["business_stage"][string(types.BusinessStageFinished)] == "" || dictionary.Enums["business_stage"]["成交"] != "" {
		t.Fatal("dictionary confused stored enums with translated display labels")
	}
	// Paid flags and dates are present, but the dictionary must expose the
	// missing cash evidence instead of advertising a cash metric over them.
	if _, exists := dictionary.MetricDefinitions["cash_receipts_by_period"]; exists {
		t.Fatal("cash flow was made calculable without receipt transactions")
	}
	cash, exists := dictionary.UnavailableFacts["cash_receipts_by_period"]
	if !exists || len(cash.MissingEvidence) != 3 {
		t.Fatal("cash evidence gap omitted despite paid flags")
	}
	for _, proxy := range []string{"leads.payment_status", "leads.converted_at", "leads.updated_at", "lead_activity_records.created_at"} {
		found := false
		for _, invalid := range cash.InvalidProxies {
			found = found || invalid == proxy
		}
		if !found {
			t.Errorf("cash date proxy not identified: %s", proxy)
		}
	}
}

func TestSnapshotDictionaryEmployeeRelationshipsPreserveBusinessIdentifiers(t *testing.T) {
	db := snapshotFixtureDB(t)
	createSnapshotLead(t, db, 1)
	now := time.Now().UTC()
	for _, row := range []repo.WeComUserBindingModel{
		{ID: 11, CRMUserID: 101, Username: "login-a", WeComUserID: "inviter-a", EmployeeName: "员工甲", Role: "inviter", Status: "disabled", CreatedAt: now, UpdatedAt: now},
		{ID: 12, CRMUserID: 102, Username: "login-b", WeComUserID: "13900001111", EmployeeName: "员工乙", Role: "closer", Status: "active", CreatedAt: now, UpdatedAt: now},
	} {
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	for index, actor := range []string{"inviter-a", "13900001111", "system"} {
		if err := db.Create(&repo.LeadFollowupModel{ID: int64(index + 1), LeadID: 1, SalesUserID: actor, Remark: "隔离跟进", IsValid: true, CreatedAt: now}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Create(&repo.LeadDispatchRecordModel{ID: 1, LeadID: 1, ManagerUserID: "inviter-a", SalesUserID: "13900001111", ActionType: "reassign", CreatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	snapshot, err := BuildSnapshot(context.Background(), db, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.Close()
	dictionary := snapshot.Describe().(SnapshotDescription)
	assertColumn := func(table, column string) {
		t.Helper()
		for _, field := range dictionary.Tables[table] {
			if field == column {
				return
			}
		}
		t.Fatalf("dictionary advertises a nonexistent relationship column: %s.%s", table, column)
	}
	personKeys := map[string]SnapshotRelationship{}
	for _, relation := range dictionary.Relationships {
		assertColumn(relation.ToTable, relation.ToColumn)
		for _, column := range relation.FromColumns {
			assertColumn(relation.FromTable, column)
			if relation.ToTable == "crm_wecom_user_bindings" {
				if relation.ToColumn != "wecom_userid" {
					t.Fatal("business relationship incorrectly uses numeric account IDs")
				}
				personKeys[relation.FromTable+"."+column] = relation
			}
		}
	}
	// New business userid columns must acquire a reviewed relationship rather
	// than silently inheriting an assumed numeric account join.
	for table, fields := range dictionary.Tables {
		if table == "crm_wecom_user_bindings" || strings.HasSuffix(table, "_history") {
			continue
		}
		for _, field := range fields {
			if strings.HasSuffix(field, "_user_id") || field == "actor_userid" {
				if _, ok := personKeys[table+"."+field]; !ok {
					t.Errorf("missing business identifier relationship: %s.%s", table, field)
				}
			}
		}
	}
	followup := personKeys["lead_followups.sales_user_id"]
	result, err := snapshot.Query(context.Background(), "SELECT f.id,u.employee_name FROM "+followup.FromTable+" f LEFT JOIN "+followup.ToTable+" u ON f.sales_user_id=u."+followup.ToColumn+" ORDER BY f.id", nil, 100)
	if err != nil || fmt.Sprint(result.Rows) != "[[1 员工甲] [2 员工乙] [3 <nil>]]" {
		t.Fatalf("business joins lost disabled, redacted or system identifiers: %+v err=%v", result, err)
	}
	dispatch := personKeys["lead_dispatch_records.sales_user_id"]
	result, err = snapshot.Query(context.Background(), "SELECT manager.employee_name,sales.employee_name FROM "+dispatch.FromTable+" d LEFT JOIN "+dispatch.ToTable+" manager ON d.manager_user_id=manager."+dispatch.ToColumn+" LEFT JOIN "+dispatch.ToTable+" sales ON d.sales_user_id=sales."+dispatch.ToColumn, nil, 100)
	if err != nil || fmt.Sprint(result.Rows) != "[[员工甲 员工乙]]" {
		t.Fatalf("separate dispatch participants were joined as a composite key: %+v err=%v", result, err)
	}
}
