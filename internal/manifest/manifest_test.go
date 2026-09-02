package manifest

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestProfiles(t *testing.T) {
	profiles, err := All()
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, len(profiles))
	for _, profile := range profiles {
		ids = append(ids, profile.ID)
	}
	if !reflect.DeepEqual(ids, []string{
		"tiny", "qwen3-1.7b", "minicpm5-1b", "granite4.2-3b", "ling3-tiny", "qwen35-4b-helper", "granite4-h-tiny", "qwen35b-mtp",
	}) {
		t.Fatalf("profile ids = %v", ids)
	}
	tiny, err := Get("tiny")
	if err != nil {
		t.Fatal(err)
	}
	if tiny.Model.Repo != "ggml-org/Qwen3.5-0.8B-GGUF" || tiny.Model.File != "Qwen3.5-0.8B-Q4_0.gguf" {
		t.Fatalf("unexpected tiny model: %#v", tiny.Model)
	}
	if tiny.Persistence.Enabled {
		t.Fatal("hybrid tiny profile enabled unsupported persistent KV")
	}
	qwen, err := Get("qwen3-1.7b")
	if err != nil {
		t.Fatal(err)
	}
	if !qwen.Runnable || qwen.Context.Size != 32768 || qwen.Model.SHA256 != "d2387ca2dbfee2ffabce7120d3770dadca0b293052bc2f0e138fdc940d9bc7b5" {
		t.Fatalf("unexpected qwen profile: %#v", qwen)
	}
	if !qwen.Persistence.Enabled {
		t.Fatal("pure-attention qwen profile disabled proven persistent KV")
	}
	large, err := Get("qwen35b-mtp")
	if err != nil {
		t.Fatal(err)
	}
	if !large.Runnable || large.Context.Size != 32768 {
		t.Fatalf("large context = %d", large.Context.Size)
	}
	if large.Persistence.Enabled {
		t.Fatal("hybrid MTP profile enabled unsupported persistent KV")
	}
	if _, err := Get("missing"); err == nil {
		t.Fatal("missing profile did not fail")
	}
}

func TestProfilesRoundTripJSON(t *testing.T) {
	profiles, err := All()
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(profiles)
	if err != nil {
		t.Fatal(err)
	}
	var decoded []Profile
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded) != len(profiles) {
		t.Fatalf("decoded %d profiles, want %d", len(decoded), len(profiles))
	}
	for index := range profiles {
		if decoded[index].GPULayers != profiles[index].GPULayers {
			t.Fatalf("profile %s gpu layers = %#v", profiles[index].ID, decoded[index].GPULayers)
		}
	}
}

func TestTinyArguments(t *testing.T) {
	profile, _ := Get("tiny")
	slots := t.TempDir()
	args, err := BuildServerArgs(profile, BuildOptions{Port: 23456, SlotSavePath: slots})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"--host", "127.0.0.1", "--port", "23456",
		"--alias", "tiny",
		"--cors-origins", "https://outrider.invalid", "--no-cors-credentials",
		"--hf-repo", "ggml-org/Qwen3.5-0.8B-GGUF:Q4_0", "--hf-file", "Qwen3.5-0.8B-Q4_0.gguf",
		"--ctx-size", "4096",
	}
	if !reflect.DeepEqual(args[:len(want)], want) {
		t.Fatalf("argument prefix = %v", args[:len(want)])
	}
	if !containsPair(args, "--spec-type", "none") {
		t.Fatalf("missing disabled speculation: %v", args)
	}
	for flag, value := range map[string]string{
		"--n-gpu-layers":        "all",
		"--flash-attn":          "on",
		"--cache-ram":           "512",
		"--ctx-checkpoints":     "0",
		"--checkpoint-min-step": "8192",
	} {
		if !containsPair(args, flag, value) {
			t.Fatalf("missing %s %s: %v", flag, value, args)
		}
	}
}

func TestMTPPlan(t *testing.T) {
	profile, _ := Get("qwen35b-mtp")
	root := t.TempDir()
	port := 22001
	plan, err := Resolve(profile, ResolveOptions{Root: root, Port: &port})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Endpoint != "http://127.0.0.1:22001" {
		t.Fatalf("endpoint = %q", plan.Endpoint)
	}
	if plan.HealthEndpoint != "http://127.0.0.1:22001/health" {
		t.Fatalf("health endpoint = %q", plan.HealthEndpoint)
	}
	wantExecutable := filepath.Join(root, "llama.cpp", LlamaRelease.Tag, LlamaRelease.Directory, "llama-server")
	if plan.Executable != wantExecutable {
		t.Fatalf("executable = %q, want %q", plan.Executable, wantExecutable)
	}
	if !containsPair(plan.Args, "--spec-type", "draft-mtp") || !containsPair(plan.Args, "--spec-draft-n-max", "2") {
		t.Fatalf("missing MTP arguments: %v", plan.Args)
	}
	for flag, value := range map[string]string{
		"--cache-ram":        "0",
		"--spec-draft-n-min": "0",
		"--spec-draft-p-min": "0",
	} {
		if !containsPair(plan.Args, flag, value) {
			t.Fatalf("missing %s %s: %v", flag, value, plan.Args)
		}
	}
	for _, arg := range plan.Args {
		if arg == "--draft" {
			t.Fatalf("legacy draft flag present: %v", plan.Args)
		}
	}
}

func TestQwenArguments(t *testing.T) {
	profile, _ := Get("qwen3-1.7b")
	slots := t.TempDir()
	args, err := BuildServerArgs(profile, BuildOptions{Port: DefaultPort, SlotSavePath: slots})
	if err != nil {
		t.Fatal(err)
	}
	for flag, value := range map[string]string{
		"--alias":            "qwen3-1.7b",
		"--ctx-size":         "32768",
		"--presence-penalty": "1.5",
	} {
		if !containsPair(args, flag, value) {
			t.Fatalf("missing %s %s: %v", flag, value, args)
		}
	}
	if !containsPair(args, "--slot-save-path", slots) || !containsValue(args, "--slots") {
		t.Fatalf("missing persistence arguments: %v", args)
	}
}

func TestLocalModelAndExtraArguments(t *testing.T) {
	profile, _ := Get("tiny")
	profile.Model = Artifact{LocalPath: "models/local.gguf", SizeBytes: 1}
	profile.ExtraArgs = []string{"--verbose", "--log-colors"}
	cwd := t.TempDir()
	args, err := BuildServerArgs(profile, BuildOptions{
		Port: DefaultPort, CWD: cwd, SlotSavePath: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !containsPair(args, "--model", filepath.Join(cwd, "models/local.gguf")) {
		t.Fatalf("local model was not resolved: %v", args)
	}
	if !reflect.DeepEqual(args[len(args)-2:], profile.ExtraArgs) {
		t.Fatalf("extra arguments changed: %v", args[len(args)-2:])
	}
}

func TestRejectsUnsafeExtraArguments(t *testing.T) {
	profile, _ := Get("tiny")
	for _, arg := range []string{
		"--port=9999", "--alias=other", "--model", "--cors-origins=*", "--cors-credentials", "bad\narg",
	} {
		profile.ExtraArgs = []string{arg}
		if err := Validate(profile); err == nil {
			t.Fatalf("accepted extra argument %q", arg)
		}
	}
}

func TestRejectsIncompleteDraftArtifact(t *testing.T) {
	profile, _ := Get("tiny")
	profile.Speculation = Speculation{Mode: "dflash", Tokens: 2, Draft: &Artifact{Repo: "example/model"}}
	if err := Validate(profile); err == nil {
		t.Fatal("accepted incomplete draft artifact")
	}
}

func TestRejectsInvalidPorts(t *testing.T) {
	profile, _ := Get("tiny")
	for _, port := range []int{0, 65536} {
		if _, err := BuildServerArgs(profile, BuildOptions{Port: port, SlotSavePath: t.TempDir()}); err == nil {
			t.Fatalf("accepted port %d", port)
		}
	}
}

func TestReturnsManifestError(t *testing.T) {
	profile, _ := Get("tiny")
	profile.Context.Size = 0
	err := Validate(profile)
	var target *ManifestError
	if !errors.As(err, &target) {
		t.Fatalf("error type = %T", err)
	}
	if !strings.Contains(target.Error(), "context") {
		t.Fatalf("error = %q", target.Error())
	}
}

func TestTinyModelHasContentHash(t *testing.T) {
	profile, err := Get("tiny")
	if err != nil {
		t.Fatal(err)
	}
	if profile.Model.SHA256 != "57d1997790d1744fba5b40a7317df71ea5e2acee28c47e78f0cce39c0703f8cf" {
		t.Fatalf("tiny model hash = %q", profile.Model.SHA256)
	}
}

func TestCachedPlanPreservesModelIdentity(t *testing.T) {
	profile, _ := Get("tiny")
	plan, err := ResolveCached(profile, ResolveOptions{Root: t.TempDir(), Executable: "/fake/llama-server"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Profile.Model.Repo != profile.Model.Repo || plan.Profile.Model.File != profile.Model.File || plan.Profile.Model.Quant != profile.Model.Quant {
		t.Fatalf("cached model identity = %#v", plan.Profile.Model)
	}
	if plan.Profile.Model.LocalPath != plan.State.Model {
		t.Fatalf("cached local path = %q, model path = %q", plan.Profile.Model.LocalPath, plan.State.Model)
	}
}

func TestPersistenceRequiresOneSlot(t *testing.T) {
	profile, _ := Get("tiny")
	profile.Persistence.Enabled = true
	profile.Batch.Parallel = 2
	if err := Validate(profile); err == nil || !strings.Contains(err.Error(), "one server slot") {
		t.Fatalf("validation error = %v", err)
	}
}

func TestSessionKeyTracksKVCompatibility(t *testing.T) {
	profile, _ := Get("qwen3-1.7b")
	first, err := SessionKey(profile)
	if err != nil {
		t.Fatal(err)
	}
	profile.KVCache.KeyType = "q4_0"
	second, err := SessionKey(profile)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("session key did not change with KV layout")
	}
}

func containsPair(args []string, flag string, value string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag && args[i+1] == value {
			return true
		}
	}
	return false
}

func containsValue(args []string, value string) bool {
	for _, candidate := range args {
		if candidate == value {
			return true
		}
	}
	return false
}
