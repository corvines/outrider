package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
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
		started: true,
	}
	if err := owner.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(ran) != 1 || ran[0] != "stop" {
		t.Fatalf("ran = %v", ran)
	}
}

func TestGatewayOwnerAdoptsAndLeavesAnExternalGateway(t *testing.T) {
	var ran []string
	owner := &gatewayOwner{
		endpoint: "http://127.0.0.1:11435",
		lookPath: func() (string, error) { return "/bin/outrider", nil },
		run: func(_ context.Context, _ string, args ...string) error {
			ran = append(ran, args...)
			return nil
		},
		healthy: func(context.Context, string) bool { return true },
	}
	if err := owner.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := owner.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(ran) != 0 {
		t.Fatalf("ran %v against a gateway the app did not start", ran)
	}
}

func TestGatewayOwnerStopsWhatEnsureStarted(t *testing.T) {
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
	if err := owner.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(ran) != 2 || ran[0] != "start" || ran[1] != "stop" {
		t.Fatalf("ran = %v", ran)
	}
}

func TestGatewayOwnerDoesNotStopAfterAFailedStart(t *testing.T) {
	var ran []string
	owner := &gatewayOwner{
		endpoint: "http://127.0.0.1:11435",
		lookPath: func() (string, error) { return "/bin/outrider", nil },
		run: func(_ context.Context, _ string, args ...string) error {
			ran = append(ran, args...)
			return errors.New("port in use")
		},
		healthy: func(context.Context, string) bool { return false },
	}
	if err := owner.Ensure(context.Background()); err == nil {
		t.Fatal("expected a start error")
	}
	if err := owner.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(ran) != 1 || ran[0] != "start" {
		t.Fatalf("ran = %v", ran)
	}
}

func TestResolveOutriderBinaryPrefersASibling(t *testing.T) {
	t.Setenv("OUTRIDER_BIN", "")
	directory := t.TempDir()
	sibling := filepath.Join(directory, "outrider")
	if err := os.WriteFile(sibling, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := resolveSiblingBinary(filepath.Join(directory, "outrider-dashboard"))
	if err != nil {
		t.Fatal(err)
	}
	if got != sibling {
		t.Fatalf("binary = %q, want %q", got, sibling)
	}
}

// The app must never fall back to PATH: a Finder-launched bundle inherits
// launchd's PATH, where ~/.local/bin is absent.
func TestResolveOutriderBinaryDoesNotSearchPath(t *testing.T) {
	t.Setenv("OUTRIDER_BIN", "")
	directory := t.TempDir()
	onPath := filepath.Join(directory, "outrider")
	if err := os.WriteFile(onPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", directory)
	if _, err := resolveSiblingBinary(filepath.Join(t.TempDir(), "outrider-dashboard")); err == nil {
		t.Fatal("resolved a binary from PATH")
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

func TestInstallCommandLineToolLinksTheResolvedBinary(t *testing.T) {
	var commands [][]string
	owner := &gatewayOwner{
		lookPath: func() (string, error) { return "/Applications/Outrider.app/Contents/MacOS/outrider", nil },
		run: func(_ context.Context, binary string, args ...string) error {
			commands = append(commands, append([]string{binary}, args...))
			return nil
		},
	}
	target, err := owner.InstallCommandLineTool(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(commands) != 1 {
		t.Fatalf("commands = %v", commands)
	}
	want := []string{"/Applications/Outrider.app/Contents/MacOS/outrider", "install", "--link"}
	if !slices.Equal(commands[0], want) {
		t.Fatalf("command = %v, want %v", commands[0], want)
	}
	if filepath.Base(target) != "outrider" || !strings.HasSuffix(filepath.Dir(target), ".local/bin") {
		t.Fatalf("target = %s", target)
	}
}

func TestInstallCommandLineToolReportsAFailure(t *testing.T) {
	owner := &gatewayOwner{
		lookPath: func() (string, error) { return "/bin/outrider", nil },
		run: func(_ context.Context, _ string, _ ...string) error {
			return errors.New("refusing to replace")
		},
	}
	if _, err := owner.InstallCommandLineTool(context.Background()); err == nil {
		t.Fatal("a failed install was reported as a success")
	}
}
