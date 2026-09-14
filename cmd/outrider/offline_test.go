package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/corvines/outrider/internal/admission"
)

func TestCheckArguments(t *testing.T) {
	for _, args := range [][]string{{"ling3-tiny"}, {"--offline", "ling3-tiny"}, {"ling3-tiny", "--offline"}} {
		id, offline, err := parseCheckArguments(args)
		if err != nil || id != "ling3-tiny" || offline != (len(args) == 2) {
			t.Fatalf("%v: %s, %v, %v", args, id, offline, err)
		}
	}
	for _, args := range [][]string{nil, {"--offline"}, {"--oops", "ling3-tiny"}, {"ling3-tiny", "extra"}, {"--offline", "--offline", "ling3-tiny"}} {
		if _, _, err := parseCheckArguments(args); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
}

func TestOfflineCheckReturnsFailureAndReportWithoutDownloading(t *testing.T) {
	root := t.TempDir()
	output, err := run(context.Background(), []string{"check", "--offline", "qwen35b-mtp"}, map[string]string{
		"OUTRIDER_HOME": root, "LLAMA_SERVER_BIN": filepath.Join(root, "missing-runtime"),
	})
	if err == nil {
		t.Fatal("missing files passed")
	}
	var result offlineOutput
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatal(err)
	}
	if result.OfflineReady || !result.Admission.Blocking() {
		t.Fatalf("result = %#v", result)
	}
	for _, id := range []string{"runtime_capabilities", "cached_model", "cached_projector"} {
		found := false
		for _, check := range result.Admission.Checks {
			if check.ID == id && check.Result == admission.ResultFail {
				found = true
			}
		}
		if !found {
			t.Fatalf("missing failure %s: %s", id, output)
		}
	}
	for _, name := range []string{"models", "downloads", "llama.cpp"} {
		if _, err := os.Stat(filepath.Join(root, name)); !os.IsNotExist(err) {
			t.Fatalf("created %s: %v", name, err)
		}
	}
	human, err := humanOutput(result)
	if err != nil || !strings.Contains(human, "Offline preflight blocked") || !strings.Contains(human, "outrider pull qwen35b-mtp") {
		t.Fatalf("human = %q, %v", human, err)
	}
}
