package endpoint

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCheckHealth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/health" {
			t.Fatalf("path = %q", request.URL.Path)
		}
		response.WriteHeader(http.StatusOK)
		_, _ = response.Write([]byte("ready"))
	}))
	defer server.Close()
	check := CheckHealth(context.Background(), server.URL+"/health", time.Second)
	if !check.OK || check.Status != http.StatusOK || check.Body != "ready" {
		t.Fatalf("health = %#v", check)
	}
}

func TestWaitForHealthPollsUntilReady(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		if attempts.Add(1) < 3 {
			http.Error(response, "loading", http.StatusServiceUnavailable)
			return
		}
		_, _ = response.Write([]byte("ready"))
	}))
	defer server.Close()
	check, err := WaitForHealth(context.Background(), server.URL, WaitOptions{
		Timeout: time.Second, PollInterval: 10 * time.Millisecond, RequestTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !check.OK || attempts.Load() != 3 {
		t.Fatalf("health = %#v, attempts = %d", check, attempts.Load())
	}
}

func TestWaitForHealthReportsLastResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		http.Error(response, "still loading\nmore detail", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	_, err := WaitForHealth(context.Background(), server.URL, WaitOptions{
		Timeout: 20 * time.Millisecond, PollInterval: 5 * time.Millisecond, RequestTimeout: time.Second,
	})
	if err == nil || !strings.Contains(err.Error(), "HTTP 503: still loading") {
		t.Fatalf("error = %v", err)
	}
}

func TestWaitForHealthHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := WaitForHealth(ctx, "http://127.0.0.1:1/health", WaitOptions{Timeout: time.Second})
	if err == nil || !strings.Contains(err.Error(), "aborted") {
		t.Fatalf("error = %v", err)
	}
}
