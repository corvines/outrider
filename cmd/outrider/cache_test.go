package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/corvines/outrider/internal/manifest"
)

func TestCacheCleanProtectsProfileResume(t *testing.T) {
	root := t.TempDir()
	state, err := manifest.Paths(root, mustProfile(t, "qwen35b-mtp"), "")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(state.Root, "downloads"), 0o700); err != nil {
		t.Fatal(err)
	}
	profilePart := state.Model + ".part"
	profileResume := profilePart + ".resume.json"
	stalePart := filepath.Join(state.Models, "old-model.gguf.part")
	staleCorrupt := filepath.Join(state.Models, "old-runtime.corrupt")
	staleArchive := filepath.Join(state.Root, "downloads", "old.tar.gz.part")
	for _, path := range []string{profilePart, profileResume, stalePart, staleCorrupt, staleArchive} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("partial"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	environment := map[string]string{"OUTRIDER_HOME": root}
	dryJSON, err := run(context.Background(), []string{"cache", "clean"}, environment)
	if err != nil {
		t.Fatal(err)
	}
	var dry cacheCleanupOutput
	if err := json.Unmarshal([]byte(dryJSON), &dry); err != nil {
		t.Fatal(err)
	}
	if !dry.DryRun || len(dry.Candidates) != 3 || len(dry.Protected) != 2 {
		t.Fatalf("dry run = %#v", dry)
	}
	applyJSON, err := run(context.Background(), []string{"cache", "clean", "--apply"}, environment)
	if err != nil {
		t.Fatal(err)
	}
	var applied cacheCleanupOutput
	if err := json.Unmarshal([]byte(applyJSON), &applied); err != nil {
		t.Fatal(err)
	}
	if applied.DryRun || len(applied.Removed) != 3 {
		t.Fatalf("apply = %#v", applied)
	}
	for _, path := range []string{profilePart, profileResume} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("protected path %s: %v", path, err)
		}
	}
	for _, path := range []string{stalePart, staleCorrupt, staleArchive} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("stale path %s remains: %v", path, err)
		}
	}
}

func mustProfile(t *testing.T, id string) manifest.Profile {
	t.Helper()
	profile, err := manifest.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	return profile
}
