package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestInlineReasoningFilterBoundsIncompleteTags(t *testing.T) {
	var filter inlineReasoningFilter
	if got := filter.Write("<think " + strings.Repeat("x", 5000)); got != "" || !filter.blocked || len(filter.pending) != 0 {
		t.Fatal("oversized incomplete reasoning tag was not discarded")
	}
	if got := filter.Write(">PRIVATE</think>answer") + filter.Finish(); got != "" {
		t.Fatal("oversized reasoning tag reopened output")
	}
}

func TestModelInlineReasoningNeverReachesAnswerOrDelta(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprint(stream), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if stream {
					for _, piece := range []string{"<thi", "NK>PRIVATE_A<ana", "lysis>PRIVATE_B</analysis>", "PRIVATE_C</t", "HiNk>答案", "：7。<reason", "ing>PRIVATE_D</reasoning>"} {
						raw, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"content": piece, "reasoning_content": "PRIVATE_E"}}}})
						fmt.Fprintf(w, "data: %s\n\n", raw)
					}
					fmt.Fprint(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":40,\"completion_tokens\":15}}\n\ndata: [DONE]\n\n")
				} else {
					_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": "<THINK>PRIVATE_A<analysis>PRIVATE_B</analysis>PRIVATE_C</think>答案：7。<reasoning>PRIVATE_D</reasoning>", "reasoning_content": "PRIVATE_E"}}}, "usage": map[string]any{"prompt_tokens": 40, "completion_tokens": 15}})
				}
			}))
			defer server.Close()
			cfg := DefaultConfig()
			cfg.BaseURL = server.URL
			var deltas strings.Builder
			out, err := callModel(context.Background(), cfg, true, ModelRequest{Model: cfg.Model, Stream: stream}, func(value string) { deltas.WriteString(value) })
			if err != nil || out.Message.Content != "答案：7。" || (stream && deltas.String() != "答案：7。") {
				t.Fatalf("reasoning leaked or answer lost: content=%q deltas=%q err=%v", out.Message.Content, deltas.String(), err)
			}
			if !out.HasUsage || out.Usage.InputTokens != 40 || out.Usage.OutputTokens != 15 {
				t.Fatalf("reasoning filtering changed usage: %+v", out)
			}
		})
	}
}

func TestInlineReasoningFilterEveryChunkBoundary(t *testing.T) {
	cases := []struct{ input, expected string }{
		{"前<think>PRIVATE</think>后", "前后"},
		{"前<ANALYSIS>PRIVATE</aNaLySiS>后", "前后"},
		{"前<reasoning>PRIVATE</reasoning>后", "前后"},
		{"前<think>PRIVATE<analysis>PRIVATE<reasoning>PRIVATE</reasoning>PRIVATE</analysis>PRIVATE</think>后", "前后"},
		{"<ThInK role=\"internal\">PRIVATE</THINK >答案<think/>继续", "答案继续"},
		{"前<think>PRIVATE 尚未结束", "前"},
		{"前<reasoning attr=\"unfinished", "前"},
		{"前<thi", "前"},
		{"前<think>PRIVATE<analysis>PRIVATE</think>PRIVATE</analysis>后", "前"},
		{"1 < 2；<b>正文</b>；<thinking>正文</thinking>；<analysis-note>说明</analysis-note>；最后 <", "1 < 2；<b>正文</b>；<thinking>正文</thinking>；<analysis-note>说明</analysis-note>；最后 <"},
		{"1 < 2 <think>PRIVATE</think>3 < 4", "1 < 2 3 < 4"},
		{"前</THINK>后", "前后"},
	}
	for index, tc := range cases {
		t.Run(fmt.Sprint(index), func(t *testing.T) {
			// Three chunks exercise every pair of split positions, including
			// inside tag names, delimiters and UTF-8 answer text.
			for first := 0; first <= len(tc.input); first++ {
				for second := first; second <= len(tc.input); second++ {
					var filter inlineReasoningFilter
					actual := filter.Write(tc.input[:first]) + filter.Write(tc.input[first:second]) + filter.Write(tc.input[second:]) + filter.Finish()
					if actual != tc.expected {
						t.Fatalf("split=%d,%d actual=%q expected=%q", first, second, actual, tc.expected)
					}
				}
			}
			var filter inlineReasoningFilter
			var chars strings.Builder
			for i := range []byte(tc.input) {
				chars.WriteString(filter.Write(tc.input[i : i+1]))
			}
			chars.WriteString(filter.Finish())
			if chars.String() != tc.expected {
				t.Fatalf("single-byte chunks: %q", chars.String())
			}
		})
	}
	var excessive inlineReasoningFilter
	if result := excessive.Write(strings.Repeat("<think>", 65)+"PRIVATE"+strings.Repeat("</think>", 65)+"tail") + excessive.Finish(); result != "" {
		t.Fatal("excessive nesting reopened untrusted output")
	}
}

func TestModelReasoningOnlyRequiresAnswerButAllowsToolCalls(t *testing.T) {
	for _, stream := range []bool{false, true} {
		for _, tool := range []bool{false, true} {
			for _, content := range []string{"<think>PRIVATE</think>", "<analysis>PRIVATE"} {
				t.Run(fmt.Sprintf("stream=%v/tool=%v/closed=%v", stream, tool, strings.HasSuffix(content, ">")), func(t *testing.T) {
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						message := map[string]any{"role": "assistant", "content": content}
						if tool {
							message["tool_calls"] = []any{map[string]any{"index": 0, "id": "call-reason-filter", "type": "function", "function": map[string]any{"name": "query_data", "arguments": `{"sql":"SELECT COUNT(*) FROM leads"}`}}}
						}
						choice := map[string]any{"message": message}
						if stream {
							choice = map[string]any{"delta": message}
						}
						response := map[string]any{"choices": []any{choice}, "usage": map[string]any{"prompt_tokens": 70, "completion_tokens": 20}}
						if stream {
							raw, _ := json.Marshal(response)
							fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", raw)
						} else {
							_ = json.NewEncoder(w).Encode(response)
						}
					}))
					defer server.Close()
					cfg := DefaultConfig()
					cfg.BaseURL = server.URL
					var deltas strings.Builder
					out, err := callModel(context.Background(), cfg, true, ModelRequest{Model: cfg.Model, Stream: stream}, func(value string) { deltas.WriteString(value) })
					if out.Message.Content != "" || deltas.Len() != 0 || !out.HasUsage || out.Usage.OutputTokens != 20 {
						t.Fatalf("reasoning-only output or usage incorrect: %+v delta=%q", out, deltas.String())
					}
					if tool {
						if err != nil || len(out.Message.ToolCalls) != 1 || out.Message.ToolCalls[0].Function.Name != "query_data" || out.Message.ToolCalls[0].Function.Arguments != `{"sql":"SELECT COUNT(*) FROM leads"}` {
							t.Fatalf("reasoning filter damaged tool protocol: %+v %v", out, err)
						}
					} else if err == nil || !strings.Contains(err.Error(), "未返回可展示的回答") {
						t.Fatalf("reasoning-only response must return actionable error: %v", err)
					}
				})
			}
		}
	}
}

func TestModelReasoningFilterStillStreamsVisibleAnswerBeforeCompletion(t *testing.T) {
	visible := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"<think>PRIVATE</think>可见答案\"}}]}\n\n")
		w.(http.Flusher).Flush()
		select {
		case <-visible:
		case <-r.Context().Done():
			return
		}
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"继续输出\"}}]}\n\ndata: {\"choices\":[],\"usage\":{\"prompt_tokens\":70,\"completion_tokens\":20}}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()
	cfg := DefaultConfig()
	cfg.BaseURL = server.URL
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var deltas strings.Builder
	out, err := callModel(ctx, cfg, true, ModelRequest{Model: cfg.Model, Stream: true}, func(value string) {
		deltas.WriteString(value)
		if value == "可见答案" {
			visible <- struct{}{}
		}
	})
	if err != nil || out.Message.Content != "可见答案继续输出" || deltas.String() != out.Message.Content {
		t.Fatalf("answer was buffered until provider completion: content=%q delta=%q err=%v", out.Message.Content, deltas.String(), err)
	}
}

func TestAgentReasoningFilteredBeforePersistenceAndEvents(t *testing.T) {
	for _, answer := range []string{"最终答案", ""} {
		t.Run(fmt.Sprint(answer != ""), func(t *testing.T) {
			f := newAgentSecurityFixture(t, func(w http.ResponseWriter, r *http.Request) {
				for _, text := range []string{"<thi", "nk>PRIVATE_INTERNAL</thi", "nk>", answer} {
					body, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"content": text}}}})
					fmt.Fprintf(w, "data: %s\n\n", body)
				}
				fmt.Fprint(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":70,\"completion_tokens\":20}}\n\ndata: [DONE]\n\n")
			})
			conversation := securityCreateConversation(t, f, securityAdmin)
			run, err := f.service.Start(context.Background(), securityAdmin, conversation.ID, "核对测试数据", uuid.NewString())
			if err != nil {
				t.Fatal(err)
			}
			finished := securityWaitRun(t, f, run.ID)
			if (answer == "" && finished.Status != "failed") || (answer != "" && finished.Status != "completed") {
				t.Fatalf("unexpected run status: %+v", finished)
			}
			var message Message
			visibleContent := answer
			if answer == "" {
				visibleContent = "模型未返回可展示的回答，请重试或调整模型配置"
			}
			if err := f.db.Where("run_id = ? AND role = ?", run.ID, "assistant").First(&message).Error; err != nil || message.Content != visibleContent {
				t.Fatalf("unsafe persisted content: %q err=%v", message.Content, err)
			}
			var events []Event
			if err := f.db.Where("run_id = ?", run.ID).Find(&events).Error; err != nil {
				t.Fatal(err)
			}
			for _, event := range events {
				if strings.Contains(event.Data, "PRIVATE_INTERNAL") || strings.Contains(event.Data, "<think>") {
					t.Fatalf("unsafe persisted %s event", event.Type)
				}
			}
		})
	}
}
