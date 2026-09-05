package guide

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, path string, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The override wins over an installed copy, which is what makes a local edit
// testable without replacing the installed file.
func TestOverrideBeatsTheUserCopy(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	write(t, filepath.Join(home, filepath.FromSlash(UserRelative)), "installed\n")

	override := filepath.Join(t.TempDir(), Filename)
	write(t, override, "override\n")
	t.Setenv(EnvOverride, override)

	source, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if source.Text != "override\n" || source.Path != override {
		t.Fatalf("source = %#v", source)
	}
}

func TestUserCopyIsFoundWithoutAnOverride(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(EnvOverride, "")
	installed := filepath.Join(home, filepath.FromSlash(UserRelative))
	write(t, installed, "installed\n")

	source, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if source.Path != installed {
		t.Fatalf("source = %#v", source)
	}
}

// requireNoSystemGuide keeps the negative cases honest on a machine that has
// Outrider installed system wide.
func requireNoSystemGuide(t *testing.T) {
	t.Helper()
	if _, err := os.Stat(SystemPath); err == nil {
		t.Skipf("%s exists on this machine", SystemPath)
	}
}

// An empty file is treated as absent. A truncated write should not turn help
// mode into a model answering from nothing.
func TestBlankGuideIsSkipped(t *testing.T) {
	requireNoSystemGuide(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	blank := filepath.Join(t.TempDir(), Filename)
	write(t, blank, "   \n")
	t.Setenv(EnvOverride, blank)

	_, err := Load()
	var missing *NotFoundError
	if !errors.As(err, &missing) {
		t.Fatalf("error = %v", err)
	}
}

// The error names every path tried, because the fix is putting the file in one
// of them.
func TestMissingGuideNamesThePathsTried(t *testing.T) {
	requireNoSystemGuide(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(EnvOverride, filepath.Join(t.TempDir(), Filename))

	_, err := Load()
	var missing *NotFoundError
	if !errors.As(err, &missing) {
		t.Fatalf("error = %v", err)
	}
	if len(missing.Looked) < 3 {
		t.Fatalf("looked = %v", missing.Looked)
	}
}
