package r2

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestConfigRejectsAccountIDThatCouldChangeDestination(t *testing.T) {
	config := Config{
		AccountID:       "example.invalid/path",
		Bucket:          "public-snapshot",
		AccessKeyID:     "access",
		SecretAccessKey: "secret",
		ObjectKey:       "snapshot.json",
	}
	if err := config.Validate(); err == nil {
		t.Fatal("Validate accepted an unsafe account ID")
	}
}

func TestSignedRequestUsesPutAndCacheHeaders(t *testing.T) {
	config := Config{
		AccountID:       "0123456789abcdef0123456789abcdef",
		Bucket:          "public-snapshot",
		AccessKeyID:     "access",
		SecretAccessKey: "secret",
		ObjectKey:       "snapshot.json",
	}
	request, err := newSignedRequest(
		context.Background(),
		[]byte("{}\n"),
		config,
		time.Date(2026, time.September, 20, 14, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("newSignedRequest returned an error: %v", err)
	}
	wantURL := "https://0123456789abcdef0123456789abcdef.r2.cloudflarestorage.com/" +
		"public-snapshot/snapshot.json"
	if request.URL.String() != wantURL {
		t.Errorf("request URL = %q, want %q", request.URL.String(), wantURL)
	}
	if request.Method != "PUT" {
		t.Errorf("request method = %q, want PUT", request.Method)
	}
	if request.Header.Get("Cache-Control") != "public, max-age=3600" {
		t.Error("request does not contain the expected cache policy")
	}
	if !strings.HasPrefix(request.Header.Get("Authorization"), "AWS4-HMAC-SHA256") {
		t.Error("request is not signed with AWS Signature Version 4")
	}
}
