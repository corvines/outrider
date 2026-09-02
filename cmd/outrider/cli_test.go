package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
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

func TestCheckReturnsStructuredAdmissionWithoutPreparingRuntime(t *testing.T) {
	root := t.TempDir()
	output, err := run(context.Background(), []string{"check", "tiny"}, map[string]string{
		"OUTRIDER_HOME": root, "LLAMA_SERVER_BIN": "/missing/llama-server",
	})
	if err != nil {
		t.Fatal(err)
	}
	var report struct {
		Profile string `json:"profile"`
		Class   string `json:"class"`
	}
	if err := json.Unmarshal([]byte(output), &report); err != nil {
		t.Fatal(err)
	}
	if report.Profile != "tiny" || report.Class == "" {
		t.Fatalf("report = %#v", report)
	}
}

func TestBlockedAdmissionDoesNotPrepareRuntimeOrModel(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	root := t.TempDir()
	port := listener.Addr().(*net.TCPAddr).Port
	_, err = run(context.Background(), []string{"up", "tiny"}, map[string]string{
		"OUTRIDER_HOME": root, "LLAMA_SERVER_BIN": "/missing/llama-server",
		"OUTRIDER_PORT": fmt.Sprintf("%d", port),
	})
	if err == nil || !strings.Contains(err.Error(), "admission port") {
		t.Fatalf("error = %v", err)
	}
	for _, path := range []string{"models", "downloads", "llama.cpp"} {
		if _, statErr := os.Stat(root + "/" + path); !os.IsNotExist(statErr) {
			t.Fatalf("%s exists after blocked admission: %v", path, statErr)
		}
	}
}

func TestStatusAndDownDoNotPrepareRuntime(t *testing.T) {
	environment := map[string]string{
		"OUTRIDER_HOME": t.TempDir(), "LLAMA_SERVER_BIN": "/missing/llama-server",
	}
	for _, argv := range [][]string{{"status"}, {"down"}} {
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
		{"check"},
		{"verify"},
		{"up", "qwen35b-mtp"},
		{"smoke", "tiny"},
		{"demo"},
		{"status", "tiny"},
		{"down", "qwen3-1.7b"},
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
