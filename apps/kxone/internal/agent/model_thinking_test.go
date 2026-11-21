package agent

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestExplicitDeepSeekThinkingContextStaysPrivateAndReturnsToTools(t *testing.T) {
	const thought = "PRIVATE_REASONING_FIXTURE_NOT_VISIBLE"
	calls := 0
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var request map[string]any
		_ = json.NewDecoder(r.Body).Decode(&request)
		thinking, _ := request["thinking"].(map[string]any)
		if thinking["type"] != "enabled" || request["reasoning_effort"] != "low" {
			t.Error("Pro analytical policy not applied")
		}
		if calls == 2 {
			msgs := request["messages"].([]any)
			m := msgs[len(msgs)-2].(map[string]any)
			if m["reasoning_content"] != thought {
				t.Error("required private tool reasoning context was lost")
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		if calls == 1 {
			fmt.Fprintf(w, `data: {"choices":[{"delta":{"reasoning_content":%q,"tool_calls":[{"index":0,"id":"tool-one","function":{"name":"describe_data","arguments":"{}"}}]}}]}`+"\n\n", thought)
		} else {
			fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"可见分析\"}}]}\n\n")
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer provider.Close()
	cfg := DefaultConfig()
	cfg.Model = "deepseek-v4-pro"
	cfg.BaseURL = provider.URL
	request := ModelRequest{Model: cfg.Model, Thinking: &ModelThinking{Type: "enabled"}, ReasoningEffort: "low", Tools: agentTools(), Stream: true, Messages: []ModelMessage{{Role: "user", Content: "查询"}}}
	var visible strings.Builder
	first, err := callModel(t.Context(), cfg, true, request, func(s string) { visible.WriteString(s) })
	if err != nil {
		t.Fatal(err)
	}
	request.Messages = append(request.Messages, first.Message, ModelMessage{Role: "tool", ToolCallID: "tool-one", Content: "{}"})
	second, err := callModel(t.Context(), cfg, true, request, func(s string) { visible.WriteString(s) })
	if err != nil {
		t.Fatal(err)
	}
	if visible.String() != "可见分析" || strings.Contains(second.Message.Content, thought) {
		t.Fatal("private reasoning entered visible answer")
	}
}

// Non-thinking tool calls are an official protocol mode, not a partially erased
// thinking segment. Production requests choose it before the first model call.
func TestDeepSeekDefaultsToNonThinkingWithoutChangingExistingHistory(t *testing.T) {
	for _, model := range []string{"deepseek-v4-pro", "deepseek-flash", "deepseek-chat"} {
		t.Run(model, func(t *testing.T) {
			reasoning := "EXISTING_NATIVE_REASONING_MUST_NOT_BE_DELETED"
			request := ModelRequest{Model: model, ReasoningEffort: "low", Messages: []ModelMessage{{Role: "assistant", ReasoningContent: &reasoning}}}
			applyProviderPolicy(&request)
			if request.Thinking == nil || request.Thinking.Type != "disabled" || request.ReasoningEffort != "" {
				t.Fatalf("non-thinking policy missing: thinking=%+v effort=%q", request.Thinking, request.ReasoningEffort)
			}
			if request.Messages[0].ReasoningContent == nil || *request.Messages[0].ReasoningContent != reasoning {
				t.Fatal("provider policy mutated an existing native segment")
			}
			explicit := ModelRequest{Model: model, Thinking: &ModelThinking{Type: "enabled"}, ReasoningEffort: "low"}
			applyProviderPolicy(&explicit)
			if explicit.Thinking.Type != "enabled" || explicit.ReasoningEffort != "low" {
				t.Fatal("explicit native thinking protocol was silently disabled")
			}
		})
	}
}

func TestDeepSeekNonThinkingToolRoundTripDoesNotCreateNativeReasoning(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprint("stream=", stream), func(t *testing.T) {
			const ignored = "api_key=SYNTHETIC_DISABLED_REASONING_NOT_PART_OF_PROTOCOL"
			calls := 0
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				var request ModelRequest
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
				}
				if request.Thinking == nil || request.Thinking.Type != "disabled" || request.ReasoningEffort != "" {
					t.Error("request did not explicitly select non-thinking mode")
				}
				for _, message := range request.Messages {
					if message.ReasoningContent != nil {
						t.Error("disabled mode replayed unexpected native reasoning")
					}
				}
				if calls == 2 {
					if len(request.Messages) != 3 || request.Messages[1].ToolCalls[0].ID != "tool-one" || request.Messages[2].ToolCallID != "tool-one" || request.Messages[2].Content != "{}" {
						t.Error("ordinary tool call/reply group was not preserved")
					}
				}
				message := map[string]any{"role": "assistant", "reasoning_content": ignored}
				if calls == 1 {
					tc := map[string]any{"id": "tool-one", "type": "function", "function": map[string]any{"name": "describe_data", "arguments": "{}"}}
					if stream {
						tc["index"] = 0
					}
					message["tool_calls"] = []any{tc}
				} else {
					message["content"] = "可见分析"
				}
				choiceKey := "message"
				if stream {
					choiceKey = "delta"
				}
				payload, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{choiceKey: message}}, "usage": map[string]any{"prompt_tokens": 100, "completion_tokens": 10}})
				if stream {
					w.Header().Set("Content-Type", "text/event-stream")
					fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", payload)
				} else {
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write(payload)
				}
			}))
			defer provider.Close()
			cfg := DefaultConfig()
			cfg.Model = "deepseek-v4-pro"
			cfg.BaseURL = provider.URL
			request := ModelRequest{Model: cfg.Model, Stream: stream, Tools: agentTools(), Messages: []ModelMessage{{Role: "user", Content: "核对"}}}
			first, err := callModel(t.Context(), cfg, true, request, nil)
			if err != nil {
				t.Fatal(err)
			}
			if first.Message.ReasoningContent != nil || len(first.Message.ToolCalls) != 1 || !first.HasUsage {
				t.Fatalf("invalid non-thinking tool response: %+v", first)
			}
			request.Messages = append(request.Messages, first.Message, ModelMessage{Role: "tool", ToolCallID: "tool-one", Content: "{}"})
			second, err := callModel(t.Context(), cfg, true, request, nil)
			if err != nil || second.Message.ReasoningContent != nil || second.Message.Content != "可见分析" || calls != 2 || !second.HasUsage {
				t.Fatalf("non-thinking continuation failed: %+v %v", second, err)
			}
		})
	}
}
