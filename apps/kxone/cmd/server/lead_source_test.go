package main

import (
	"net/http"
	"testing"
)

func TestManualLeadSourceIsRequiredAndMustBeAnOption(t *testing.T) {
	router := newTestRouter(t)
	token := issueDevToken(t, router, "manager_source", "manager")
	for _, value := range []any{nil, "", "   ", "手动录入", "ad:tencent", "座机主动来电", "主动来电（老王）", "自己上门的"} {
		body := map[string]any{"customer_name": "校验来源", "customer_mobile": "13912345678"}
		if value != nil {
			body["source_channel"] = value
		}
		response := doJSON(t, router, http.MethodPost, "/api/leads", token, body)
		if response.Code != http.StatusUnprocessableEntity {
			t.Fatalf("source=%v status=%d body=%s", value, response.Code, response.Body.String())
		}
	}
	for _, source := range []string{"主动来电（员工丙）", "自己上门", "襄五小红书（员工甲/员工丙）", "员工戊小红书（员工甲）"} {
		response := doJSON(t, router, http.MethodPost, "/api/leads", token, map[string]any{"customer_name": "合法来源", "customer_mobile": "13912345678", "source_channel": source})
		if response.Code != http.StatusCreated {
			t.Fatalf("source=%q status=%d body=%s", source, response.Code, response.Body.String())
		}
	}
}
