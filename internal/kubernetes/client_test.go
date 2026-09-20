package kubernetes

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListUsesAuthenticationAndFollowsPagination(t *testing.T) {
	t.Helper()
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests++
		if request.Header.Get("Authorization") != "Bearer test-token" {
			t.Error("request did not include the service account token")
		}
		response.Header().Set("Content-Type", "application/json")
		if request.URL.Query().Get("continue") == "" {
			fmt.Fprint(response, `{"items":[{"metadata":{"name":"first"}}],"metadata":{"continue":"next"}}`)
			return
		}
		fmt.Fprint(response, `{"items":[{"metadata":{"name":"second"}}],"metadata":{}}`)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-token", server.Client())
	items, err := client.List(context.Background(), "/api/v1/pods")
	if err != nil {
		t.Fatalf("List returned an error: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("List returned %d items, want 2", len(items))
	}
	if requests != 2 {
		t.Fatalf("List made %d requests, want 2", requests)
	}
}
