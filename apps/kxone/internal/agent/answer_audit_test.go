package agent

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"unicode/utf8"

	"github.com/google/uuid"
)

func TestFinalAnswerAuditExcludesNativeReasoningAndUnknownProperties(t *testing.T) {
	records := make(chan AnswerValidationAuditRecord, 2)
	f := newAgentSecurityFixture(t, func(w http.ResponseWriter, r *http.Request) {
		repairFixtureReply(w, finalResultTool("finalize_answer", map[string]any{"result_ids": []string{}, "markdown": "暂无已核对的数据。", "references": []any{}, "reasoning_content": "UNKNOWN_PROPERTY_REASONING_SENTINEL"}), "NATIVE_PRIVATE_REASONING_SENTINEL")
	})
	f.service.cfg.AnswerValidationAudit = func(record AnswerValidationAuditRecord) { records <- record }
	if err := f.db.Model(&Settings{}).Where("id=1").Update("model", "deepseek-v4-pro").Error; err != nil {
		t.Fatal(err)
	}
	c := securityCreateConversation(t, f, securityAdmin)
	run, err := f.service.Start(t.Context(), securityAdmin, c.ID, "核对数据", uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	finished := securityWaitRun(t, f, run.ID)
	if finished.Status != "completed" || len(records) != 1 {
		t.Fatalf("safe proposal audit missing: %+v records=%d", finished, len(records))
	}
	record := <-records
	raw, _ := json.Marshal(record)
	if record.Status != "accepted" || !record.ProposalAvailable || strings.Contains(string(raw), "REASONING") || strings.Contains(string(raw), "reasoning_content") || strings.Contains(string(raw), f.service.cfg.APIKey) {
		t.Fatalf("audit copied private protocol: %s", raw)
	}
}

func TestFinalAnswerAuditNeverReceivesPrivacyRejectedTurn(t *testing.T) {
	for _, field := range []string{"arguments", "content"} {
		t.Run(field, func(t *testing.T) {
			var audited atomic.Int32
			f := newAgentSecurityFixture(t, func(w http.ResponseWriter, r *http.Request) {
				markdown, content := "暂无已核对数据。", ""
				if field == "arguments" {
					markdown = "TEST_SECRET_INITIAL_MODEL_KEY"
				} else {
					content = "电话13800138001"
				}
				repairFixtureReplyWithContent(w, finalResultTool("finalize_answer", FinalizeRequest{ResultIDs: []string{}, Markdown: markdown}), "UNEXPECTED_DISABLED_REASONING", content)
			})
			f.service.cfg.MaxCalls = 2
			if err := f.db.Model(&Settings{}).Where("id=1").Update("model", "deepseek-v4-pro").Error; err != nil {
				t.Fatal(err)
			}
			f.service.cfg.AnswerValidationAudit = func(AnswerValidationAuditRecord) { audited.Add(1) }
			c := securityCreateConversation(t, f, securityAdmin)
			run, err := f.service.Start(t.Context(), securityAdmin, c.ID, "核对数据", uuid.NewString())
			if err != nil {
				t.Fatal(err)
			}
			finished := securityWaitRun(t, f, run.ID)
			if finished.Status == "completed" || audited.Load() != 0 {
				t.Fatalf("privacy rejected turn reached audit: %+v audit=%d", finished, audited.Load())
			}
		})
	}
}

func TestFinalAnswerAuditBoundsUTF8MasksCredentialsAndCannotAffectCompletion(t *testing.T) {
	snapshot := measuredFixture(t)
	cfg := DefaultConfig()
	cfg.APIKey = "SYNTHETIC_AUDIT_KEY"
	var record AnswerValidationAuditRecord
	cfg.AnswerValidationAudit = func(value AnswerValidationAuditRecord) { record = value }
	proposal := FinalizeRequest{Markdown: cfg.APIKey + strings.Repeat("待核对说明。", 3000)}
	emitAnswerValidationAudit(cfg, snapshot, "run", 3, &proposal, errors.New(cfg.APIKey+strings.Repeat("校验错误", 2000)))
	if !record.ArgumentsTruncated || !record.ErrorTruncated || record.ArgumentBytes <= len(record.Arguments) || record.ErrorBytes <= len(record.ValidationError) || len(record.Arguments) > 16<<10 || len(record.ValidationError) > 4<<10 || !utf8.ValidString(record.Arguments) || !utf8.ValidString(record.ValidationError) || strings.Contains(record.Arguments+record.ValidationError, cfg.APIKey) {
		t.Fatalf("unsafe bounded diagnostic metadata: %+v", record)
	}
	cfg.AnswerValidationAudit = func(AnswerValidationAuditRecord) { panic("synthetic diagnostic failure") }
	emitAnswerValidationAudit(cfg, snapshot, "run", 3, &proposal, nil)
}

func TestFinalAnswerAuditIsInheritedByFallback(t *testing.T) {
	backup := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		finalResultFixtureReply(w, "", finalResultTool("finalize_answer", FinalizeRequest{ResultIDs: []string{}, Markdown: "暂无已核对数据。"}))
	}))
	defer backup.Close()
	f := newAgentSecurityFixture(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) })
	configureTestBackup(t, f, backup.URL)
	records := make(chan AnswerValidationAuditRecord, 2)
	f.service.cfg.AnswerValidationAudit = func(record AnswerValidationAuditRecord) { records <- record }
	c := securityCreateConversation(t, f, securityAdmin)
	run, err := f.service.Start(t.Context(), securityAdmin, c.ID, "核对来源数量", uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	finished := securityWaitRun(t, f, run.ID)
	if finished.Status != "completed" || len(records) != 1 {
		t.Fatalf("fallback lost diagnostic hook: %+v records=%d", finished, len(records))
	}
	if record := <-records; record.RunID != run.ID || record.Status != "accepted" {
		t.Fatalf("wrong fallback audit: %+v", record)
	}
}
