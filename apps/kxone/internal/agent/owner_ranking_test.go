package agent

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

// Replay the exact three-turn production conversation at the HTTP boundary.
// The provider selects the same valid owner table for both ranking questions;
// the answer must still distinguish inventory from converted-lead ranking.
func TestOwnerRankingFollowUpAnswersTheAskedMeasure(t *testing.T) {
	var calls atomic.Int32
	f := newAgentSecurityFixture(t, func(w http.ResponseWriter, r *http.Request) {
		metric := "owner_current"
		if calls.Add(1) == 1 {
			metric = "lead_count"
		}
		finalResultFixtureReply(w, "", metricToolCall("select_metrics", map[string]any{"selections": []any{map[string]any{"metric_id": metric}}}))
	})
	f.service.cfg.FixedMetrics = true
	assertOwnerRankingConversation(t, f)
}

func TestOwnerRankingWhenProviderUnavailable(t *testing.T) {
	f := newAgentSecurityFixture(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) })
	f.service.cfg.FixedMetrics = true
	assertOwnerRankingConversation(t, f)
}

func TestOwnerRankingCannotBeReplacedWithGlobalCounts(t *testing.T) {
	f := newAgentSecurityFixture(t, func(w http.ResponseWriter, r *http.Request) {
		finalResultFixtureReply(w, "", metricToolCall("select_metrics", map[string]any{"selections": []any{map[string]any{"metric_id": "lead_count"}}}))
	})
	f.service.cfg.FixedMetrics = true
	assertOwnerRankingConversation(t, f)
}

func TestOwnerRankingDistinguishesUnknownMoneyFromZero(t *testing.T) {
	for _, unknown := range []bool{true, false} {
		t.Run(map[bool]string{true: "unknown", false: "zero"}[unknown], func(t *testing.T) {
			f := newAgentSecurityFixture(t, func(w http.ResponseWriter, r *http.Request) {
				finalResultFixtureReply(w, "", metricToolCall("select_metrics", map[string]any{"selections": []any{map[string]any{"metric_id": "owner_current"}}}))
			})
			f.service.cfg.FixedMetrics = true
			seedOwnerInventory(t, f)
			if unknown {
				if err := f.db.Exec(`UPDATE leads SET deal_amount='missing' WHERE id=801`).Error; err != nil {
					t.Fatal(err)
				}
			}
			c := securityCreateConversation(t, f, securityAdmin)
			start := securityRequest(t, f, http.MethodPost, "/api/agent/conversations/"+c.ID+"/runs", map[string]any{"message": "谁净成交额最多？", "request_id": uuid.NewString()}, securityAdmin)
			var run Run
			if start.Code != http.StatusAccepted || json.Unmarshal(start.Body.Bytes(), &run) != nil {
				t.Fatal(start.Body.String())
			}
			securityWaitRun(t, f, run.ID)
			get := securityRequest(t, f, http.MethodGet, "/api/agent/runs/"+run.ID, nil, securityAdmin)
			var detail struct {
				Messages []Message `json:"messages"`
			}
			if get.Code != http.StatusOK || json.Unmarshal(get.Body.Bytes(), &detail) != nil {
				t.Fatal(get.Body.String())
			}
			answer := ""
			for _, m := range detail.Messages {
				if m.Role == "assistant" {
					answer = m.Content
				}
			}
			if unknown && (!strings.Contains(answer, "金额资料不完整") || strings.Contains(answer, "最多")) {
				t.Errorf("missing amount cannot establish winner: %s", answer)
			}
			if !unknown && (!strings.Contains(answer, "并列") || !strings.Contains(answer, "0 元")) {
				t.Errorf("known zero is a valid amount: %s", answer)
			}
		})
	}
}

func TestOwnerRankingKeepsSameNamedAccountsSeparate(t *testing.T) {
	f := newAgentSecurityFixture(t, func(w http.ResponseWriter, r *http.Request) {
		finalResultFixtureReply(w, "", metricToolCall("select_metrics", map[string]any{"selections": []any{map[string]any{"metric_id": "owner_current"}}}))
	})
	f.service.cfg.FixedMetrics = true
	seedOwnerInventory(t, f)
	if err := f.db.Exec(`UPDATE crm_wecom_user_bindings SET employee_name='占伟' WHERE wecom_userid='staff-li'`).Error; err != nil {
		t.Fatal(err)
	}
	a, _ := askScopeQuestion(t, f, "手上线索最多的是谁？")
	if !strings.Contains(a.Content, "staff-zhan") || !strings.Contains(a.Content, "4 条") {
		t.Fatalf("distinct accounts must not merge: %s", a.Content)
	}
}

func TestOwnerNetRankingDoesNotSilentlyUseCreationMonth(t *testing.T) {
	f := newAgentSecurityFixture(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) })
	f.service.cfg.FixedMetrics = true
	seedOwnerInventory(t, f)
	a, events := askScopeQuestion(t, f, "本月净额谁最高？")
	if !strings.Contains(a.Content, "成交发生日期") {
		t.Fatalf("net month must clarify occurrence versus cohort: %s", a.Content)
	}
	for _, event := range events {
		if event.Type == "metric_selected" {
			t.Fatal("unsupported date basis produced a replacement number")
		}
	}
}

func assertOwnerRankingConversation(t *testing.T, f *agentSecurityFixture) {
	t.Helper()
	seedOwnerInventory(t, f)
	if err := f.db.Exec(`UPDATE leads SET status='converted',result_status='converted',business_stage='finished' WHERE id=805`).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.Exec(`INSERT INTO leads(id,lead_no,customer_name,source_channel,status,business_stage,ownership_state,result_status,current_owner_user_id,current_owner_role,assigned_inviter_user_id,deal_amount,refund_amount,payment_status,created_at,updated_at) VALUES (806,'ranking-extra','匿名客户','好学校','converted','finished','private','converted','staff-li','inviter','staff-li',0,0,'unpaid',?,?)`, time.Now(), time.Now()).Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		if err := f.db.Exec(`INSERT INTO leads(id,lead_no,customer_name,status,ownership_state,result_status,current_owner_user_id,created_at,updated_at) VALUES (?,?,?,'public_pool','public_pool','active','',?,?)`, 900+i, uuid.NewString(), "匿名公海客户", time.Now(), time.Now()).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := f.db.Exec(`UPDATE leads SET deal_amount=CASE id WHEN 801 THEN 9000 ELSE 1000 END,payment_status='paid' WHERE id IN (801,805,806)`).Error; err != nil {
		t.Fatal(err)
	}
	c := securityCreateConversation(t, f, securityAdmin)
	for _, tc := range []struct {
		question       string
		want           []string
		column, winner string
	}{
		{"现在系统中一共有多少条线索 ？", []string{"17 条"}, "", ""},
		{"手上线索最多的是谁？", []string{"占伟", "4 条", "最多"}, "线索数", "占伟"},
		{"谁成交的最多？", []string{"李明", "2", "成交", "最多"}, "成交数", "李明"},
		{"谁净成交额最多？", []string{"占伟", "9000", "净成交额", "最多"}, "净成交额(元)", "占伟"},
	} {
		response := securityRequest(t, f, http.MethodPost, "/api/agent/conversations/"+c.ID+"/runs", map[string]any{"message": tc.question, "request_id": uuid.NewString()}, securityAdmin)
		var run Run
		if response.Code != http.StatusAccepted || json.Unmarshal(response.Body.Bytes(), &run) != nil {
			t.Fatalf("start %s: %d %s", tc.question, response.Code, response.Body.String())
		}
		securityWaitRun(t, f, run.ID)
		response = securityRequest(t, f, http.MethodGet, "/api/agent/runs/"+run.ID, nil, securityAdmin)
		var detail struct {
			Run      Run       `json:"run"`
			Messages []Message `json:"messages"`
			Results  []Result  `json:"results"`
		}
		if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &detail) != nil || detail.Run.Status != "completed" {
			t.Fatalf("get run: %d %s", response.Code, response.Body.String())
		}
		answer := ""
		for _, message := range detail.Messages {
			if message.Role == "assistant" {
				answer = strings.Split(message.Content, "\n")[0]
			}
		}
		for _, want := range tc.want {
			if !strings.Contains(answer, want) {
				t.Errorf("%s must answer the ranking measure directly; missing %q in %q", tc.question, want, answer)
			}
		}
		if strings.Contains(tc.question, "最多") && len(detail.Results) > 0 {
			for _, row := range detail.Results[0].Rows {
				if row[0] == "公海" || row[0] == "已分配待领取" {
					t.Errorf("人员排名不应把非员工分组当作人: %+v", row)
				}
			}
		}
		if tc.column != "" {
			if len(detail.Results) != 1 || len(detail.Results[0].Rows) < 2 || detail.Results[0].Rows[0][0] != tc.winner {
				t.Errorf("%s排名表应把%s排在首行: %+v", tc.column, tc.winner, detail.Results)
			}
			var view struct {
				Messages []struct {
					Blocks []Block `json:"blocks"`
				} `json:"messages"`
			}
			_ = json.Unmarshal(response.Body.Bytes(), &view)
			found := false
			for _, message := range view.Messages {
				for _, block := range message.Blocks {
					if block.Type == "chart" {
						found = true
						if len(block.YColumns) != 1 || block.YColumns[0] != tc.column {
							t.Errorf("%s排名图使用错误字段: %+v", tc.column, block.YColumns)
						}
					}
				}
			}
			if !found {
				t.Error("missing owner ranking chart")
			}
		}
	}
}
