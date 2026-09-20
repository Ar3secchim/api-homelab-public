package r2

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var (
	accountIDPattern = regexp.MustCompile(`^[a-fA-F0-9]{32}$`)
	bucketPattern    = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,61}[a-z0-9]$`)
)

type Config struct {
	AccountID       string
	Bucket          string
	AccessKeyID     string
	SecretAccessKey string
	ObjectKey       string
}

func (config Config) Validate() error {
	if !accountIDPattern.MatchString(config.AccountID) {
		return errors.New("R2_ACCOUNT_ID must be a 32-character hexadecimal ID")
	}
	if !bucketPattern.MatchString(config.Bucket) {
		return errors.New("R2_BUCKET is not a valid bucket name")
	}
	if config.AccessKeyID == "" || config.SecretAccessKey == "" {
		return errors.New("R2 credentials cannot be empty")
	}
	if config.ObjectKey == "" || strings.HasPrefix(config.ObjectKey, "/") {
		return errors.New("R2_OBJECT_KEY must be a relative object key")
	}
	for _, segment := range strings.Split(config.ObjectKey, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return errors.New("R2_OBJECT_KEY contains an invalid path segment")
		}
	}
	return nil
}

func Upload(ctx context.Context, body []byte, config Config, now time.Time) error {
	request, err := newSignedRequest(ctx, body, config, now)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 30 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return errors.New("R2 upload failed due to a network error")
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4<<10))
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusCreated {
		return fmt.Errorf("R2 upload failed with status %d", response.StatusCode)
	}
	return nil
}

func newSignedRequest(
	ctx context.Context,
	body []byte,
	config Config,
	now time.Time,
) (*http.Request, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}

	now = now.UTC()
	amzDate := now.Format("20060102T150405Z")
	date := now.Format("20060102")
	host := config.AccountID + ".r2.cloudflarestorage.com"
	canonicalURI := "/" + url.PathEscape(config.Bucket) + "/" + escapeObjectKey(config.ObjectKey)
	payloadHash := sha256Hex(body)
	canonicalHeaders := strings.Join([]string{
		"cache-control:public, max-age=3600\n",
		"content-type:application/json\n",
		"host:" + host + "\n",
		"x-amz-content-sha256:" + payloadHash + "\n",
		"x-amz-date:" + amzDate + "\n",
	}, "")
	signedHeaders := "cache-control;content-type;host;x-amz-content-sha256;x-amz-date"
	canonicalRequest := strings.Join([]string{
		http.MethodPut,
		canonicalURI,
		"",
		canonicalHeaders,
		signedHeaders,
		payloadHash,
	}, "\n")
	scope := date + "/auto/s3/aws4_request"
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		scope,
		sha256Hex([]byte(canonicalRequest)),
	}, "\n")
	signature := hex.EncodeToString(hmacSHA256(signingKey(config.SecretAccessKey, date), stringToSign))
	authorization := "AWS4-HMAC-SHA256 Credential=" + config.AccessKeyID + "/" + scope +
		", SignedHeaders=" + signedHeaders + ", Signature=" + signature

	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPut,
		"https://"+host+canonicalURI,
		bytes.NewReader(body),
	)
	if err != nil {
		return nil, fmt.Errorf("build R2 upload request: %w", err)
	}
	request.Host = host
	request.Header.Set("Authorization", authorization)
	request.Header.Set("Cache-Control", "public, max-age=3600")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Amz-Content-Sha256", payloadHash)
	request.Header.Set("X-Amz-Date", amzDate)
	return request, nil
}

func escapeObjectKey(key string) string {
	segments := strings.Split(key, "/")
	for index, segment := range segments {
		segments[index] = url.PathEscape(segment)
	}
	return strings.Join(segments, "/")
}

func signingKey(secret, date string) []byte {
	dateKey := hmacSHA256([]byte("AWS4"+secret), date)
	regionKey := hmacSHA256(dateKey, "auto")
	serviceKey := hmacSHA256(regionKey, "s3")
	return hmacSHA256(serviceKey, "aws4_request")
}

func hmacSHA256(key []byte, value string) []byte {
	digest := hmac.New(sha256.New, key)
	_, _ = digest.Write([]byte(value))
	return digest.Sum(nil)
}

func sha256Hex(value []byte) string {
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:])
}
