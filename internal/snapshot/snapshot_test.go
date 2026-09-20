package snapshot

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

var testNow = time.Date(2026, time.September, 20, 14, 0, 0, 0, time.UTC)

func TestBuildPublishesOnlyThePublicContract(t *testing.T) {
	applications := []map[string]any{
		{
			"metadata": map[string]any{"name": "private-application-name"},
			"status": map[string]any{
				"sync":           map[string]any{"status": "Synced"},
				"health":         map[string]any{"status": "Healthy"},
				"operationState": map[string]any{"finishedAt": "2026-09-20T13:56:00Z"},
			},
		},
		{
			"metadata": map[string]any{"name": "another-private-name"},
			"status": map[string]any{
				"sync":   map[string]any{"status": "OutOfSync"},
				"health": map[string]any{"status": "Degraded"},
				"history": []any{
					map[string]any{"deployedAt": "2026-09-20T12:00:00Z"},
				},
			},
		},
	}
	pods := []map[string]any{
		{
			"metadata": map[string]any{
				"name":      "private-api-6f8b7c9d4f-abcde",
				"namespace": "private-namespace",
				"labels":    map[string]any{"pod-template-hash": "6f8b7c9d4f"},
				"ownerReferences": []any{
					map[string]any{
						"controller": true,
						"kind":       "ReplicaSet",
						"name":       "private-api-6f8b7c9d4f",
					},
				},
			},
			"status": map[string]any{"phase": "Running"},
		},
		{
			"metadata": map[string]any{
				"name":      "private-api-6f8b7c9d4f-fghij",
				"namespace": "private-namespace",
				"labels":    map[string]any{"pod-template-hash": "6f8b7c9d4f"},
				"ownerReferences": []any{
					map[string]any{
						"controller": true,
						"kind":       "ReplicaSet",
						"name":       "private-api-6f8b7c9d4f",
					},
				},
			},
			"status": map[string]any{"phase": "Pending"},
		},
	}
	certificates := []map[string]any{
		{
			"metadata": map[string]any{"name": "private-certificate"},
			"status":   map[string]any{"renewalTime": "2026-10-20T14:00:00Z"},
		},
	}

	document, err := Build(
		applications,
		[]map[string]any{{"metadata": map[string]any{"name": "private-namespace"}}},
		pods,
		certificates,
		testNow,
	)
	if err != nil {
		t.Fatalf("Build returned an error: %v", err)
	}
	if document.GitOps.Applications != 2 || document.GitOps.Synced != 1 || document.GitOps.Healthy != 1 {
		t.Errorf("unexpected GitOps counts: %+v", document.GitOps)
	}
	if document.GitOps.LastSyncAt == nil || *document.GitOps.LastSyncAt != "2026-09-20T13:56:00Z" {
		t.Errorf("unexpected last sync: %v", document.GitOps.LastSyncAt)
	}
	if document.Scale != (Scale{Namespaces: 1, Workloads: 1, PodsRunning: 1}) {
		t.Errorf("unexpected scale: %+v", document.Scale)
	}
	if document.TLS.DaysToNextRenewal == nil || *document.TLS.DaysToNextRenewal != 30 {
		t.Errorf("unexpected renewal interval: %v", document.TLS.DaysToNextRenewal)
	}

	body, err := Encode(document)
	if err != nil {
		t.Fatalf("Encode returned an error: %v", err)
	}
	if strings.Contains(string(body), "private-") {
		t.Fatal("encoded snapshot leaked a discovered resource name")
	}
	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("encoded snapshot is not valid JSON: %v", err)
	}
	wantKeys := []string{"generatedAt", "gitops", "scale", "tls", "services"}
	for _, key := range wantKeys {
		if _, found := decoded[key]; !found {
			t.Errorf("encoded snapshot is missing %q", key)
		}
	}
	if len(decoded) != len(wantKeys) {
		t.Errorf("encoded snapshot has %d top-level fields, want %d", len(decoded), len(wantKeys))
	}
}

func TestValidateNoForbiddenContentRejectsSensitivePatterns(t *testing.T) {
	tests := map[string]string{
		"private address":  `{"value":"10.12.0.9"}`,
		"internal domain":  `{"value":"service.internal.homelab"}`,
		"cluster domain":   `{"value":"service.namespace.svc.cluster.local"}`,
		"database user":    `{"value":"application_user"}`,
		"software version": `{"value":"component 1.2.3"}`,
		"NodePort field":   `{"nodePort":30080}`,
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			if err := ValidateNoForbiddenContent([]byte(body)); err == nil {
				t.Fatal("validator accepted forbidden content")
			}
		})
	}
}

func TestValidateNoForbiddenContentAcceptsContractTimestamps(t *testing.T) {
	body := []byte(`{"generatedAt":"2026-09-20T14:00:00Z","value":"healthy"}`)
	if err := ValidateNoForbiddenContent(body); err != nil {
		t.Fatalf("validator rejected safe content: %v", err)
	}
}

func TestValidateRejectsChangedServiceAllowlist(t *testing.T) {
	document, err := Build(nil, nil, nil, nil, testNow)
	if err != nil {
		t.Fatalf("Build returned an error: %v", err)
	}
	document.Services = append(document.Services, "Unapproved")
	if err := document.Validate(); err == nil {
		t.Fatal("Validate accepted a changed service allowlist")
	}
}
