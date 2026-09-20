package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/Ar3secchim/api-homelab-public/internal/kubernetes"
	"github.com/Ar3secchim/api-homelab-public/internal/r2"
	"github.com/Ar3secchim/api-homelab-public/internal/snapshot"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	if err := run(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "snapshot publication failed:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	client, err := kubernetes.NewInCluster()
	if err != nil {
		return err
	}

	applications, err := client.List(ctx, "/apis/argoproj.io/v1alpha1/applications")
	if err != nil {
		return err
	}
	namespaces, err := client.List(ctx, "/api/v1/namespaces")
	if err != nil {
		return err
	}
	pods, err := client.List(ctx, "/api/v1/pods")
	if err != nil {
		return err
	}
	certificates, err := client.List(ctx, "/apis/cert-manager.io/v1/certificates")
	if err != nil {
		return err
	}

	document, err := snapshot.Build(
		applications,
		namespaces,
		pods,
		certificates,
		time.Now(),
	)
	if err != nil {
		return err
	}
	body, err := snapshot.Encode(document)
	if err != nil {
		return err
	}

	config, err := r2ConfigFromEnvironment()
	if err != nil {
		return err
	}
	return r2.Upload(ctx, body, config, time.Now())
}

func r2ConfigFromEnvironment() (r2.Config, error) {
	accountID, err := requiredEnvironment("R2_ACCOUNT_ID")
	if err != nil {
		return r2.Config{}, err
	}
	bucket, err := requiredEnvironment("R2_BUCKET")
	if err != nil {
		return r2.Config{}, err
	}
	accessKeyID, err := requiredEnvironment("R2_ACCESS_KEY_ID")
	if err != nil {
		return r2.Config{}, err
	}
	secretAccessKey, err := requiredEnvironment("R2_SECRET_ACCESS_KEY")
	if err != nil {
		return r2.Config{}, err
	}
	objectKey := os.Getenv("R2_OBJECT_KEY")
	if objectKey == "" {
		objectKey = "snapshot.json"
	}

	config := r2.Config{
		AccountID:       accountID,
		Bucket:          bucket,
		AccessKeyID:     accessKeyID,
		SecretAccessKey: secretAccessKey,
		ObjectKey:       objectKey,
	}
	if err := config.Validate(); err != nil {
		return r2.Config{}, err
	}
	return config, nil
}

func requiredEnvironment(name string) (string, error) {
	value := os.Getenv(name)
	if value == "" {
		return "", errors.New("required environment variable is missing: " + name)
	}
	return value, nil
}
