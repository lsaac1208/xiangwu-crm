package main

import (
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/lsaac128/kxone/pkg/types"
	"net/http"
	"sync"
	"testing"
)

func followupFixture(t *testing.T) (*gin.Engine, string, string) {
	t.Helper()
	r := newTestRouter(t)
	token := issueDevToken(t, r, "followup-idem-owner", "manager")
	created := doJSON(t, r, http.MethodPost, "/api/leads", token, map[string]any{"customer_name": "幂等跟进测试", "customer_mobile": "13912345678", "source_channel": "好学校"})
	if created.Code != 201 {
		t.Fatal(created.Body.String())
	}
	var b struct {
		Lead types.Lead `json:"lead"`
	}
	decodeResponse(t, created, &b)
	path := fmt.Sprintf("/api/leads/%d", b.Lead.ID)
	// Manager-created leads may enter the unassigned queue; use the normal workflow.
	if b.Lead.OwnerUserID != "followup-idem-owner" {
		resp := doJSON(t, r, http.MethodPost, path+"/assign", token, map[string]any{"sales_userid": "followup-idem-owner"})
		if resp.Code != 200 {
			t.Fatal(resp.Body.String())
		}
		resp = doJSON(t, r, http.MethodPost, path+"/claim", token, nil)
		if resp.Code != 200 {
			t.Fatal(resp.Body.String())
		}
	}
	return r, token, path
}
func followupCount(t *testing.T, r http.Handler, token, path string) int {
	t.Helper()
	resp := doJSON(t, r, http.MethodGet, path, token, nil)
	if resp.Code != 200 {
		t.Fatalf("detail %d: %s", resp.Code, resp.Body.String())
	}
	var b struct {
		Followups []json.RawMessage `json:"followups"`
	}
	decodeResponse(t, resp, &b)
	return len(b.Followups)
}
func TestFollowupHTTPRetryCreatesOneRecordAndRejectsChangedContent(t *testing.T) {
	r, token, path := followupFixture(t)
	body := []byte(`{"followup_result":"connected","remark":"同一次联系","is_valid":true}`)
	headers := map[string]string{"Content-Type": "application/json", "Idempotency-Key": "0614f960-ec81-4a6a-a7e0-d096f0524ca3"}
	for i := 0; i < 2; i++ {
		resp := doRaw(t, r, http.MethodPost, path+"/followups", token, body, headers)
		if resp.Code != 200 {
			t.Fatalf("submit/replay %d: %s", resp.Code, resp.Body.String())
		}
	}
	if n := followupCount(t, r, token, path); n != 1 {
		t.Fatalf("followups=%d, want 1", n)
	}
	resp := doRaw(t, r, http.MethodPost, path+"/followups", token, []byte(`{"followup_result":"connected","remark":"另一内容","is_valid":true}`), headers)
	if resp.Code != 409 {
		t.Fatalf("changed request=%d: %s", resp.Code, resp.Body.String())
	}
}
func TestFollowupHTTPMissingKeyNeverWrites(t *testing.T) {
	r, token, path := followupFixture(t)
	resp := doRaw(t, r, http.MethodPost, path+"/followups", token, []byte(`{"followup_result":"connected","remark":"不能无标识提交"}`), map[string]string{"Content-Type": "application/json"})
	if resp.Code != 422 {
		t.Fatalf("missing key=%d", resp.Code)
	}
	if n := followupCount(t, r, token, path); n != 0 {
		t.Fatalf("missing key wrote %d records", n)
	}
}
func TestConcurrentFollowupHTTPRetriesCommitOnce(t *testing.T) {
	r, token, path := followupFixture(t)
	var wg sync.WaitGroup
	status := make(chan int, 6)
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp := doRaw(t, r, http.MethodPost, path+"/followups", token, []byte(`{"followup_result":"connected","remark":"并发同一联系","is_valid":true}`), map[string]string{"Content-Type": "application/json", "Idempotency-Key": "06b3f5d7-3d38-4355-bc15-d90c7824fd60"})
			status <- resp.Code
		}()
	}
	wg.Wait()
	close(status)
	for s := range status {
		if s != 200 {
			t.Fatalf("retry status %d", s)
		}
	}
	if n := followupCount(t, r, token, path); n != 1 {
		t.Fatalf("concurrent retries wrote %d records", n)
	}
}
