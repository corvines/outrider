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
	if _, err := InstallUserWithOptions(testBinary(t, "owned"), home, UserInstallOptions{ReplaceUnmanaged: true}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(layout.Target, []byte("modified"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := InstallUser(testBinary(t, "new"), home); err == nil || !strings.Contains(err.Error(), "refusing to replace") {
		t.Fatalf("modified target error = %v", err)
	}
	if _, err := InstallUserWithOptions(
		testBinary(t, "forced"), home, UserInstallOptions{ReplaceUnmanaged: true},
	); err == nil || !strings.Contains(err.Error(), "refusing to replace") {
		t.Fatalf("modified managed target was replaced: %v", err)
	}
}

func TestInstallUserLinksInsteadOfCopying(t *testing.T) {
	home := t.TempDir()
	binary := testBinary(t, "linked")
	marker, err := InstallUserWithOptions(binary, home, UserInstallOptions{Link: true})
	if err != nil {
		t.Fatal(err)
	}
	if marker.Link != binary || marker.SHA256 != "" {
		t.Fatalf("marker = %#v", marker)
	}
	layout, err := ResolveUserLayout(home)
	if err != nil {
		t.Fatal(err)
	}
	destination, err := os.Readlink(layout.Target)
	if err != nil {
		t.Fatal(err)
	}
	if destination != binary {
		t.Fatalf("target points at %s, want %s", destination, binary)
	}
	verified, err := VerifyUser(home)
	if err != nil {
		t.Fatal(err)
	}
	if verified != marker {
		t.Fatalf("verified = %#v", verified)
	}
	if err := UninstallUser(home); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(layout.Target); !os.IsNotExist(err) {
		t.Fatalf("link remains: %v", err)
	}
}

func TestInstallUserRefusesARedirectedLink(t *testing.T) {
	home := t.TempDir()
	if _, err := InstallUserWithOptions(testBinary(t, "linked"), home, UserInstallOptions{Link: true}); err != nil {
		t.Fatal(err)
	}
	layout, _ := ResolveUserLayout(home)
	if err := os.Remove(layout.Target); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(testBinary(t, "other"), layout.Target); err != nil {
		t.Fatal(err)
	}
	_, err := InstallUserWithOptions(testBinary(t, "linked"), home, UserInstallOptions{Link: true})
	if err == nil || !strings.Contains(err.Error(), "refusing to replace") {
		t.Fatalf("error = %v", err)
	}
}

func TestInstallUserReplacesALinkWithACopy(t *testing.T) {
	home := t.TempDir()
	binary := testBinary(t, "same")
	if _, err := InstallUserWithOptions(binary, home, UserInstallOptions{Link: true}); err != nil {
		t.Fatal(err)
	}
	marker, err := InstallUserWithOptions(binary, home, UserInstallOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if marker.Link != "" || len(marker.SHA256) != 64 {
		t.Fatalf("marker = %#v", marker)
	}
	layout, _ := ResolveUserLayout(home)
	info, err := os.Lstat(layout.Target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatal("target is still a symlink")
	}
}
