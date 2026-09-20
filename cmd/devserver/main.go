package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Ar3secchim/api-homelab-public/internal/snapshot"
)

const defaultAddress = "127.0.0.1:8080"

func main() {
	address := flag.String("addr", defaultAddress, "local address used by the development server")
	corsOrigin := flag.String(
		"cors-origin",
		"http://localhost:5173",
		"frontend origin allowed during local development",
	)
	flag.Parse()

	if err := validateLocalAddress(*address); err != nil {
		log.Fatal(err)
	}

	server := &http.Server{
		Addr:              *address,
		Handler:           newHandler(*corsOrigin, time.Now),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       30 * time.Second,
	}

	shutdownContext, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()
	go func() {
		<-shutdownContext.Done()
		contextWithTimeout, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(contextWithTimeout); err != nil {
			log.Printf("development server shutdown failed: %v", err)
		}
	}()

	log.Printf("local snapshot API available at http://%s/snapshot.json", *address)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

func newHandler(corsOrigin string, clock func() time.Time) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/" {
			http.NotFound(response, request)
			return
		}
		http.Redirect(response, request, "/snapshot.json", http.StatusTemporaryRedirect)
	})
	mux.HandleFunc("/healthz", func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet && request.Method != http.MethodHead {
			response.Header().Set("Allow", "GET, HEAD")
			http.Error(response, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		response.Header().Set("Content-Type", "text/plain; charset=utf-8")
		response.WriteHeader(http.StatusOK)
		if request.Method == http.MethodGet {
			_, _ = response.Write([]byte("ok\n"))
		}
	})
	mux.HandleFunc("/snapshot.json", func(response http.ResponseWriter, request *http.Request) {
		setCORSHeaders(response, corsOrigin)
		if request.Method == http.MethodOptions {
			response.WriteHeader(http.StatusNoContent)
			return
		}
		if request.Method != http.MethodGet && request.Method != http.MethodHead {
			response.Header().Set("Allow", "GET, HEAD, OPTIONS")
			http.Error(response, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		body, err := sampleSnapshot(clock().UTC())
		if err != nil {
			http.Error(response, "unable to generate sample snapshot", http.StatusInternalServerError)
			return
		}
		response.Header().Set("Cache-Control", "no-store")
		response.Header().Set("Content-Type", "application/json; charset=utf-8")
		response.WriteHeader(http.StatusOK)
		if request.Method == http.MethodGet {
			_, _ = response.Write(body)
		}
	})
	return mux
}

func setCORSHeaders(response http.ResponseWriter, corsOrigin string) {
	response.Header().Set("Access-Control-Allow-Origin", corsOrigin)
	response.Header().Set("Access-Control-Allow-Methods", "GET, HEAD, OPTIONS")
	response.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	response.Header().Set("Vary", "Origin")
}

func sampleSnapshot(now time.Time) ([]byte, error) {
	lastSync := now.Add(-5 * time.Minute).Format(time.RFC3339)
	daysToRenewal := 45
	document := snapshot.Document{
		GeneratedAt: now.Truncate(time.Second).Format(time.RFC3339),
		GitOps: snapshot.GitOps{
			Applications: 8,
			Synced:       7,
			Healthy:      8,
			LastSyncAt:   &lastSync,
		},
		Scale: snapshot.Scale{
			Namespaces:  5,
			Workloads:   18,
			PodsRunning: 24,
		},
		TLS: snapshot.TLS{
			Certificates:      6,
			DaysToNextRenewal: &daysToRenewal,
		},
		Services: []string{"ArgoCD", "Traefik", "cert-manager", "Infisical"},
	}
	return snapshot.Encode(document)
}

func validateLocalAddress(address string) error {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("invalid development server address: %w", err)
	}
	if port == "" {
		return errors.New("development server port cannot be empty")
	}
	if host == "localhost" {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return errors.New("development server must bind to a loopback address")
	}
	return nil
}
