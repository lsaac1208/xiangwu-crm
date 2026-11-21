package agent

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSupplierContextFailureIsDistinctAndNeverFallsBack(t *testing.T) {
	for _, stream := range []bool{false, true} {
		supplier := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if stream {
				fmt.Fprint(w, "data: {\"error\":{\"code\":\"context_length_exceeded\"}}\n\ndata: [DONE]\n\n")
			} else {
				w.WriteHeader(500)
				fmt.Fprint(w, `{"error":{"code":"context_window_exceeded","message":"private diagnostic"}}`)
			}
		}))
		cfg := DefaultConfig()
		cfg.BaseURL = supplier.URL
		_, err := callModel(t.Context(), cfg, true, ModelRequest{Stream: stream}, nil)
		supplier.Close()
		if err == nil || canFallback(err) || runErrorCode(err.Error()) != "provider_context_limit" {
			t.Fatalf("context failure incorrectly classified or switched: %v", err)
		}
	}
}

func TestStreamingProxyContextTypeOverridesGenericRetryMessage(t *testing.T) {
	supplier := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, "event: error\ndata: {\"error\":{\"type\":\"context_exceeded_error\",\"message\":\"Upstream temporarily failed; retry later\"}}\n\n")
	}))
	defer supplier.Close()
	cfg := DefaultConfig()
	cfg.BaseURL = supplier.URL
	_, err := callModel(t.Context(), cfg, true, ModelRequest{Stream: true}, nil)
	if err == nil || canFallback(err) || runErrorCode(err.Error()) != "provider_context_limit" {
		t.Fatalf("explicit proxy context type must remain a context rejection: %v", err)
	}
	if providerContextError([]byte(`{"error":{"type":"upstream_error","message":"context_exceeded_error"}}`)) {
		t.Fatal("free text must not override structured provider error classification")
	}
}
