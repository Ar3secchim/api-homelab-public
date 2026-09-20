package tests

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestRBACDoesNotGrantSecretOrConfigMapAccess(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "k8s", "base", "rbac.yaml"))
	if err != nil {
		t.Fatalf("read RBAC manifest: %v", err)
	}
	manifest := string(body)
	for _, forbidden := range []string{"- secrets", "- configmaps", "- '*'"} {
		if strings.Contains(manifest, forbidden) {
			t.Errorf("RBAC manifest contains forbidden grant %q", forbidden)
		}
	}
}

func TestWorkflowsNeverUseSelfHostedRunner(t *testing.T) {
	actionReference := regexp.MustCompile(`uses:\s*[^@\s]+@([0-9a-f]{40})\s*(?:#.*)?$`)
	paths, err := filepath.Glob(filepath.Join("..", ".github", "workflows", "*.yml"))
	if err != nil {
		t.Fatalf("list workflows: %v", err)
	}
	for _, path := range paths {
		body, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatalf("read workflow: %v", readErr)
		}
		if strings.Contains(string(body), "self-hosted") {
			t.Errorf("workflow %s uses a self-hosted runner", path)
		}
		for lineNumber, line := range strings.Split(string(body), "\n") {
			if strings.Contains(line, "uses:") && !actionReference.MatchString(strings.TrimSpace(line)) {
				t.Errorf("workflow %s:%d does not pin an action by commit SHA", path, lineNumber+1)
			}
		}
	}
}

func TestPrivateOperationalFilesAreIgnored(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", ".gitignore"))
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	ignoreFile := string(body)
	patterns := []string{
		"docs/task/",
		"docs/private/",
		"k8s/overlays/private/",
		".env",
		".secrets/",
		"credentials/",
		"*.pem",
		"*.key",
		"kubeconfig.*",
		"*.tfstate",
	}
	for _, pattern := range patterns {
		if !strings.Contains(ignoreFile, pattern) {
			t.Errorf(".gitignore does not protect %q", pattern)
		}
	}
}

func TestDockerBuildContextUsesAnAllowlist(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", ".dockerignore"))
	if err != nil {
		t.Fatalf("read .dockerignore: %v", err)
	}
	lines := strings.Split(string(body), "\n")
	if !containsLine(lines, "**") {
		t.Fatal(".dockerignore does not deny the build context by default")
	}
	for _, allowed := range []string{"!go.mod", "!cmd/**", "!internal/**"} {
		if !containsLine(lines, allowed) {
			t.Errorf(".dockerignore does not allow required build input %q", allowed)
		}
	}
}

func containsLine(lines []string, expected string) bool {
	for _, line := range lines {
		if strings.TrimSpace(line) == expected {
			return true
		}
	}
	return false
}
