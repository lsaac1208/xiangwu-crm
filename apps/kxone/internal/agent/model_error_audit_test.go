package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProviderErrorAuditCapturesOnlyFailedStreamPrefixes(t *testing.T) {
	const key = "SYNTHETIC_STREAM_ERROR_KEY"
	const headerSecret = "SYNTHETIC_STREAM_HEADER_SECRET"
	answer := `data: {"choices":[{"delta":{"content":"可见答案"}}]}` + "\n\n"
	usage := `data: {"usage":{"prompt_tokens":12,"completion_tokens":3},"choices":[]}` + "\n\n"
	tests := []struct {
		name          string
		body          string
		publicError   string
		knownUsage    bool
		visibleAnswer bool
		brokenLength  bool
	}{
		{"json_error_without_done", `{"error":"` + key + strings.Repeat("x", 10000) + `","usage":{"prompt_tokens":12,"completion_tokens":3}}`, "模型响应中断，请重试", false, false, false},
		{"malformed_sse", "data: {\"error\":\"" + key + "\n\n", "模型流式响应格式无效", false, false, false},
		{"sse_provider_error", `data: {"error":"` + key + `","usage":{"prompt_tokens":12,"completion_tokens":3}}` + "\n\n", "模型服务在生成过程中返回错误", false, false, false},
		{"stream_read_interrupted", ": " + key + "\n\n", "模型响应中断，请重试", false, false, true},
		{"known_usage_before_missing_done", answer + usage, "模型响应中断，请重试", true, true, false},
		{"complete_stream", answer + usage + "data: [DONE]\n\n", "", true, true, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				w.Header().Set("X-Private-Header", headerSecret)
				if test.brokenLength {
					w.Header().Set("Content-Length", fmt.Sprint(len(test.body)+50))
				}
				fmt.Fprint(w, test.body)
			}))
			defer provider.Close()
			for _, mode := range []string{"disabled", "capture", "panicking"} {
				t.Run(mode, func(t *testing.T) {
					cfg := DefaultConfig()
					cfg.BaseURL, cfg.APIKey = provider.URL, key
					calls := 0
					if mode != "disabled" {
						cfg.ProviderErrorAudit = func(status int, body []byte) {
							calls++
							want := test.body
							if len(want) > providerDiagnosticLimit {
								want = want[:providerDiagnosticLimit]
								if len(body) != providerDiagnosticLimit {
									t.Errorf("diagnostic did not retain the bounded prefix: %d", len(body))
								}
							}
							if status != http.StatusOK || len(body) > providerDiagnosticLimit || len(body) == 0 || !strings.HasPrefix(want, string(body)) {
								t.Errorf("unexpected private diagnostic status=%d bytes=%d", status, len(body))
							}
							if bytes.Contains(body, []byte(headerSecret)) {
								t.Error("response headers reached stream audit")
							}
							if mode == "panicking" {
								panic("private stream diagnostic " + key)
							}
						}
					}
					var delta strings.Builder
					out, err := callModel(context.Background(), cfg, true, ModelRequest{Model: cfg.Model, Stream: true, MaxTokens: 10}, func(text string) { delta.WriteString(text) })
					if test.publicError == "" {
						if err != nil || calls != 0 {
							t.Fatal("complete stream was diagnosed as a failure")
						}
					} else if err == nil || err.Error() != test.publicError || (mode != "disabled" && calls != 1) {
						t.Fatal("stream diagnostic changed public failure or missed audit")
					}
					if out.HasUsage != test.knownUsage || (test.knownUsage && (out.Usage.InputTokens != 12 || out.Usage.OutputTokens != 3)) {
						t.Fatal("stream diagnostic changed whether usage is known")
					}
					wantAnswer := ""
					if test.visibleAnswer {
						wantAnswer = "可见答案"
					}
					encoded, marshalErr := json.Marshal(out)
					if marshalErr != nil || bytes.Contains(encoded, []byte(key)) || bytes.Contains(encoded, []byte(headerSecret)) || delta.String() != wantAnswer || out.Message.Content != wantAnswer {
						t.Fatal("private stream body leaked or changed the visible answer")
					}
				})
			}
		})
	}
}

func TestProviderErrorAuditIsBoundedPrivateAndDoesNotChangePublicFailure(t *testing.T) {
	const key = "SYNTHETIC_PROVIDER_ERROR_KEY"
	const headerSecret = "SYNTHETIC_RESPONSE_HEADER_SECRET"
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Private-Header", headerSecret)
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"error":"`+key+" "+strings.Repeat("x", 10000)+`"}`)
	}))
	defer provider.Close()
	for _, mode := range []string{"disabled", "capture", "panicking"} {
		t.Run(mode, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.BaseURL = provider.URL
			cfg.APIKey = key
			calls := 0
			if mode != "disabled" {
				cfg.ProviderErrorAudit = func(status int, body []byte) {
					calls++
					if status != 400 || len(body) != 8192 {
						t.Errorf("unbounded or incorrect diagnostic status=%d bytes=%d", status, len(body))
					}
					if strings.Contains(string(body), headerSecret) {
						t.Error("response headers reached body-only audit hook")
					}
					if mode == "panicking" {
						panic("private callback failure " + key)
					}
				}
			}
			out, err := callModel(context.Background(), cfg, true, ModelRequest{Model: cfg.Model, Stream: true, MaxTokens: 10}, nil)
			if err == nil || err.Error() != "模型服务返回 HTTP 400，请检查配置、余额或稍后重试" {
				t.Fatal("diagnostics changed the public error")
			}
			encoded, marshalErr := json.Marshal(out)
			if marshalErr != nil || strings.Contains(string(encoded), key) || strings.Contains(err.Error(), key) || out.HasUsage {
				t.Fatal("private error body became public output or trusted usage")
			}
			if mode != "disabled" && calls != 1 {
				t.Fatalf("audit called %d times", calls)
			}
		})
	}
}

func TestConfigJSONOmitsCloudKeyAndDiagnosticCallbacks(t *testing.T) {
	cfg := DefaultConfig()
	cfg.APIKey = "SYNTHETIC_CONFIG_KEY"
	cfg.RequestAudit = func([]byte) error { return nil }
	cfg.ProviderErrorAudit = func(int, []byte) {}
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal("callbacks should be excluded from configuration JSON")
	}
	for _, forbidden := range []string{cfg.APIKey, "APIKey", "RequestAudit", "ProviderErrorAudit"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatal("server-only credential or diagnostic callback leaked into JSON")
		}
	}
}
