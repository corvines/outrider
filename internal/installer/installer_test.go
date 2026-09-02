package installer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStageVerifyAndUninstall(t *testing.T) {
	root := t.TempDir()
	binary := testBinary(t, "first")
	marker, err := Stage(binary, root)
	if err != nil {
		t.Fatal(err)
	}
	if marker.Schema != 1 || marker.Target != TargetPath || len(marker.SHA256) != 64 {
		t.Fatalf("marker = %#v", marker)
	}
	verified, err := Verify(root)
	if err != nil {
		t.Fatal(err)
	}
	if verified != marker {
		t.Fatalf("verified marker = %#v", verified)
	}
	if err := Uninstall(root); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{TargetPath, MarkerPath} {
		resolved, _ := rootedPath(root, path)
		if _, err := os.Stat(resolved); !os.IsNotExist(err) {
			t.Fatalf("%s remains: %v", path, err)
		}
	}
}

func TestUninstallRefusesReplacedArtifact(t *testing.T) {
	root := t.TempDir()
	if _, err := Stage(testBinary(t, "owned"), root); err != nil {
		t.Fatal(err)
	}
	target, _ := rootedPath(root, TargetPath)
	marker, _ := rootedPath(root, MarkerPath)
	if err := os.WriteFile(target, []byte("replacement"), 0o755); err != nil {
		t.Fatal(err)
	}
	err := Uninstall(root)
	if err == nil || !strings.Contains(err.Error(), "refusing to remove") {
		t.Fatalf("error = %v", err)
	}
	for _, path := range []string{target, marker} {
		if _, statErr := os.Stat(path); statErr != nil {
			t.Fatalf("protected path %s was removed: %v", path, statErr)
		}
	}
}

func TestRootedPathRejectsRelativeTarget(t *testing.T) {
	if _, err := rootedPath(t.TempDir(), "../../escape"); err == nil {
		t.Fatal("relative escape target was accepted")
	}
}

func testBinary(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "outrider")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}
