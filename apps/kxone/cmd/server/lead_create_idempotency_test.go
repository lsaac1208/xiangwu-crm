package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/lsaac128/kxone/internal/adapter/repo"
	"github.com/lsaac128/kxone/internal/usecase"
	"gorm.io/gorm"
)

func TestNativeCreateReplaysAndRejectsChangedIntent(t *testing.T) {
	r := newTestRouter(t)
	token := issueDevToken(t, r, "idem-manager", "manager")
	path := "/api/leads"
	key := "4d72a024-2722-4d82-9f30-b1465f215897"
	body := []byte(`{"customer_name":"测试客户","customer_mobile":"13912345678","source_channel":"好学校"}`)
	headers := map[string]string{"Content-Type": "application/json", "Idempotency-Key": key}
	one := doRaw(t, r, http.MethodPost, path, token, body, headers)
	two := doRaw(t, r, http.MethodPost, path, token, body, headers)
	if one.Code != 201 || two.Code != 201 {
		t.Fatalf("create/replay status %d/%d: %s", one.Code, two.Code, two.Body.String())
	}
	var a, b struct {
		Lead struct {
			ID int64 `json:"id"`
		} `json:"lead"`
	}
	json.Unmarshal(one.Body.Bytes(), &a)
	json.Unmarshal(two.Body.Bytes(), &b)
	if a.Lead.ID == 0 || a.Lead.ID != b.Lead.ID {
		t.Fatalf("retry duplicated intent: %d/%d", a.Lead.ID, b.Lead.ID)
	}
	changed := []byte(`{"customer_name":"改动","customer_mobile":"13912345678","source_channel":"好学校"}`)
	if got := doRaw(t, r, http.MethodPost, path, token, changed, headers); got.Code != 409 {
		t.Fatalf("changed intent status %d", got.Code)
	}
}

func TestNativeCreateReplaysRetiredSourceWithoutAcceptingNewOldSource(t *testing.T) {
	var db *gorm.DB
	var create *usecase.CreateLeadUseCase
	router := newTestRouterWithOptions(t, testRouterOptions{agentFixtureDB: &db, createLead: &create})
	token := issueDevToken(t, router, "legacy-source-manager", "manager")
	user, err := repo.NewGormUserBindingRepository(db).GetByWeComUserID(t.Context(), "legacy-source-manager")
	if err != nil || user == nil {
		t.Fatalf("load test account: %v", err)
	}
	key := "194a788c-9886-456f-9b80-b51c672e8740"
	// The previous handler accepted this source before invoking the same use case.
	prior, err := create.Execute(t.Context(), &usecase.CreateLeadRequest{
		AccountID: user.ID, IdempotencyKey: key, CustomerName: "旧页面客户", CustomerMobile: "13912345678",
		SourceChannel: "主动来电（老王）", DispatchManagerUserID: "legacy-source-manager",
		ActorUserID: "legacy-source-manager", ActorRole: "manager",
	})
	if err != nil {
		t.Fatalf("seed previous version: %v", err)
	}
	body := []byte(`{"customer_name":"旧页面客户","customer_mobile":"13912345678","source_channel":"主动来电（老王）"}`)
	headers := map[string]string{"Content-Type": "application/json", "Idempotency-Key": key}
	replayed := doRaw(t, router, http.MethodPost, "/api/leads", token, body, headers)
	if replayed.Code != http.StatusCreated {
		t.Fatalf("old request replay status=%d body=%s", replayed.Code, replayed.Body.String())
	}
	var result struct {
		Replayed bool `json:"replayed"`
		Lead     struct {
			ID int64 `json:"id"`
		} `json:"lead"`
	}
	if err := json.Unmarshal(replayed.Body.Bytes(), &result); err != nil || !result.Replayed || result.Lead.ID != prior.Lead.ID {
		t.Fatalf("old request did not replay prior lead: %+v, %v", result, err)
	}
	headers["Idempotency-Key"] = "ac37af07-df5f-41c8-b77c-e0ea351367ce"
	blocked := doRaw(t, router, http.MethodPost, "/api/leads", token, body, headers)
	if blocked.Code != http.StatusUnprocessableEntity || !strings.Contains(blocked.Body.String(), "复制已填资料") || !strings.Contains(blocked.Body.String(), "刷新页面") {
		t.Fatalf("new request with retired source status=%d body=%s", blocked.Code, blocked.Body.String())
	}
	corrected := []byte(`{"customer_name":"旧页面客户","customer_mobile":"13912345678","source_channel":"主动来电（员工丙）"}`)
	if got := doRaw(t, router, http.MethodPost, "/api/leads", token, corrected, headers); got.Code != http.StatusCreated {
		t.Fatalf("valid retry after rejected source status=%d body=%s", got.Code, got.Body.String())
	}
	headers["Idempotency-Key"] = key
	changed := []byte(`{"customer_name":"变更内容","customer_mobile":"13912345678","source_channel":"主动来电（老王）"}`)
	if got := doRaw(t, router, http.MethodPost, "/api/leads", token, changed, headers); got.Code != http.StatusConflict {
		t.Fatalf("changed request with prior key status=%d body=%s", got.Code, got.Body.String())
	}
}

func TestConcurrentNativeCreateRequestsShareOneResult(t *testing.T) {
	r := newTestRouter(t)
	token := issueDevToken(t, r, "concurrent-idem", "manager")
	headers := map[string]string{"Content-Type": "application/json", "Idempotency-Key": "7d5acb67-8ce7-4aeb-9bd2-375fe6dba3ea"}
	payload := []byte(`{"customer_name":"并发录入","customer_mobile":"13912345678","source_channel":"好学校"}`)
	type result struct {
		status int
		id     int64
	}
	results := make(chan result, 6)
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			response := doRaw(t, r, http.MethodPost, "/api/leads", token, payload, headers)
			var body struct {
				Lead struct {
					ID int64 `json:"id"`
				} `json:"lead"`
			}
			json.Unmarshal(response.Body.Bytes(), &body)
			results <- result{response.Code, body.Lead.ID}
		}()
	}
	wg.Wait()
	close(results)
	var first int64
	for item := range results {
		if item.status != 201 || item.id <= 0 {
			t.Fatalf("concurrent request failed: %+v", item)
		}
		if first == 0 {
			first = item.id
		}
		if first != item.id {
			t.Fatalf("duplicate concurrent lead: %d/%d", first, item.id)
		}
	}
}
