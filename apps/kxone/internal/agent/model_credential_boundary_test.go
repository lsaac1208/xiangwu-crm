package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
)

func TestKnownModelCredentialCannotEnterProviderJSON(t *testing.T) {
	for _, key := range []string{"UNLABELLED_VENDOR_CREDENTIAL_2026", `UNLABELLED_"<>&_CREDENTIAL_2026`} {
		var calls atomic.Int64
		provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"OK"}}]}`)
		}))
		cfg := DefaultConfig()
		cfg.APIKey, cfg.BaseURL = key, provider.URL
		request := ModelRequest{Model: "fixture", Messages: []ModelMessage{{Role: "user", Content: "未标明性质的原文：" + key}}}
		_, err := callModel(context.Background(), cfg, true, request, nil)
		if err == nil || calls.Load() != 0 {
			t.Error("known credential was sent in the provider JSON body")
		}
		if err != nil && strings.Contains(err.Error(), key) {
			t.Error("rejection leaked credential")
		}
		provider.Close()
	}
}

func TestServiceRedactsKnownCredentialBeforeUsageReservation(t *testing.T) {
	var leaked atomic.Bool
	f := newAgentSecurityFixture(t, func(w http.ResponseWriter, r *http.Request) {
		var request ModelRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			return
		}
		for _, m := range request.Messages {
			if strings.Contains(m.Content, "TEST_SECRET_INITIAL_MODEL_KEY") {
				leaked.Store(true)
			}
		}
		fixtureModelHandler(w, rWithoutBody(request, r))
	})
	c := securityCreateConversation(t, f, securityAdmin)
	question := "请查询全部来源数量。补充原文：" + f.service.cfg.APIKey
	run, err := f.service.Start(t.Context(), securityAdmin, c.ID, question, uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	finished := securityWaitRun(t, f, run.ID)
	if finished.Status != "completed" {
		t.Fatalf("sanitized analysis did not finish: %s", finished.Error)
	}
	if leaked.Load() {
		t.Error("known credential bypassed generic text patterns")
	}
}

func TestProviderCredentialEchoIsMaskedAcrossEveryStreamBoundary(t *testing.T) {
	key := `CREDENTIAL_<>&_"_VALUE_2026`
	raw, _ := json.Marshal(key)
	for _, representation := range []string{key, html.EscapeString(key), string(raw[1 : len(raw)-1])} {
		for boundary := 0; boundary <= len(representation); boundary++ {
			f := newCredentialStreamFilter(key)
			output := f.Write("普通文字。"+representation[:boundary]) + f.Write(representation[boundary:]+"继续回答。") + f.Finish()
			if output != "普通文字。[凭证片段已隐藏]继续回答。" {
				t.Fatalf("stream boundary %d changed visible text or exposed credential", boundary)
			}
		}
	}
	f := newCredentialStreamFilter(key)
	if text := f.Write("正常中文分析，金额 123.45 元。") + f.Finish(); text != "正常中文分析，金额 123.45 元。" {
		t.Fatal("ordinary answer was rewritten")
	}
}

func TestCredentialEchoCannotReachActualModelDeltaOrResponse(t *testing.T) {
	const key = "SYNTHETIC_LONG_OPAQUE_CREDENTIAL"
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, part := range []string{"答复：" + key[:12], key[12:] + "，业务结论。"} {
			raw, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"content": part}}}})
			fmt.Fprintf(w, "data: %s\n\n", raw)
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer provider.Close()
	cfg := DefaultConfig()
	cfg.APIKey = key
	cfg.BaseURL = provider.URL
	var stream strings.Builder
	response, err := callModel(t.Context(), cfg, true, ModelRequest{Model: "fixture", Stream: true, Messages: []ModelMessage{{Role: "user", Content: "查询业务"}}}, func(s string) { stream.WriteString(s) })
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stream.String(), key) || strings.Contains(response.Message.Content, key) || stream.String() != response.Message.Content {
		t.Fatal("credential echo escaped streaming output boundary")
	}
	if response.Message.Content != "答复：[凭证片段已隐藏]，业务结论。" {
		t.Fatal("ordinary text surrounding credential changed")
	}
}
