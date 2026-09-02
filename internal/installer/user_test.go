package installer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUserInstallUpgradeVerifyAndUninstall(t *testing.T) {
	home := t.TempDir()
	first := testBinary(t, "first")
	marker, err := InstallUser(first, home)
	if err != nil {
		t.Fatal(err)
	}
	layout, _ := ResolveUserLayout(home)
	if marker.Target != layout.Target || marker.Schema != MarkerSchema {
		t.Fatalf("marker = %#v", marker)
	}
	second := testBinary(t, "second")
	upgraded, err := InstallUser(second, home)
	if err != nil {
		t.Fatal(err)
	}
	if upgraded.SHA256 == marker.SHA256 {
		t.Fatal("upgrade did not replace the installed artifact")
	}
	verified, err := VerifyUser(home)
	if err != nil || verified != upgraded {
		t.Fatalf("verified = %#v, error = %v", verified, err)
	}
	if err := UninstallUser(home); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{layout.Target, layout.Marker} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("%s remains: %v", path, err)
		}
	}
}

func TestUserInstallRefusesUnownedOrModifiedTarget(t *testing.T) {
	home := t.TempDir()
	layout, _ := ResolveUserLayout(home)
	if err := os.MkdirAll(filepath.Dir(layout.Target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(layout.Target, []byte("unowned"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := InstallUser(testBinary(t, "new"), home); err == nil || !strings.Contains(err.Error(), "incomplete") {
		t.Fatalf("unowned target error = %v", err)
	}
	if err := os.Remove(layout.Target); err != nil {
		t.Fatal(err)
	}
	if _, err := InstallUser(testBinary(t, "owned"), home); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(layout.Target, []byte("modified"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := InstallUser(testBinary(t, "new"), home); err == nil || !strings.Contains(err.Error(), "refusing to replace") {
		t.Fatalf("modified target error = %v", err)
	}
}
