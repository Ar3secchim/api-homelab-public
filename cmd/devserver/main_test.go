package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSnapshotEndpointServesSafeSample(t *testing.T) {
	now := time.Date(2026, time.September, 20, 14, 0, 0, 0, time.UTC)
	handler := newHandler("http://localhost:5173", func() time.Time { return now })
	request := httptest.NewRequest(http.MethodGet, "/snapshot.json", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if response.Header().Get("Access-Control-Allow-Origin") != "http://localhost:5173" {
		t.Error("response does not contain the configured local CORS origin")
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Error("local response should not be cached")
	}
	var document struct {
		GeneratedAt string `json:"generatedAt"`
		GitOps      struct {
			Applications int `json:"applications"`
		} `json:"gitops"`
		Scale struct {
			PodsRunning int `json:"podsRunning"`
		} `json:"scale"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &document); err != nil {
		t.Fatalf("decode local snapshot: %v", err)
	}
	if document.GeneratedAt != "2026-09-20T14:00:00Z" {
		t.Errorf("generatedAt = %q", document.GeneratedAt)
	}
	if document.GitOps.Applications != 8 || document.Scale.PodsRunning != 24 {
		t.Errorf("unexpected sample document: %+v", document)
	}
}

func TestSnapshotEndpointRejectsWrites(t *testing.T) {
	handler := newHandler("http://localhost:5173", time.Now)
	request := httptest.NewRequest(http.MethodPost, "/snapshot.json", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusMethodNotAllowed)
	}
}

func TestDevelopmentServerOnlyAcceptsLoopbackAddresses(t *testing.T) {
	for _, address := range []string{"127.0.0.1:8080", "localhost:8080", "[::1]:8080"} {
		if err := validateLocalAddress(address); err != nil {
			t.Errorf("validateLocalAddress(%q) returned an error: %v", address, err)
		}
	}
	for _, address := range []string{"0.0.0.0:8080", "192.0.2.10:8080", ":8080"} {
		if err := validateLocalAddress(address); err == nil {
			t.Errorf("validateLocalAddress(%q) accepted a non-loopback address", address)
		}
	}
}
