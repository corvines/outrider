package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGatewayOwnerEnsureSkipsStartWhenHealthy(t *testing.T) {
	var started bool
	owner := &gatewayOwner{
		endpoint: "http://127.0.0.1:11435",
		lookPath: func() (string, error) { return "/bin/outrider", nil },
		run: func(_ context.Context, _ string, args ...string) error {
			started = true
			t.Fatalf("ran %v", args)
			return nil
		},
		healthy: func(context.Context, string) bool { return true },
	}
	if err := owner.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	if started {
		t.Fatal("started an already healthy server")
	}
}

func TestGatewayOwnerEnsureStartsThenWaits(t *testing.T) {
	healthy := false
	var ran []string
	owner := &gatewayOwner{
		endpoint: "http://127.0.0.1:11435",
		lookPath: func() (string, error) { return "/bin/outrider", nil },
		run: func(_ context.Context, _ string, args ...string) error {
			ran = append(ran, args...)
			healthy = true
			return nil
		},
		healthy: func(context.Context, string) bool { return healthy },
	}
	if err := owner.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(ran) != 1 || ran[0] != "start" {
		t.Fatalf("ran = %v", ran)
	}
}

func TestGatewayOwnerStopRunsStop(t *testing.T) {
	var ran []string
	owner := &gatewayOwner{
		lookPath: func() (string, error) { return "/bin/outrider", nil },
		run: func(_ context.Context, _ string, args ...string) error {
			ran = args
			return nil
		},
	}
	if err := owner.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(ran) != 1 || ran[0] != "stop" {
		t.Fatalf("ran = %v", ran)
	}
}

func TestGatewayOwnerEnsureReturnsStartError(t *testing.T) {
	owner := &gatewayOwner{
		lookPath: func() (string, error) { return "/bin/outrider", nil },
		run: func(context.Context, string, ...string) error {
			return errors.New("port in use")
		},
		healthy: func(context.Context, string) bool { return false },
	}
	if err := owner.Ensure(context.Background()); err == nil || err.Error() != "port in use" {
		t.Fatalf("error = %v", err)
	}
}

func TestResolveOutriderBinaryUsesOverride(t *testing.T) {
	t.Setenv("OUTRIDER_BIN", "/tmp/outrider")
	got, err := resolveOutriderBinary()
	if err != nil {
		t.Fatal(err)
	}
	if got != "/tmp/outrider" {
		t.Fatalf("binary = %q", got)
	}
}

func TestGatewayHealthyReadsAdminStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	if !gatewayHealthy(context.Background(), server.URL) {
		t.Fatal("expected healthy gateway")
	}
}
