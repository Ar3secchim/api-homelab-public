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

const (
	currentSchemaVersion = 2
	snapshotValidity     = 3 * time.Hour
)

type Document struct {
	SchemaVersion int      `json:"schemaVersion"`
	GeneratedAt   string   `json:"generatedAt"`
	ValidUntil    string   `json:"validUntil"`
	Status        string   `json:"status"`
	GitOps        GitOps   `json:"gitops"`
	Scale         Scale    `json:"scale"`
	TLS           TLS      `json:"tls"`
	Services      []string `json:"services"`
}

type GitOps struct {
	Applications    int     `json:"applications"`
	Synced          int     `json:"synced"`
	OutOfSync       int     `json:"outOfSync"`
	Healthy         int     `json:"healthy"`
	Degraded        int     `json:"degraded"`
	AutoSyncEnabled int     `json:"autoSyncEnabled"`
	LastSyncAt      *string `json:"lastSyncAt"`
}

type Scale struct {
	Namespaces      int           `json:"namespaces"`
	NodesObserved   int           `json:"nodesObserved"`
	Workloads       int           `json:"workloads"`
	WorkloadsByKind WorkloadKinds `json:"workloadsByKind"`
	PodsRunning     int           `json:"podsRunning"`
	Pods            PodSummary    `json:"pods"`
}

type WorkloadKinds struct {
	Deployments  int `json:"deployments"`
	StatefulSets int `json:"statefulSets"`
	DaemonSets   int `json:"daemonSets"`
	Other        int `json:"other"`
}

type PodSummary struct {
	Total     int `json:"total"`
	Running   int `json:"running"`
	Ready     int `json:"ready"`
	Pending   int `json:"pending"`
	Succeeded int `json:"succeeded"`
	Failed    int `json:"failed"`
}

type TLS struct {
	Certificates         int  `json:"certificates"`
	Ready                int  `json:"ready"`
	NotReady             int  `json:"notReady"`
	ExpiringWithin30Days int  `json:"expiringWithin30Days"`
	DaysToNextRenewal    *int `json:"daysToNextRenewal"`
}

func Build(
	applications []map[string]any,
	namespaces []map[string]any,
	pods []map[string]any,
	certificates []map[string]any,
	now time.Time,
) (Document, error) {
	now = now.UTC().Truncate(time.Second)

	gitOps := GitOps{Applications: len(applications)}
	for _, application := range applications {
		status := object(application["status"])
		switch stringValue(object(status["sync"])["status"]) {
		case "Synced":
			gitOps.Synced++
		case "OutOfSync":
			gitOps.OutOfSync++
		}
		switch stringValue(object(status["health"])["status"]) {
		case "Healthy":
			gitOps.Healthy++
		case "Degraded":
			gitOps.Degraded++
		}
		syncPolicy := object(object(application["spec"])["syncPolicy"])
		if _, found := syncPolicy["automated"]; found {
			gitOps.AutoSyncEnabled++
		}
	}
	gitOps.LastSyncAt = latestSync(applications)

	podSummary := PodSummary{Total: len(pods)}
	workloads := make(map[string]string)
	nodes := make(map[string]struct{})
	for _, pod := range pods {
		phase := stringValue(object(pod["status"])["phase"])
		switch phase {
		case "Running":
			podSummary.Running++
			if hasCondition(pod, "Ready", "True") {
				podSummary.Ready++
			}
		case "Pending":
			podSummary.Pending++
		case "Succeeded":
			podSummary.Succeeded++
		case "Failed":
			podSummary.Failed++
		}
		if phase != "Succeeded" && phase != "Failed" {
			identity := workloadForPod(pod)
			workloads[identity.key] = identity.kind
			nodeName := stringValue(object(pod["spec"])["nodeName"])
			if nodeName != "" {
				nodes[nodeName] = struct{}{}
			}
		}
	}
	workloadKinds := summarizeWorkloadKinds(workloads)
	tls := summarizeCertificates(certificates, now)

	document := Document{
		SchemaVersion: currentSchemaVersion,
		GeneratedAt:   now.Format(time.RFC3339),
		ValidUntil:    now.Add(snapshotValidity).Format(time.RFC3339),
		GitOps:        gitOps,
		Scale: Scale{
			Namespaces:      len(namespaces),
			NodesObserved:   len(nodes),
			Workloads:       len(workloads),
			WorkloadsByKind: workloadKinds,
			PodsRunning:     podSummary.Running,
			Pods:            podSummary,
		},
		TLS:      tls,
		Services: slices.Clone(publicServices),
	}
	document.Status = overallStatus(document.GitOps, document.TLS)

	if err := document.Validate(); err != nil {
		return Document{}, err
	}
	return document, nil
}

func (document Document) Validate() error {
	if document.SchemaVersion != currentSchemaVersion {
		return fmt.Errorf("schemaVersion must be %d", currentSchemaVersion)
	}
	generatedAt, err := time.Parse(time.RFC3339, document.GeneratedAt)
	if err != nil {
		return errors.New("generatedAt must be an RFC3339 timestamp")
	}
	validUntil, err := time.Parse(time.RFC3339, document.ValidUntil)
	if err != nil {
		return errors.New("validUntil must be an RFC3339 timestamp")
	}
	if validUntil.Sub(generatedAt) != snapshotValidity {
		return errors.New("validUntil must be three hours after generatedAt")
	}
	if document.GitOps.LastSyncAt != nil {
		if _, err := time.Parse(time.RFC3339, *document.GitOps.LastSyncAt); err != nil {
			return errors.New("lastSyncAt must be null or an RFC3339 timestamp")
		}
	}
	counts := []int{
		document.GitOps.Applications,
		document.GitOps.Synced,
		document.GitOps.OutOfSync,
		document.GitOps.Healthy,
		document.GitOps.Degraded,
		document.GitOps.AutoSyncEnabled,
		document.Scale.Namespaces,
		document.Scale.NodesObserved,
		document.Scale.Workloads,
		document.Scale.WorkloadsByKind.Deployments,
		document.Scale.WorkloadsByKind.StatefulSets,
		document.Scale.WorkloadsByKind.DaemonSets,
		document.Scale.WorkloadsByKind.Other,
		document.Scale.PodsRunning,
		document.Scale.Pods.Total,
		document.Scale.Pods.Running,
		document.Scale.Pods.Ready,
		document.Scale.Pods.Pending,
		document.Scale.Pods.Succeeded,
		document.Scale.Pods.Failed,
		document.TLS.Certificates,
		document.TLS.Ready,
		document.TLS.NotReady,
		document.TLS.ExpiringWithin30Days,
	}
	for _, count := range counts {
		if count < 0 {
			return errors.New("snapshot counts cannot be negative")
		}
	}
	if document.GitOps.Synced > document.GitOps.Applications {
		return errors.New("synced applications exceed the total")
	}
	if document.GitOps.OutOfSync > document.GitOps.Applications ||
		document.GitOps.Synced+document.GitOps.OutOfSync > document.GitOps.Applications {
		return errors.New("application sync states exceed the total")
	}
	if document.GitOps.Healthy > document.GitOps.Applications {
		return errors.New("healthy applications exceed the total")
	}
	if document.GitOps.Degraded > document.GitOps.Applications ||
		document.GitOps.Healthy+document.GitOps.Degraded > document.GitOps.Applications {
		return errors.New("application health states exceed the total")
	}
	if document.GitOps.AutoSyncEnabled > document.GitOps.Applications {
		return errors.New("automatic sync applications exceed the total")
	}
	workloadTotal := document.Scale.WorkloadsByKind.Deployments +
		document.Scale.WorkloadsByKind.StatefulSets +
		document.Scale.WorkloadsByKind.DaemonSets +
		document.Scale.WorkloadsByKind.Other
	if workloadTotal != document.Scale.Workloads {
		return errors.New("workload kind counts do not match the total")
	}
	if document.Scale.PodsRunning != document.Scale.Pods.Running {
		return errors.New("podsRunning does not match pods.running")
	}
	if document.Scale.Pods.Ready > document.Scale.Pods.Running {
		return errors.New("ready pods exceed running pods")
	}
	knownPodPhases := document.Scale.Pods.Running + document.Scale.Pods.Pending +
		document.Scale.Pods.Succeeded + document.Scale.Pods.Failed
	if knownPodPhases > document.Scale.Pods.Total {
		return errors.New("pod phase counts exceed the total")
	}
	if document.Scale.NodesObserved > document.Scale.Pods.Total {
		return errors.New("observed nodes exceed total pods")
	}
	if document.TLS.Ready+document.TLS.NotReady != document.TLS.Certificates {
		return errors.New("certificate readiness counts do not match the total")
	}
	if document.TLS.ExpiringWithin30Days > document.TLS.Certificates {
		return errors.New("expiring certificates exceed the total")
	}
	if document.Status != overallStatus(document.GitOps, document.TLS) {
		return errors.New("status does not match the aggregate health")
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

type workloadIdentity struct {
	key  string
	kind string
}

func workloadForPod(pod map[string]any) workloadIdentity {
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

	return workloadIdentity{
		key:  strings.Join([]string{namespace, kind, name}, "\x00"),
		kind: kind,
	}
}

func summarizeWorkloadKinds(workloads map[string]string) WorkloadKinds {
	var summary WorkloadKinds
	for _, kind := range workloads {
		switch kind {
		case "Deployment":
			summary.Deployments++
		case "StatefulSet":
			summary.StatefulSets++
		case "DaemonSet":
			summary.DaemonSets++
		default:
			summary.Other++
		}
	}
	return summary
}

func hasCondition(resource map[string]any, conditionType, conditionStatus string) bool {
	for _, entry := range array(object(resource["status"])["conditions"]) {
		condition := object(entry)
		if stringValue(condition["type"]) == conditionType &&
			stringValue(condition["status"]) == conditionStatus {
			return true
		}
	}
	return false
}

func summarizeCertificates(certificates []map[string]any, now time.Time) TLS {
	summary := TLS{
		Certificates:      len(certificates),
		DaysToNextRenewal: daysToNextRenewal(certificates, now),
	}
	deadline := now.Add(30 * 24 * time.Hour)
	for _, certificate := range certificates {
		if hasCondition(certificate, "Ready", "True") {
			summary.Ready++
		} else {
			summary.NotReady++
		}
		renewalTime, err := time.Parse(
			time.RFC3339,
			stringValue(object(certificate["status"])["renewalTime"]),
		)
		if err == nil && !renewalTime.After(deadline) {
			summary.ExpiringWithin30Days++
		}
	}
	return summary
}

func overallStatus(gitOps GitOps, tls TLS) string {
	if gitOps.Applications == 0 {
		return "unknown"
	}
	if gitOps.Synced != gitOps.Applications ||
		gitOps.Healthy != gitOps.Applications ||
		tls.NotReady > 0 {
		return "attention"
	}
	return "healthy"
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
