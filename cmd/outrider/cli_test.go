package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/corvines/outrider/internal/manifest"
	"github.com/corvines/outrider/internal/ollamacache"
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

func TestProcessCommandsDoNotPrepareRuntime(t *testing.T) {
	environment := map[string]string{
		"OUTRIDER_HOME": t.TempDir(), "LLAMA_SERVER_BIN": "/missing/llama-server",
	}
	output, err := run(context.Background(), []string{"ps"}, environment)
	if err != nil {
		t.Fatal(err)
	}
	var processStatus map[string]any
	if err := json.Unmarshal([]byte(output), &processStatus); err != nil {
		t.Fatal(err)
	}
	if processStatus["kind"] != "stopped" {
		t.Fatalf("ps status = %#v", processStatus)
	}
	for _, argv := range [][]string{{"status"}, {"stop"}, {"down"}} {
		output, err := run(context.Background(), argv, environment)
		if err != nil {
			t.Fatalf("%v: %v", argv, err)
		}
		var status serviceStatusOutput
		if err := json.Unmarshal([]byte(output), &status); err != nil {
			t.Fatal(err)
		}
		if status.Gateway.Kind != "stopped" || status.Model.Kind != "stopped" {
			t.Fatalf("%v status = %#v", argv, status)
		}
	}
}

func TestLifecycleCommandAliases(t *testing.T) {
	tests := map[string]string{
		"models": "ls",
		"serve":  "serve",
		"up":     "serve",
		"ps":     "ps",
		"status": "status",
		"stop":   "stop",
		"down":   "stop",
	}
	for command, want := range tests {
		if got := canonicalCommand(command); got != want {
			t.Fatalf("canonicalCommand(%q) = %q, want %q", command, got, want)
		}
	}
}

func TestChatCommand(t *testing.T) {
	calledWith := ""
	output, err := runWithOptions(
		context.Background(),
		[]string{"chat", "--endpoint", "http://127.0.0.1:11436"},
		map[string]string{},
		runOptions{Chat: func(endpoint string) error {
			calledWith = endpoint
			return nil
		}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if output != "" || calledWith != "http://127.0.0.1:11436" {
		t.Fatalf("output = %q, endpoint = %q", output, calledWith)
	}
}

func TestUserInstallCommands(t *testing.T) {
	home := t.TempDir()
	runnerHome := t.TempDir()
	source := filepath.Join(t.TempDir(), "outrider")
	if err := os.WriteFile(source, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	environment := map[string]string{"HOME": home, "OUTRIDER_HOME": runnerHome}
	output, err := runWithOptions(context.Background(), []string{"install"}, environment, runOptions{
		CurrentExecutable: func() (string, error) { return source, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	var installed installOutput
	if err := json.Unmarshal([]byte(output), &installed); err != nil {
		t.Fatal(err)
	}
	if installed.Status != "installed" || !strings.HasSuffix(installed.Target, "/.local/bin/outrider") {
		t.Fatalf("install output = %#v", installed)
	}
	output, err = run(context.Background(), []string{"uninstall"}, environment)
	if err != nil {
		t.Fatal(err)
	}
	var removed installOutput
	if err := json.Unmarshal([]byte(output), &removed); err != nil {
		t.Fatal(err)
	}
	if removed.Status != "uninstalled" {
		t.Fatalf("uninstall output = %#v", removed)
	}
	if _, err := os.Stat(installed.Target); !os.IsNotExist(err) {
		t.Fatalf("installed binary remains: %v", err)
	}
}

func TestListAndShowProfiles(t *testing.T) {
	root := t.TempDir()
	ollamaRoot := t.TempDir()
	writeOllamaFixture(t, ollamaRoot)
	listJSON, err := run(context.Background(), []string{"ls"}, map[string]string{
		"OUTRIDER_HOME": root, "OLLAMA_MODELS": ollamaRoot,
	})
	if err != nil {
		t.Fatal(err)
	}
	var list profileListOutput
	if err := json.Unmarshal([]byte(listJSON), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Profiles) != 9 {
		t.Fatalf("profiles = %#v", list.Profiles)
	}
	if list.Profiles[0].Cache.State != "missing" {
		t.Fatalf("first cache = %#v", list.Profiles[0].Cache)
	}
	if len(list.DevelopmentModels) != 1 || list.DevelopmentModels[0].Name != "granite4.2:8b" {
		t.Fatalf("development models = %#v", list.DevelopmentModels)
	}

	showJSON, err := run(context.Background(), []string{"show", "qwen35b-mtp"}, map[string]string{"OUTRIDER_HOME": root})
	if err != nil {
		t.Fatal(err)
	}
	var detail profileDetailOutput
	if err := json.Unmarshal([]byte(showJSON), &detail); err != nil {
		t.Fatal(err)
	}
	if detail.Profile.ID != "qwen35b-mtp" || !detail.Profile.Runnable || detail.Profile.Speculation.Mode != "mtp" {
		t.Fatalf("profile detail = %#v", detail)
	}
}

func writeOllamaFixture(t *testing.T, root string) {
	t.Helper()
	digest := strings.Repeat("a", 64)
	blobPath := filepath.Join(root, "blobs", "sha256-"+digest)
	manifestPath := filepath.Join(
		root, "manifests", "registry.ollama.ai", "library", "granite4.2", "8b",
	)
	if err := os.MkdirAll(filepath.Dir(blobPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0o700); err != nil {
		t.Fatal(err)
	}
	model := []byte("GGUFmodel")
	if err := os.WriteFile(blobPath, model, 0o600); err != nil {
		t.Fatal(err)
	}
	payload := fmt.Sprintf(
		`{"layers":[{"mediaType":"application/vnd.ollama.image.model","digest":"sha256:%s","size":%d}]}`,
		digest, len(model),
	)
	if err := os.WriteFile(manifestPath, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestProfileCacheInspection(t *testing.T) {
	profile, err := manifest.Get("tiny")
	if err != nil {
		t.Fatal(err)
	}
	profile.Model.SizeBytes = 4
	path := filepath.Join(t.TempDir(), "tiny.gguf")
	if err := os.WriteFile(path, make([]byte, profile.Model.SizeBytes), 0o600); err != nil {
		t.Fatal(err)
	}
	cache, err := inspectProfileCache(profile, path)
	if err != nil {
		t.Fatal(err)
	}
	if cache.State != "present" || cache.SizeBytes != profile.Model.SizeBytes {
		t.Fatalf("cache = %#v", cache)
	}
}

func TestDevelopmentProfileUsesCachedBlobWithConservativeContext(t *testing.T) {
	temperature := 0.6
	topP := 0.95
	topK := 20
	repeatPenalty := 1.0
	model := ollamacache.Model{
		Name: "granite4.2:8b", Digest: "sha256:" + strings.Repeat("a", 64),
		Path: "/cache/blobs/sha256-model", SizeBytes: 1024,
		Parameters: &ollamacache.Parameters{
			Temperature: &temperature, TopP: &topP, TopK: &topK, RepeatPenalty: &repeatPenalty,
		},
	}
	profile, err := developmentProfile(model)
	if err != nil {
		t.Fatal(err)
	}
	if profile.ID != model.Name || profile.Model.LocalPath != model.Path || profile.Context.Size != 4096 {
		t.Fatalf("profile = %#v", profile)
	}
	if !containsSequence(profile.ExtraArgs, "--no-webui") {
		t.Fatalf("extra args = %v", profile.ExtraArgs)
	}
	if profile.Sampling.Temperature != temperature || profile.Sampling.TopP != topP ||
		profile.Sampling.TopK != topK || profile.Sampling.RepeatPenalty != repeatPenalty {
		t.Fatalf("sampling = %#v", profile.Sampling)
	}
}

func TestUsageErrors(t *testing.T) {
	for _, argv := range [][]string{
		nil,
		{"plan"},
		{"check"},
		{"verify"},
		{"ls", "extra"},
		{"show"},
		{"pull"},
		{"install", "extra"},
		{"uninstall", "extra"},
		{"version", "extra"},
		{"start", "qwen35b-mtp"},
		{"use"},
		{"status", "extra"},
		{"run"},
		{"chat", "unexpected"},
		{"chat", "--missing"},
		{"serve", "tiny", "extra"},
		{"smoke", "tiny"},
		{"demo"},
		{"ps", "tiny"},
		{"logs", "extra"},
		{"stop", "qwen3-1.7b"},
		{"ps", "tiny", "extra"},
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
