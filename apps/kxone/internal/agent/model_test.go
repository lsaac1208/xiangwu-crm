package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

func TestModelEndpointBlocksPrivateReservedAndMalformedURLs(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for _, url := range []string{
		"http://8.8.8.8/v1/chat/completions", "https://127.0.0.1/chat/completions", "https://[::1]/chat/completions",
		"https://10.1.1.1/api", "https://172.16.1.1/api", "https://192.168.1.1/api", "https://169.254.169.254/api",
		"https://100.100.100.200/api", "https://198.18.0.1/api", "https://192.0.2.1/api", "https://[fc00::1]/api",
		"https://api-key-secret@8.8.8.8/api", "https://8.8.8.8/api?key=api-key-secret", "https://8.8.8.8/api#key",
		"file:///etc/passwd", "https://%invalid/api",
	} {
		t.Run(url, func(t *testing.T) {
			err := validateEndpoint(ctx, url, false)
			if err == nil {
				t.Fatalf("unsafe endpoint accepted: %s", url)
			}
			if strings.Contains(err.Error(), "api-key-secret") {
				t.Fatal("endpoint validation echoed secret")
			}
		})
	}
	if err := validateEndpoint(ctx, "https://8.8.8.8/v1/chat/completions", false); err != nil {
		t.Fatalf("public HTTPS address rejected: %v", err)
	}
	if err := validateEndpoint(ctx, "http://127.0.0.1/test", true); err != nil {
		t.Fatalf("explicit isolated fixture blocked: %v", err)
	}
	if err := validateEndpoint(ctx, "http://secret@127.0.0.1/test", true); err == nil {
		t.Fatal("userinfo allowed even in fixture mode")
	}
}

func TestModelTransportRechecksDNSBeforeDial(t *testing.T) {
	// Resolve a public address at configuration time, then change DNS to a
	// loopback address before DialContext. No external TCP connection is made.
	packet, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer packet.Close()
	var rebound atomic.Bool
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 4096)
		for {
			n, addr, err := packet.ReadFrom(buf)
			if err != nil {
				return
			}
			var p dnsmessage.Parser
			head, err := p.Start(buf[:n])
			if err != nil {
				continue
			}
			qs, err := p.AllQuestions()
			if err != nil {
				continue
			}
			b := dnsmessage.NewBuilder(nil, dnsmessage.Header{ID: head.ID, Response: true, Authoritative: true, RecursionDesired: head.RecursionDesired, RecursionAvailable: true})
			_ = b.StartQuestions()
			for _, q := range qs {
				_ = b.Question(q)
			}
			_ = b.StartAnswers()
			for _, q := range qs {
				if q.Type == dnsmessage.TypeA {
					ip := [4]byte{8, 8, 8, 8}
					if rebound.Load() {
						ip = [4]byte{127, 0, 0, 1}
					}
					_ = b.AResource(dnsmessage.ResourceHeader{Name: q.Name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET, TTL: 0}, dnsmessage.AResource{A: ip})
				}
			}
			reply, err := b.Finish()
			if err == nil {
				_, _ = packet.WriteTo(reply, addr)
			}
		}
	}()
	previous := net.DefaultResolver
	net.DefaultResolver = &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "udp", packet.LocalAddr().String())
	}}
	t.Cleanup(func() { net.DefaultResolver = previous; _ = packet.Close(); <-done })
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := validateEndpoint(ctx, "https://agent-rebind.fixture.invalid/chat/completions", false); err != nil {
		t.Fatalf("public first resolution failed: %v", err)
	}
	rebound.Store(true)
	client := modelClient(false)
	defer client.CloseIdleConnections()
	transport := client.Transport.(*http.Transport)
	conn, err := transport.DialContext(ctx, "tcp", "agent-rebind.fixture.invalid:443")
	if conn != nil {
		conn.Close()
		t.Fatal("DNS rebinding made a connection")
	}
	if err == nil {
		t.Fatal("rebound private address accepted")
	}
}

func TestModelErrorsAndInvalidStreamsNeverEchoProviderSecrets(t *testing.T) {
	const key = "SECRET_MODEL_TEST_KEY_NEVER_RETURN"
	cases := []struct {
		name   string
		status int
		body   string
	}{
		{"http-provider-error", 401, `{"error":{"message":"invalid key SECRET_MODEL_TEST_KEY_NEVER_RETURN"}}`},
		{"stream-provider-error", 200, "data: {\"error\":{\"message\":\"SECRET_MODEL_TEST_KEY_NEVER_RETURN\"}}\n\ndata: [DONE]\n\n"},
		{"invalid-json", 200, "data: malformed SECRET_MODEL_TEST_KEY_NEVER_RETURN\n\ndata: [DONE]\n\n"},
		{"interrupted", 200, "data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n"},
		{"invalid-tool-index", 200, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":32}]}}]}\n\ndata: [DONE]\n\n"},
		{"invalid-usage-large", 200, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":9223372036854775807,\"completion_tokens\":1}}\n\ndata: [DONE]\n\n"},
		{"invalid-usage-negative", 200, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":-1,\"completion_tokens\":1}}\n\ndata: [DONE]\n\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer "+key {
					t.Error("model authorization missing")
				}
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			}))
			defer server.Close()
			cfg := DefaultConfig()
			cfg.APIKey = key
			cfg.BaseURL = server.URL
			out, err := callModel(context.Background(), cfg, true, ModelRequest{Model: cfg.Model, Messages: []ModelMessage{{Role: "user", Content: "test"}}, Stream: true, MaxTokens: 10}, nil)
			if err == nil {
				t.Fatal("invalid provider response accepted")
			}
			if strings.Contains(err.Error(), key) {
				t.Fatalf("provider key leaked: %v", err)
			}
			encoded, _ := json.Marshal(out)
			if strings.Contains(string(encoded), key) {
				t.Fatal("provider secret returned in model result")
			}
			if strings.HasPrefix(tc.name, "invalid-usage") && out.HasUsage {
				t.Fatal("invalid usage marked reliable")
			}
		})
	}
}

func TestModelStreamingToolAssemblyAndKnownUsage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call-id\",\"function\":{\"name\":\"query_\",\"arguments\":\"{\\\"sql\\\":\"}}]}}]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"name\":\"data\",\"arguments\":\"\\\"SELECT count(*) FROM leads\\\"}\"}}]}}]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":50,\"completion_tokens\":10}}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()
	cfg := DefaultConfig()
	cfg.BaseURL = server.URL
	cfg.APIKey = "fixture-key"
	out, err := callModel(context.Background(), cfg, true, ModelRequest{Model: cfg.Model, Stream: true, MaxTokens: 100}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !out.HasUsage || out.Usage.InputTokens != 50 || out.Usage.OutputTokens != 10 {
		t.Fatalf("known usage lost: %+v", out)
	}
	if len(out.Message.ToolCalls) != 1 || out.Message.ToolCalls[0].ID != "call-id" || out.Message.ToolCalls[0].Function.Name != "query_data" || out.Message.ToolCalls[0].Function.Arguments != `{"sql":"SELECT count(*) FROM leads"}` {
		t.Fatalf("tool fragments incorrect: %+v", out.Message.ToolCalls)
	}
}

func TestModelPriceUsesPerMillionTokenMicroYuanAndRoundsUp(t *testing.T) {
	cfg := DefaultConfig()
	if got := modelCost(1_000_000, 1_000_000, cfg); got != 48_000_000 {
		t.Fatalf("million-token price=%d", got)
	}
	cfg.InputPriceMicro = 1
	cfg.OutputPriceMicro = 1
	if got := modelCost(1, 1, cfg); got != 2 {
		t.Fatalf("fractional micro-yuan ceiling=%d", got)
	}
}

func TestModelOutputTokenLimitPreservesPartialButReportsLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"部分查询已完成，但分析还没有\"}}]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"length\"}]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":50,\"completion_tokens\":100}}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()
	cfg := DefaultConfig()
	cfg.BaseURL = server.URL
	cfg.APIKey = "fixture-key"
	out, err := callModel(context.Background(), cfg, true, ModelRequest{Model: cfg.Model, Stream: true, MaxTokens: 100}, nil)
	if err == nil {
		t.Fatal("length-truncated answer accepted as complete")
	}
	if out.Message.Content == "" {
		t.Fatal("partial output lost after token limit")
	}
	if !out.HasUsage || out.Usage.OutputTokens != 100 {
		t.Fatal("known usage lost after token limit")
	}
}
