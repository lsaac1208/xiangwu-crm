package main

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

func TestWorkerHealthEndpointReturnsValidJSON(t *testing.T) {
	server, err := startWorkerMetricsServer("127.0.0.1:0")
	if err != nil {
		t.Fatalf("start metrics server: %v", err)
	}
	if server == nil {
		t.Fatal("metrics server is nil")
	}
	t.Cleanup(func() { _ = server.Close() })

	client := &http.Client{Timeout: time.Second}
	var resp *http.Response
	for i := 0; i < 20; i++ {
		resp, err = client.Get("http://" + server.Addr + "/health")
		if err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("get health: %v", err)
	}
	defer resp.Body.Close()

	var body struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode health response: %v", err)
	}
	if resp.StatusCode != http.StatusOK || body.Status != "ok" {
		t.Fatalf("health status=%d body=%+v", resp.StatusCode, body)
	}
}

func TestWorkerMetricsServerReportsBindFailure(t *testing.T) {
	first, err := startWorkerMetricsServer("127.0.0.1:0")
	if err != nil {
		t.Fatalf("start first server: %v", err)
	}
	t.Cleanup(func() { _ = first.Close() })

	second, err := startWorkerMetricsServer(first.Addr)
	if err == nil {
		if second != nil {
			_ = second.Close()
		}
		t.Fatal("second server unexpectedly bound the same address")
	}
}
