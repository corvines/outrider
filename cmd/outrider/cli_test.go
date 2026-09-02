package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/corvines/outrider/internal/manifest"
)

func TestPlanPreservesWireShape(t *testing.T) {
	environment := map[string]string{
		"OUTRIDER_HOME": t.TempDir(), "LLAMA_SERVER_BIN": "/opt/llama-server", "OUTRIDER_PORT": "12345",
	}
	output, err := run(context.Background(), []string{"plan", "tiny"}, environment)
	if err != nil {
		t.Fatal(err)
	}
	var plan planOutput
	if err := json.Unmarshal([]byte(output), &plan); err != nil {
		t.Fatal(err)
	}
	if plan.Preset != "tiny" || plan.Port != 12345 || plan.Executable != "/opt/llama-server" {
		t.Fatalf("plan = %#v", plan)
	}
	if plan.Model.Repository != "ggml-org/Qwen3.5-0.8B-GGUF" || plan.Model.LocalPath != "" {
		t.Fatalf("model = %#v", plan.Model)
	}
	if plan.State.ModelCache == "" || plan.State.PIDFile == "" || plan.HealthEndpoint != "http://127.0.0.1:12345/health" {
		t.Fatalf("state = %#v, health = %q", plan.State, plan.HealthEndpoint)
	}
	if !containsSequence(plan.Argv, "--cors-origins", manifest.DeniedBrowserOrigin, "--no-cors-credentials") {
		t.Fatalf("CORS policy missing from argv: %v", plan.Argv)
	}
}

func TestPlanIncludesMTPArguments(t *testing.T) {
	output, err := run(context.Background(), []string{"plan", "qwen35b-mtp"}, map[string]string{
		"OUTRIDER_HOME": t.TempDir(), "LLAMA_SERVER_BIN": "/opt/llama-server",
	})
	if err != nil {
		t.Fatal(err)
	}
	var plan planOutput
	if err := json.Unmarshal([]byte(output), &plan); err != nil {
		t.Fatal(err)
	}
	if !containsSequence(plan.Argv, "--spec-type", "draft-mtp", "--spec-draft-n-max", "2") {
		t.Fatalf("MTP arguments missing: %v", plan.Argv)
	}
}

func TestStatusAndDownDoNotPrepareRuntime(t *testing.T) {
	environment := map[string]string{
		"OUTRIDER_HOME": t.TempDir(), "LLAMA_SERVER_BIN": "/missing/llama-server",
	}
	for _, argv := range [][]string{{"status"}, {"down"}, {"status", "qwen3-1.7b"}, {"down", "qwen3-1.7b"}} {
		output, err := run(context.Background(), argv, environment)
		if err != nil {
			t.Fatalf("%v: %v", argv, err)
		}
		var status map[string]any
		if err := json.Unmarshal([]byte(output), &status); err != nil {
			t.Fatal(err)
		}
		if status["kind"] != "stopped" {
			t.Fatalf("%v status = %#v", argv, status)
		}
	}
}

func TestUsageErrors(t *testing.T) {
	for _, argv := range [][]string{
		nil,
		{"plan"},
		{"up", "qwen35b-mtp"},
		{"smoke", "tiny"},
		{"demo"},
		{"status", "tiny", "extra"},
		{"missing"},
	} {
		_, err := run(context.Background(), argv, map[string]string{})
		if err == nil || !strings.Contains(err.Error(), usage) {
			t.Fatalf("argv %v error = %v", argv, err)
		}
	}
}

func TestRejectsInvalidEnvironmentPort(t *testing.T) {
	for _, value := range []string{"", "0", "65536", "12.5", "-1", "port"} {
		_, err := run(context.Background(), []string{"plan", "tiny"}, map[string]string{"OUTRIDER_PORT": value})
		if err == nil || !strings.Contains(err.Error(), "OUTRIDER_PORT") {
			t.Fatalf("port %q error = %v", value, err)
		}
	}
}

func TestEnvironmentMapKeepsEqualsInValues(t *testing.T) {
	got := environmentMap([]string{"A=one=two", "B=three", "invalid"})
	if got["A"] != "one=two" || got["B"] != "three" {
		t.Fatalf("environment = %#v", got)
	}
}

func containsSequence(values []string, sequence ...string) bool {
	for index := 0; index+len(sequence) <= len(values); index++ {
		matches := true
		for offset := range sequence {
			if values[index+offset] != sequence[offset] {
				matches = false
				break
			}
		}
		if matches {
			return true
		}
	}
	return false
}
