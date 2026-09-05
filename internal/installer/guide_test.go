package installer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/corvines/outrider/internal/guide"
)

// A release archive puts the guide beside the binary, so an install carries it
// to where the chat window reads it and an uninstall takes it back out.
func TestUserInstallPlacesAndRemovesTheGuide(t *testing.T) {
	binary := testBinary(t, "true")
	shipped := filepath.Join(filepath.Dir(binary), guide.Filename)
	if err := os.WriteFile(shipped, []byte("shipped guide\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	home := t.TempDir()
	if _, err := InstallUser(binary, home); err != nil {
		t.Fatal(err)
	}
	layout, err := ResolveUserLayout(home)
	if err != nil {
		t.Fatal(err)
	}
	placed, err := os.ReadFile(layout.Guide)
	if err != nil {
		t.Fatal(err)
	}
	if string(placed) != "shipped guide\n" {
		t.Fatalf("guide = %q", placed)
	}

	if err := UninstallUser(home); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(layout.Guide); !os.IsNotExist(err) {
		t.Fatalf("guide remains: %v", err)
	}
}

// A repository build has no guide beside it, only under docs/.
func TestFindGuidePrefersTheArchiveLayoutThenTheRepository(t *testing.T) {
	root := t.TempDir()
	binary := filepath.Join(root, "outrider")
	docs := filepath.Join(root, "docs", guide.Filename)
	if err := os.MkdirAll(filepath.Dir(docs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(docs, []byte("repository\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if found := FindGuide(binary); found != docs {
		t.Fatalf("found = %q, want %q", found, docs)
	}

	beside := filepath.Join(root, guide.Filename)
	if err := os.WriteFile(beside, []byte("archive\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if found := FindGuide(binary); found != beside {
		t.Fatalf("found = %q, want %q", found, beside)
	}
}

// A binary that ships without a guide still installs. Help mode reports the
// absence itself, which is more useful than refusing to install anything.
func TestUserInstallWithoutAShippedGuide(t *testing.T) {
	home := t.TempDir()
	if _, err := InstallUser(testBinary(t, "true"), home); err != nil {
		t.Fatal(err)
	}
	layout, err := ResolveUserLayout(home)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(layout.Guide); !os.IsNotExist(err) {
		t.Fatalf("guide exists: %v", err)
	}
	if err := UninstallUser(home); err != nil {
		t.Fatal(err)
	}
}
