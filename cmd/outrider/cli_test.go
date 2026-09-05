package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/corvines/outrider/internal/chat"
	"github.com/corvines/outrider/internal/manifest"
	runnerprocess "github.com/corvines/outrider/internal/process"
)

func TestPlanPreservesWireShape(t *testing.T) {
	environment := map[string]string{
		"OUTRIDER_HOME": t.TempDir(), "LLAMA_SERVER_BIN": "/opt/llama-server", "OUTRIDER_PORT": "12345",
	}
	output, err := run(context.Background(), []string{"plan", "qwen35-0.8b"}, environment)
	if err != nil {
		t.Fatal(err)
	}
	var plan planOutput
	if err := json.Unmarshal([]byte(output), &plan); err != nil {
		t.Fatal(err)
	}
	if plan.Preset != "qwen35-0.8b" || plan.Port != 12345 || plan.Executable != "/opt/llama-server" {
		t.Fatalf("plan = %#v", plan)
	}
	if plan.Model.Repository != "unsloth/Qwen3.5-0.8B-MTP-GGUF" || plan.Model.LocalPath != "" {
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
	output, err := run(context.Background(), []string{"check", "qwen35-0.8b"}, map[string]string{
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
	if report.Profile != "qwen35-0.8b" || report.Class == "" {
		t.Fatalf("report = %#v", report)
	}
}

func TestBlockedAdmissionDoesNotPrepareRuntimeOrModel(t *testing.T) {
	t.Setenv("OUTRIDER_DEV", "1")
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	root := t.TempDir()
	port := listener.Addr().(*net.TCPAddr).Port
	_, err = run(context.Background(), []string{"up", "qwen35-0.8b"}, map[string]string{
		"OUTRIDER_HOME": root, "LLAMA_SERVER_BIN": "/missing/llama-server",
		"OUTRIDER_PORT": fmt.Sprintf("%d", port),
	})
	if err == nil || !strings.Contains(err.Error(), "already in use") {
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
	var calledWith chat.RunOptions
	output, err := runWithOptions(
		context.Background(),
		[]string{"chat", "--endpoint", "http://127.0.0.1:11436"},
		map[string]string{},
		runOptions{Chat: func(options chat.RunOptions) error {
			calledWith = options
			return nil
		}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if output != "" || calledWith.Endpoint != "http://127.0.0.1:11436" {
		t.Fatalf("output = %q, endpoint = %q", output, calledWith.Endpoint)
	}
	if calledWith.Debug {
		t.Fatal("chat listed unmanaged models without --debug")
	}
}

// --debug is what opens discovery to models outrider does not manage.
func TestChatCommandDebugFlag(t *testing.T) {
	var calledWith chat.RunOptions
	_, err := runWithOptions(
		context.Background(),
		[]string{"chat", "--debug"},
		map[string]string{},
		runOptions{Chat: func(options chat.RunOptions) error {
			calledWith = options
			return nil
		}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !calledWith.Debug {
		t.Fatal("--debug did not reach the chat session")
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

func TestUninstallResolvesTheStateRoot(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		arguments []string
		confirm   func(string) (bool, error)
		removed   bool
		prompted  bool
	}{
		{name: "purge", arguments: []string{"uninstall", "--purge"}, removed: true},
		{name: "keep", arguments: []string{"uninstall", "--keep-state"}},
		{
			name:      "accepted prompt",
			arguments: []string{"uninstall"},
			confirm:   func(string) (bool, error) { return true, nil },
			removed:   true,
			prompted:  true,
		},
		{
			name:      "declined prompt",
			arguments: []string{"uninstall"},
			confirm:   func(string) (bool, error) { return false, nil },
			prompted:  true,
		},
		{name: "no terminal to ask", arguments: []string{"uninstall"}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			home := t.TempDir()
			runnerHome := filepath.Join(t.TempDir(), "Outrider")
			model := filepath.Join(runnerHome, "models", "tiny.gguf")
			if err := os.MkdirAll(filepath.Dir(model), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(model, []byte("0123456789"), 0o644); err != nil {
				t.Fatal(err)
			}
			environment := map[string]string{"HOME": home, "OUTRIDER_HOME": runnerHome}
			installForTest(t, environment)
			output, err := runWithOptions(
				context.Background(), testCase.arguments, environment,
				runOptions{Confirm: testCase.confirm},
			)
			if err != nil {
				t.Fatal(err)
			}
			var removed installOutput
			if err := json.Unmarshal([]byte(output), &removed); err != nil {
				t.Fatal(err)
			}
			if removed.StateRoot != runnerHome || removed.StateBytes != 10 {
				t.Fatalf("state report = %#v", removed)
			}
			if removed.StateRemoved != testCase.removed || removed.StatePrompted != testCase.prompted {
				t.Fatalf("state decision = %#v", removed)
			}
			_, err = os.Stat(runnerHome)
			if testCase.removed && !os.IsNotExist(err) {
				t.Fatalf("state root remains: %v", err)
			}
			if !testCase.removed && err != nil {
				t.Fatalf("state root removed: %v", err)
			}
		})
	}
}

func TestUninstallInJSONModeNeverPrompts(t *testing.T) {
	runnerHome := filepath.Join(t.TempDir(), "Outrider")
	if err := os.MkdirAll(filepath.Join(runnerHome, "models"), 0o755); err != nil {
		t.Fatal(err)
	}
	environment := map[string]string{"HOME": t.TempDir(), "OUTRIDER_HOME": runnerHome}
	installForTest(t, environment)
	output, err := runWithOptions(
		context.Background(), []string{"uninstall"}, environment,
		runOptions{Confirm: nil},
	)
	if err != nil {
		t.Fatal(err)
	}
	var removed installOutput
	if err := json.Unmarshal([]byte(output), &removed); err != nil {
		t.Fatal(err)
	}
	if removed.StatePrompted || removed.StateRemoved {
		t.Fatalf("machine-readable uninstall asked or removed: %#v", removed)
	}
	if _, err := os.Stat(runnerHome); err != nil {
		t.Fatalf("state root removed without an answer: %v", err)
	}
}

func TestUninstallReportsAPromptFailure(t *testing.T) {
	runnerHome := filepath.Join(t.TempDir(), "Outrider")
	if err := os.MkdirAll(filepath.Join(runnerHome, "models"), 0o755); err != nil {
		t.Fatal(err)
	}
	environment := map[string]string{"HOME": t.TempDir(), "OUTRIDER_HOME": runnerHome}
	installForTest(t, environment)
	_, err := runWithOptions(
		context.Background(), []string{"uninstall"}, environment,
		runOptions{Confirm: func(string) (bool, error) { return false, fmt.Errorf("no answer") }},
	)
	if err == nil || !strings.Contains(err.Error(), "no answer") {
		t.Fatalf("uninstall error = %v", err)
	}
	if _, err := os.Stat(runnerHome); err != nil {
		t.Fatalf("state root removed after a failed prompt: %v", err)
	}
}

func TestListAndShowProfiles(t *testing.T) {
	t.Setenv("OUTRIDER_DEV", "1")
	root := t.TempDir()
	listJSON, err := run(context.Background(), []string{"ls"}, map[string]string{
		"OUTRIDER_HOME": root,
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

func TestListWithoutDevelopmentEnabled(t *testing.T) {
	root := t.TempDir()
	listJSON, err := run(context.Background(), []string{"ls"}, map[string]string{
		"OUTRIDER_HOME": root,
	})
	if err != nil {
		t.Fatal(err)
	}
	var list profileListOutput
	if err := json.Unmarshal([]byte(listJSON), &list); err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, len(list.Profiles))
	for _, profile := range list.Profiles {
		ids = append(ids, profile.ID)
	}
	if !reflect.DeepEqual(ids, []string{"qwen35b-mtp", "qwen35-0.8b", "qwen35-2b"}) {
		t.Fatalf("offered profiles = %v", ids)
	}
}

func TestProfileCacheInspection(t *testing.T) {
	profile, err := manifest.Get("qwen35-0.8b")
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

func TestStopArguments(t *testing.T) {
	skip, err := parseStopArguments([]string{"--skip-checkpoint"})
	if err != nil || !skip {
		t.Fatalf("skip checkpoint = %v, %v", skip, err)
	}
	if _, err := parseStopArguments([]string{"qwen35-0.8b"}); err == nil {
		t.Fatal("stop accepted a profile")
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
		{"uninstall", "--purge", "--keep-state"},
		{"version", "extra"},
		{"start", "qwen35b-mtp"},
		{"use"},
		{"status", "extra"},
		{"run"},
		{"chat", "unexpected"},
		{"chat", "--missing"},
		{"serve", "qwen35-0.8b", "extra"},
		{"smoke", "qwen35-0.8b"},
		{"demo"},
		{"ps", "qwen35-0.8b"},
		{"logs", "extra"},
		{"stop", "qwen3-1.7b"},
		{"ps", "qwen35-0.8b", "extra"},
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
		_, err := run(context.Background(), []string{"plan", "qwen35-0.8b"}, map[string]string{"OUTRIDER_PORT": value})
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

func TestServeUnknownProfileFailsBeforeGateway(t *testing.T) {
	root := t.TempDir()
	_, err := run(context.Background(), []string{"serve", "not-a-profile"}, map[string]string{
		"OUTRIDER_HOME": root,
	})
	if err == nil {
		t.Fatal("expected unknown profile to fail")
	}
	if _, statErr := os.Stat(filepath.Join(root, "runs", "gateway.json")); !os.IsNotExist(statErr) {
		t.Fatalf("gateway record = %v", statErr)
	}
}

func TestRunOptionsNotice(t *testing.T) {
	var messages []string
	options := runOptions{Notice: func(message string) { messages = append(messages, message) }}
	options.notice("Loading %s...", "qwen3-1.7b")
	if len(messages) != 1 || messages[0] != "Loading qwen3-1.7b..." {
		t.Fatalf("messages = %#v", messages)
	}
	runOptions{}.notice("ignored")
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

func installForTest(t *testing.T, environment map[string]string) {
	t.Helper()
	source := filepath.Join(t.TempDir(), "outrider")
	if err := os.WriteFile(source, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := runWithOptions(context.Background(), []string{"install"}, environment, runOptions{
		CurrentExecutable: func() (string, error) { return source, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
}

// Holding a profile out of the catalog is worth little if it can still be
// served by name, so the switch gates use as well as listing.
func TestServingADevelopmentProfileIsRefusedWithoutTheSwitch(t *testing.T) {
	t.Setenv("OUTRIDER_DEV", "")
	_, err := runnableProfile("qwen3-1.7b")
	if err == nil {
		t.Fatal("a development profile was accepted without the switch")
	}
	if !strings.Contains(err.Error(), "OUTRIDER_DEV") {
		t.Errorf("error does not name the switch that unlocks it: %v", err)
	}
	t.Setenv("OUTRIDER_DEV", "1")
	if _, err := runnableProfile("qwen3-1.7b"); err != nil {
		t.Errorf("development profile refused with the switch on: %v", err)
	}
}

// The gateway listens on the front port, which is the port a profile plan
// names, so a running gateway means the port is not free but is not a conflict
// either.
func TestPortOwnershipCountsTheRunningGateway(t *testing.T) {
	running := runnerprocess.Status{Kind: runnerprocess.StatusRunning}
	stopped := runnerprocess.Status{Kind: runnerprocess.StatusStopped}
	cases := []struct {
		name        string
		model       runnerprocess.Status
		gatewayPort int
		gateway     runnerprocess.Status
		want        bool
	}{
		{"gateway holds the same port", stopped, 11435, running, true},
		{"gateway stopped", stopped, 11435, stopped, false},
		{"gateway on another port", stopped, 21435, running, false},
		{"model runner holds it", running, 21435, stopped, true},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := ownsPort(11435, testCase.model, testCase.gatewayPort, testCase.gateway)
			if got != testCase.want {
				t.Fatalf("ownsPort = %t, want %t", got, testCase.want)
			}
		})
	}
}
