package snapshot

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/netip"
	"regexp"
	"slices"
	"strings"
	"time"
)

var (
	publicServices  = []string{"ArgoCD", "Traefik", "cert-manager", "Infisical"}
	privateNetworks = []netip.Prefix{
		netip.MustParsePrefix("10.0.0.0/8"),
		netip.MustParsePrefix("172.16.0.0/12"),
		netip.MustParsePrefix("192.168.0.0/16"),
	}
	ipv4Candidate = regexp.MustCompile(`(?:[0-9]{1,3}\.){3}[0-9]{1,3}`)
	forbiddenText = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\.homelab(?:\b|$)`),
		regexp.MustCompile(`(?i)\.svc\.cluster\.local(?:\b|$)`),
		regexp.MustCompile(`(?i)\b(?:nodeport|projectslug|secretspath)\b`),
		regexp.MustCompile(`\b[vV]?[0-9]+\.[0-9]+(?:\.[0-9]+)?(?:[-+][0-9A-Za-z.-]+)?\b`),
		regexp.MustCompile(`(?i)\b[a-z][a-z0-9-]*(?:_user|_ro)\b`),
	}
)

type Document struct {
	GeneratedAt string   `json:"generatedAt"`
	GitOps      GitOps   `json:"gitops"`
	Scale       Scale    `json:"scale"`
	TLS         TLS      `json:"tls"`
	Services    []string `json:"services"`
}

type GitOps struct {
	Applications int     `json:"applications"`
	Synced       int     `json:"synced"`
	Healthy      int     `json:"healthy"`
	LastSyncAt   *string `json:"lastSyncAt"`
}

type Scale struct {
	Namespaces  int `json:"namespaces"`
	Workloads   int `json:"workloads"`
	PodsRunning int `json:"podsRunning"`
}

type TLS struct {
	Certificates      int  `json:"certificates"`
	DaysToNextRenewal *int `json:"daysToNextRenewal"`
}

func Build(
	applications []map[string]any,
	namespaces []map[string]any,
	pods []map[string]any,
	certificates []map[string]any,
	now time.Time,
) (Document, error) {
	now = now.UTC().Truncate(time.Second)

	synced := 0
	healthy := 0
	for _, application := range applications {
		status := object(application["status"])
		if stringValue(object(status["sync"])["status"]) == "Synced" {
			synced++
		}
		if stringValue(object(status["health"])["status"]) == "Healthy" {
			healthy++
		}
	}

	running := 0
	workloads := make(map[string]struct{})
	for _, pod := range pods {
		phase := stringValue(object(pod["status"])["phase"])
		if phase == "Running" {
			running++
		}
		if phase != "Succeeded" && phase != "Failed" {
			workloads[workloadKey(pod)] = struct{}{}
		}
	}

	document := Document{
		GeneratedAt: now.Format(time.RFC3339),
		GitOps: GitOps{
			Applications: len(applications),
			Synced:       synced,
			Healthy:      healthy,
			LastSyncAt:   latestSync(applications),
		},
		Scale: Scale{
			Namespaces:  len(namespaces),
			Workloads:   len(workloads),
			PodsRunning: running,
		},
		TLS: TLS{
			Certificates:      len(certificates),
			DaysToNextRenewal: daysToNextRenewal(certificates, now),
		},
		Services: slices.Clone(publicServices),
	}

	if err := document.Validate(); err != nil {
		return Document{}, err
	}
	return document, nil
}

func (document Document) Validate() error {
	if _, err := time.Parse(time.RFC3339, document.GeneratedAt); err != nil {
		return errors.New("generatedAt must be an RFC3339 timestamp")
	}
	if document.GitOps.LastSyncAt != nil {
		if _, err := time.Parse(time.RFC3339, *document.GitOps.LastSyncAt); err != nil {
			return errors.New("lastSyncAt must be null or an RFC3339 timestamp")
		}
	}
	counts := []int{
		document.GitOps.Applications,
		document.GitOps.Synced,
		document.GitOps.Healthy,
		document.Scale.Namespaces,
		document.Scale.Workloads,
		document.Scale.PodsRunning,
		document.TLS.Certificates,
	}
	for _, count := range counts {
		if count < 0 {
			return errors.New("snapshot counts cannot be negative")
		}
	}
	if document.GitOps.Synced > document.GitOps.Applications {
		return errors.New("synced applications exceed the total")
	}
	if document.GitOps.Healthy > document.GitOps.Applications {
		return errors.New("healthy applications exceed the total")
	}
	if !slices.Equal(document.Services, publicServices) {
		return errors.New("services do not match the fixed public allowlist")
	}
	return nil
}

func Encode(document Document) ([]byte, error) {
	if err := document.Validate(); err != nil {
		return nil, err
	}
	body, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode snapshot: %w", err)
	}
	body = append(body, '\n')
	if err := ValidateNoForbiddenContent(body); err != nil {
		return nil, err
	}
	return body, nil
}

func ValidateNoForbiddenContent(body []byte) error {
	for _, candidate := range ipv4Candidate.FindAll(body, -1) {
		address, err := netip.ParseAddr(string(candidate))
		if err != nil {
			continue
		}
		for _, network := range privateNetworks {
			if network.Contains(address) {
				return errors.New("snapshot contains a private IP address")
			}
		}
	}
	for _, pattern := range forbiddenText {
		if pattern.Match(body) {
			return errors.New("snapshot contains forbidden text")
		}
	}
	return nil
}

func latestSync(applications []map[string]any) *string {
	var latest time.Time
	for _, application := range applications {
		status := object(application["status"])
		operationState := object(status["operationState"])
		latest = laterTimestamp(latest, stringValue(operationState["finishedAt"]))

		for _, entry := range array(status["history"]) {
			latest = laterTimestamp(latest, stringValue(object(entry)["deployedAt"]))
		}
	}
	if latest.IsZero() {
		return nil
	}
	formatted := latest.UTC().Format(time.RFC3339)
	return &formatted
}

func laterTimestamp(current time.Time, candidate string) time.Time {
	parsed, err := time.Parse(time.RFC3339, candidate)
	if err == nil && parsed.After(current) {
		return parsed
	}
	return current
}

func workloadKey(pod map[string]any) string {
	metadata := object(pod["metadata"])
	namespace := stringValue(metadata["namespace"])
	name := stringValue(metadata["name"])
	kind := "Pod"

	for _, reference := range array(metadata["ownerReferences"]) {
		owner := object(reference)
		controller, _ := owner["controller"].(bool)
		if !controller {
			continue
		}
		kind = stringValue(owner["kind"])
		name = stringValue(owner["name"])
		if kind == "ReplicaSet" {
			labels := object(metadata["labels"])
			hash := stringValue(labels["pod-template-hash"])
			if hash != "" && strings.HasSuffix(name, "-"+hash) {
				kind = "Deployment"
				name = strings.TrimSuffix(name, "-"+hash)
			}
		}
		break
	}

	return strings.Join([]string{namespace, kind, name}, "\x00")
}

func daysToNextRenewal(certificates []map[string]any, now time.Time) *int {
	var next time.Time
	for _, certificate := range certificates {
		status := object(certificate["status"])
		candidate, err := time.Parse(time.RFC3339, stringValue(status["renewalTime"]))
		if err == nil && (next.IsZero() || candidate.Before(next)) {
			next = candidate
		}
	}
	if next.IsZero() {
		return nil
	}
	days := int(math.Floor(next.Sub(now).Hours() / 24))
	return &days
}

func object(value any) map[string]any {
	result, _ := value.(map[string]any)
	return result
}

func array(value any) []any {
	result, _ := value.([]any)
	return result
}

func stringValue(value any) string {
	result, _ := value.(string)
	return result
}
