package wecom

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/lsaac128/kxone/pkg/types"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestClientConstructorsNormalizeAPIBaseURL(t *testing.T) {
	provider := staticConfigProvider{config: Config{CorpID: "corp", Secret: "secret", AgentID: 1}}

	tests := []struct {
		name string
		new  func() Client
		want string
	}{
		{
			name: "legacy static constructor",
			new:  func() Client { return NewClient("corp", "secret", 1, "", "", nil) },
			want: DefaultAPIBaseURL,
		},
		{
			name: "legacy dynamic constructor",
			new:  func() Client { return NewDynamicClient(provider, nil) },
			want: DefaultAPIBaseURL,
		},
		{
			name: "empty custom base falls back",
			new:  func() Client { return NewDynamicClientWithBaseURL(provider, nil, "  ") },
			want: DefaultAPIBaseURL,
		},
		{
			name: "custom base removes trailing slash",
			new: func() Client {
				return NewClientWithBaseURL("corp", "secret", 1, "", "", nil, " http://127.0.0.1:18090/wecom/ ")
			},
			want: "http://127.0.0.1:18090/wecom",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.new().(*client).apiBaseURL
			if got != tt.want {
				t.Fatalf("apiBaseURL = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestClientUsesConfiguredAPIBaseURLForAllEndpoints(t *testing.T) {
	var (
		mu       sync.Mutex
		requests = make(map[string]int)
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests[r.URL.Path]++
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/wecom/cgi-bin/gettoken":
			if r.URL.Query().Get("corpid") != "corp-id" || r.URL.Query().Get("corpsecret") != "secret-value" {
				t.Errorf("unexpected gettoken query: %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"errcode":0,"access_token":"test-token","expires_in":7200}`))
		case "/wecom/cgi-bin/message/send":
			_, _ = w.Write([]byte(`{"errcode":0,"msgid":"message-id","response_code":"response-code"}`))
		case "/wecom/cgi-bin/message/update_template_card":
			_, _ = w.Write([]byte(`{"errcode":0}`))
		case "/wecom/cgi-bin/auth/getuserinfo":
			if r.URL.Query().Get("code") != "oauth code/+" {
				t.Errorf("code query = %q", r.URL.Query().Get("code"))
			}
			_, _ = w.Write([]byte(`{"errcode":0,"userid":"user-id"}`))
		case "/wecom/cgi-bin/get_jsapi_ticket":
			_, _ = w.Write([]byte(`{"errcode":0,"ticket":"ticket-value","expires_in":7200}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	provider := staticConfigProvider{config: Config{
		CorpID:  "corp-id",
		Secret:  "secret-value",
		AgentID: 1000001,
	}}
	api := NewDynamicClientWithBaseURL(provider, nil, server.URL+"/wecom/")
	ctx := context.Background()

	if _, err := api.GetAccessToken(ctx); err != nil {
		t.Fatalf("GetAccessToken() error = %v", err)
	}
	cardContent := &types.WeComCardContent{MainTitle: "测试卡片", MainDesc: "E2E"}
	if _, err := api.SendCard(ctx, &SendCardRequest{ToUser: "user-id", Content: cardContent}); err != nil {
		t.Fatalf("SendCard() error = %v", err)
	}
	if _, err := api.UpdateCard(ctx, &UpdateCardRequest{ResponseCode: "response-code", Content: cardContent}); err != nil {
		t.Fatalf("UpdateCard() error = %v", err)
	}
	if err := api.SendText(ctx, &SendTextRequest{ToUser: "user-id", Content: "测试消息"}); err != nil {
		t.Fatalf("SendText() error = %v", err)
	}
	if got, err := api.GetUserIDByCode(ctx, "oauth code/+"); err != nil || got != "user-id" {
		t.Fatalf("GetUserIDByCode() = %q, %v", got, err)
	}
	if _, err := api.GetJSAPIConfig(ctx, "https://crm.example.com/leads"); err != nil {
		t.Fatalf("GetJSAPIConfig() error = %v", err)
	}

	wantRequests := map[string]int{
		"/wecom/cgi-bin/gettoken":                     1,
		"/wecom/cgi-bin/message/send":                 2,
		"/wecom/cgi-bin/message/update_template_card": 1,
		"/wecom/cgi-bin/auth/getuserinfo":             1,
		"/wecom/cgi-bin/get_jsapi_ticket":             1,
	}
	mu.Lock()
	defer mu.Unlock()
	for path, want := range wantRequests {
		if got := requests[path]; got != want {
			t.Errorf("requests[%q] = %d, want %d", path, got, want)
		}
	}
}

func TestClientNetworkErrorsDoNotExposeCredentialURL(t *testing.T) {
	api := NewClientWithBaseURL(
		"corp-id",
		"secret-must-not-appear",
		1000001,
		"",
		"",
		nil,
		"https://wecom-proxy.example.invalid",
	).(*client)
	api.httpClient.Transport = roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("forced failure for %s", request.URL.String())
	})

	_, err := api.GetAccessToken(context.Background())
	if err == nil {
		t.Fatal("expected network error")
	}
	for _, sensitive := range []string{"secret-must-not-appear", "wecom-proxy.example.invalid", "corp-id"} {
		if strings.Contains(err.Error(), sensitive) {
			t.Fatalf("network error leaked %q: %v", sensitive, err)
		}
	}
}

func TestJSAPISignature(t *testing.T) {
	signature := jsapiSignature(
		"ticket-test",
		"nonce-test",
		1234567890,
		"https://crm.example.com/leads",
	)

	const expected = "aeb23780c18a10a1ff597568c31d64db0ef86fca"
	if signature != expected {
		t.Fatalf("signature = %s, want %s", signature, expected)
	}
}

func TestBuildTemplateCardUpdateOmitsEmptyTaskID(t *testing.T) {
	card := buildTemplateCardUpdate(&types.WeComCardContent{
		MainTitle: "线索已领取",
		MainDesc:  "销售已领取",
		Buttons: []types.CardButton{
			{Text: "查看线索", Style: 2, Type: 1, URL: "https://kxone.example.com/leads/10"},
		},
		ActionURL: "https://kxone.example.com/leads/10",
	})

	if _, ok := card["task_id"]; ok {
		t.Fatalf("update card should not include empty task_id: %#v", card)
	}
	if card["card_type"] != "button_interaction" {
		t.Fatalf("card_type = %v", card["card_type"])
	}
}
