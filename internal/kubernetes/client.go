package kubernetes

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	serviceAccountDirectory = "/var/run/secrets/kubernetes.io/serviceaccount"
	maximumResponseBytes    = 16 << 20
)

type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

type listResponse struct {
	Items    []map[string]any `json:"items"`
	Metadata struct {
		Continue string `json:"continue"`
	} `json:"metadata"`
}

func NewInCluster() (*Client, error) {
	host := os.Getenv("KUBERNETES_SERVICE_HOST")
	if host == "" {
		return nil, errors.New("KUBERNETES_SERVICE_HOST is not set")
	}
	port := os.Getenv("KUBERNETES_SERVICE_PORT_HTTPS")
	if port == "" {
		port = "443"
	}

	token, err := os.ReadFile(filepath.Join(serviceAccountDirectory, "token"))
	if err != nil {
		return nil, errors.New("read Kubernetes service account token")
	}
	certificate, err := os.ReadFile(filepath.Join(serviceAccountDirectory, "ca.crt"))
	if err != nil {
		return nil, errors.New("read Kubernetes service account CA")
	}
	roots, err := x509.SystemCertPool()
	if err != nil {
		roots = x509.NewCertPool()
	}
	if !roots.AppendCertsFromPEM(certificate) {
		return nil, errors.New("parse Kubernetes service account CA")
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{
		MinVersion: tls.VersionTLS12,
		RootCAs:    roots,
	}
	return &Client{
		baseURL: "https://" + net.JoinHostPort(host, port),
		token:   strings.TrimSpace(string(token)),
		httpClient: &http.Client{
			Timeout:   20 * time.Second,
			Transport: transport,
		},
	}, nil
}

func NewClient(baseURL, token string, httpClient *http.Client) *Client {
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		token:      token,
		httpClient: httpClient,
	}
}

func (client *Client) List(ctx context.Context, path string) ([]map[string]any, error) {
	items := make([]map[string]any, 0)
	continuation := ""

	for {
		requestURL, err := url.Parse(client.baseURL + path)
		if err != nil {
			return nil, fmt.Errorf("build Kubernetes request for %s: %w", path, err)
		}
		query := requestURL.Query()
		query.Set("limit", "500")
		if continuation != "" {
			query.Set("continue", continuation)
		}
		requestURL.RawQuery = query.Encode()

		request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL.String(), nil)
		if err != nil {
			return nil, fmt.Errorf("build Kubernetes request for %s: %w", path, err)
		}
		request.Header.Set("Accept", "application/json")
		request.Header.Set("Authorization", "Bearer "+client.token)

		response, err := client.httpClient.Do(request)
		if err != nil {
			return nil, fmt.Errorf("Kubernetes request failed for %s: %w", path, err)
		}
		page, decodeErr := decodeList(response)
		if decodeErr != nil {
			return nil, fmt.Errorf("Kubernetes request failed for %s: %w", path, decodeErr)
		}
		items = append(items, page.Items...)
		continuation = page.Metadata.Continue
		if continuation == "" {
			return items, nil
		}
	}
}

func decodeList(response *http.Response) (listResponse, error) {
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4<<10))
		return listResponse{}, fmt.Errorf("unexpected status %d", response.StatusCode)
	}

	var page listResponse
	decoder := json.NewDecoder(io.LimitReader(response.Body, maximumResponseBytes))
	if err := decoder.Decode(&page); err != nil {
		return listResponse{}, errors.New("decode Kubernetes list response")
	}
	if page.Items == nil {
		return listResponse{}, errors.New("Kubernetes response does not contain an items list")
	}
	return page, nil
}
