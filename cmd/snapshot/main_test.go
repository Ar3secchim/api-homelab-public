package main

import (
	"strings"
	"testing"
)

func TestR2ConfigFromEnvironment(t *testing.T) {
	t.Setenv("R2_ACCOUNT_ID", "0123456789abcdef0123456789abcdef")
	t.Setenv("R2_BUCKET", "public-snapshot")
	t.Setenv("R2_ACCESS_KEY_ID", "access")
	t.Setenv("R2_SECRET_ACCESS_KEY", "secret")
	t.Setenv("R2_OBJECT_KEY", "")

	config, err := r2ConfigFromEnvironment()
	if err != nil {
		t.Fatalf("r2ConfigFromEnvironment returned an error: %v", err)
	}
	if config.ObjectKey != "snapshot.json" {
		t.Errorf("object key = %q, want snapshot.json", config.ObjectKey)
	}
}

func TestMissingEnvironmentErrorDoesNotIncludeCredentialValues(t *testing.T) {
	t.Setenv("R2_ACCOUNT_ID", "")
	t.Setenv("R2_BUCKET", "")
	t.Setenv("R2_ACCESS_KEY_ID", "")
	t.Setenv("R2_SECRET_ACCESS_KEY", "")

	_, err := r2ConfigFromEnvironment()
	if err == nil {
		t.Fatal("r2ConfigFromEnvironment accepted missing configuration")
	}
	if strings.Contains(err.Error(), "secret") {
		t.Fatalf("error unexpectedly contains a credential value: %v", err)
	}
}
