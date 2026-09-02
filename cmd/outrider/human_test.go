package main

import (
	"strings"
	"testing"

	runnerprocess "github.com/corvines/outrider/internal/process"
)

func TestHumanProfileListShowsOperationalFields(t *testing.T) {
	output := profileListOutput{Profiles: []profileSummaryOutput{{
		ID: "qwen35b-mtp", Runnable: true, Description: "primary local agent",
		SizeBytes: 22 * 1024 * 1024 * 1024, Context: 32768,
		Cache: profileCacheOutput{State: "present"},
	}}}
	text, err := humanOutput(output)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"MODEL", "qwen35b-mtp", "22.0 GiB", "present", "primary local agent"} {
		if !strings.Contains(text, want) {
			t.Fatalf("human output missing %q:\n%s", want, text)
		}
	}
}

func TestHumanStatusShowsHealthAndMemory(t *testing.T) {
	health := true
	text, err := humanOutput(runnerprocess.Status{
		Kind: runnerprocess.StatusRunning, Preset: "qwen35b-mtp", PID: 42,
		Endpoint: "http://127.0.0.1:11435", Health: &health,
		ResidentBytes: 24 * 1024 * 1024 * 1024, StartedAt: "2026-09-02T12:00:00Z",
		LogFile: "/tmp/server.log", Detail: "healthy",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"qwen35b-mtp", "healthy", "24.0 GiB", "127.0.0.1:11435", "/tmp/server.log"} {
		if !strings.Contains(text, want) {
			t.Fatalf("human output missing %q:\n%s", want, text)
		}
	}
}

func TestHumanServiceStatusShowsGatewayAndModel(t *testing.T) {
	health := true
	text, err := humanOutput(serviceStatusOutput{
		Gateway: runnerprocess.Status{
			Kind: runnerprocess.StatusRunning, Health: &health, Endpoint: "http://127.0.0.1:11435",
		},
		Model: runnerprocess.Status{
			Kind: runnerprocess.StatusRunning, Preset: "gemma4-26b", Health: &health,
			ResidentBytes: 16 * 1024 * 1024 * 1024, LogFile: "/tmp/model.log",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Gateway: running (healthy)", "127.0.0.1:11435/v1", "gemma4-26b", "16.0 GiB"} {
		if !strings.Contains(text, want) {
			t.Fatalf("human output missing %q:\n%s", want, text)
		}
	}
}

func TestOutputArgumentsRemovesJSONFlag(t *testing.T) {
	arguments, jsonOutput := outputArguments([]string{"status", "--json"})
	if !jsonOutput || len(arguments) != 1 || arguments[0] != "status" {
		t.Fatalf("arguments = %v, json = %t", arguments, jsonOutput)
	}
}

func TestLogArguments(t *testing.T) {
	lines, err := parseLogArguments([]string{"--lines", "80"})
	if err != nil || lines != 80 {
		t.Fatalf("lines = %d, error = %v", lines, err)
	}
	for _, arguments := range [][]string{{"--lines", "0"}, {"--lines", "10001"}, {"extra"}} {
		if _, err := parseLogArguments(arguments); err == nil {
			t.Fatalf("arguments %v succeeded", arguments)
		}
	}
}
