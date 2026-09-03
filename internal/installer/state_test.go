package installer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInspectStateRootReportsMissingAndPopulatedRoots(t *testing.T) {
	root := filepath.Join(t.TempDir(), "Outrider")
	report, err := InspectStateRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	if report.Exists || report.Bytes != 0 || report.Root != root {
		t.Fatalf("missing root report = %#v", report)
	}
	writeStateFixture(t, root, "models/tiny.gguf", "0123456789")
	report, err = InspectStateRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Exists || report.Bytes != 10 {
		t.Fatalf("populated root report = %#v", report)
	}
	if len(report.Entries) != 1 || report.Entries[0] != "models" {
		t.Fatalf("entries = %v", report.Entries)
	}
}

func TestRemoveStateRootDeletesAnOwnedLayout(t *testing.T) {
	root := filepath.Join(t.TempDir(), "Outrider")
	writeStateFixture(t, root, "llama.cpp/b1/llama-server", "binary")
	writeStateFixture(t, root, "runs/active.json", "{}")
	if err := RemoveStateRoot(root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("state root remains: %v", err)
	}
	if err := RemoveStateRoot(root); err != nil {
		t.Fatalf("removing a missing root = %v", err)
	}
}

func TestRemoveStateRootRefusesUnrecognisedTargets(t *testing.T) {
	unrelated := filepath.Join(t.TempDir(), "Documents")
	writeStateFixture(t, unrelated, "taxes.pdf", "keep me")
	target := filepath.Join(t.TempDir(), "Outrider")
	writeStateFixture(t, target, "models/tiny.gguf", "model")
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	for name, testCase := range map[string]struct{ root, want string }{
		"symlink":     {link, "it is a symlink"},
		"shallow":     {"/tmp", "too close to the volume root"},
		"unowned":     {unrelated, "it holds none of"},
		"empty input": {"  ", "a state root is required"},
	} {
		err := RemoveStateRoot(testCase.root)
		if err == nil || !strings.Contains(err.Error(), testCase.want) {
			t.Fatalf("%s: err = %v, want %q", name, err, testCase.want)
		}
	}
	if _, err := os.Stat(filepath.Join(unrelated, "taxes.pdf")); err != nil {
		t.Fatalf("unrelated directory disturbed: %v", err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("symlink target disturbed: %v", err)
	}
}

func TestRemoveStateRootAcceptsAnEmptyRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "Outrider")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := RemoveStateRoot(root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("empty root remains: %v", err)
	}
}

func writeStateFixture(t *testing.T, root string, relative string, contents string) {
	t.Helper()
	path := filepath.Join(root, relative)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}
