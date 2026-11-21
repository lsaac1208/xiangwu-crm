package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/lsaac128/kxone/internal/adapter/repo"
	"github.com/lsaac128/kxone/pkg/types"
)

func TestProviderMetadataIsObservedWithoutInventingVersion(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprint(stream), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if stream {
					fmt.Fprint(w, "data: {\"id\":\"chatcmpl-fixture\",\"model\":\"reported-model-2026-10-08\",\"choices\":[{\"delta\":{\"content\":\"OK\"}}]}\n\ndata: {\"choices\":[],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":2}}\n\ndata: [DONE]\n\n")
				} else {
					fmt.Fprint(w, `{"id":"chatcmpl-fixture","model":"reported-model-2026-10-08","choices":[{"message":{"role":"assistant","content":"OK"}}],"usage":{"prompt_tokens":10,"completion_tokens":2}}`)
				}
			}))
			defer server.Close()
			cfg := DefaultConfig()
			cfg.BaseURL, cfg.APIKey = server.URL, "test-secret"
			response, err := callModel(context.Background(), cfg, true, ModelRequest{Model: "configured-alias", Stream: stream}, nil)
			if err != nil || response.ReportedModel != "reported-model-2026-10-08" || response.ResponseID != "chatcmpl-fixture" || !response.HasUsage {
				t.Fatalf("metadata missing: %+v, %v", response, err)
			}
		})
	}
}

func TestProviderMetadataRejectsSnapshotPIIWithoutInventingIdentifiers(t *testing.T) {
	r := newRedactor()
	r.addName("KnownCustomer", "客户#1")
	r.addContact("13800138001", "客户#1")
	r.addContact("test_parent_0001", "客户#1")
	r.prepare()
	snapshot := &Snapshot{redactor: r}
	for _, value := range []string{"13800138001", "resp-13800138001", "test_parent_0001", "provider/test_parent_0001", "KnownCustomer", "provider-KnownCustomer", "0710-3456789", "13912345678", "sk-privatecredentialvalue", "prefix-real-key-suffix", "resp-550e8400-e29b-41d4-a716-13800138001a", "0710-3456789-550e8400-e29b-41d4-a716-446655440000"} {
		if actual := safeSnapshotProviderMetadata(value, "real-key", snapshot); actual != "" {
			t.Fatalf("private supplier metadata was retained: %q", actual)
		}
	}
	for _, value := range []string{"qwen3.8-max", "vendor/model:version-1", "reported-model-2026-10-08", "chatcmpl-aB3cD4eF5", "550e8400-e29b-41d4-a716-446655440000", "resp-9a2f716e-347b-466a-a920-ecfe5f1bd2ad"} {
		if actual := safeSnapshotProviderMetadata(value, "real-key", snapshot); actual != value {
			t.Fatalf("valid public supplier identifier changed: %q to %q", value, actual)
		}
	}
	if actual := safeSnapshotProviderMetadata("qwen3.8-max", "real-key", nil); actual != "" {
		t.Fatal("metadata accepted without local privacy context")
	}
}

func TestAgentProviderMetadataPIINeverEntersPersistedUsage(t *testing.T) {
	f := newAgentSecurityFixture(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "data: {\"id\":\"test_parent_0001\",\"model\":\"13800138001\",\"choices\":[{\"delta\":{\"content\":\"可展示答案\"}}]}\n\ndata: {\"choices\":[],\"usage\":{\"prompt_tokens\":70,\"completion_tokens\":20}}\n\ndata: [DONE]\n\n")
	})
	now := time.Now().UTC()
	lead := &types.Lead{ID: 901, LeadNo: "METADATA-PRIVACY-901", CustomerName: "登记客户", CustomerMobile: "13800138001", CustomerWechat: "test_parent_0001", SourceChannel: "好学校", Status: types.LeadStatusPendingDispatch, CreatedAt: now, UpdatedAt: now}
	if err := repo.NewGormLeadRepository(f.db).Create(context.Background(), lead); err != nil {
		t.Fatal(err)
	}
	conversation := securityCreateConversation(t, f, securityAdmin)
	run, err := f.service.Start(context.Background(), securityAdmin, conversation.ID, "核对测试数据", uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	if finished := securityWaitRun(t, f, run.ID); finished.Status != "completed" {
		t.Fatalf("privacy filtering prevented valid answer: %+v", finished)
	}
	var event Event
	if err := f.db.Where("run_id = ? AND type = ?", run.ID, "model_usage").First(&event).Error; err != nil {
		t.Fatal(err)
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(event.Data), &data); err != nil || data["reported_model"] != "" || data["response_id"] != "" || data["settle_result"] != "settled" {
		t.Fatalf("metadata must stay unknown while usage settles: %s err=%v", event.Data, err)
	}
	for _, private := range []string{"13800138001", "test_parent_0001", "登记客户"} {
		if strings.Contains(event.Data, private) {
			t.Fatal("PII entered persisted provider audit")
		}
	}
}

func TestOutboundAuditStopsTransmissionWithoutEchoingSensitiveText(t *testing.T) {
	var received atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { received.Add(1) }))
	defer server.Close()
	cfg := DefaultConfig()
	cfg.BaseURL, cfg.APIKey = server.URL, "test-secret"
	cfg.RequestAudit = func(body []byte) error { return errors.New("sensitive-test-secret") }
	_, err := callModel(context.Background(), cfg, true, ModelRequest{Model: "test", Messages: []ModelMessage{{Role: "user", Content: "synthetic"}}}, nil)
	if err == nil || err.Error() != "本地出站数据核验未通过，模型请求未发送" || received.Load() != 0 {
		t.Fatalf("outbound guard did not fail closed: %v, calls=%d", err, received.Load())
	}
}

func TestProviderMetadataRejectsSecretEchoAndUntrustedText(t *testing.T) {
	for _, value := range []string{"", "prefix-secret-value-suffix", "model\ncontrol", "<script>", "姓名手机号"} {
		if got := safeProviderIdentifier(value, "secret-value"); got != "" {
			t.Fatalf("unsafe metadata accepted: %q", got)
		}
	}
	if got := safeProviderIdentifier("vendor/model:version-1", "secret-value"); got != "vendor/model:version-1" {
		t.Fatalf("valid supplier identifier lost: %q", got)
	}
}
