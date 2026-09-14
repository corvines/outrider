package scripts

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDesktopInstaller(t *testing.T) {
	for _, scenario := range []string{"fresh", "existing-app", "different-cli", "bad-signature", "bad-checksum"} {
		t.Run(scenario, func(t *testing.T) {
			root := t.TempDir()
			bin := filepath.Join(root, "bin")
			payload := filepath.Join(root, "payload")
			apps := filepath.Join(root, "Applications")
			called := filepath.Join(root, "cli-installed")
			write := func(path, content string) {
				t.Helper()
				if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(content), 0755); err != nil {
					t.Fatal(err)
				}
			}
			cli := "#!/bin/sh\nif [ \"$1\" = install ]; then\n touch \"$TEST_CALLED\"\n printf '{\"target\":\"%s/outrider\"}\\n' \"$TEST_ROOT\"\nfi\n"
			write(filepath.Join(payload, "outrider"), cli)
			write(filepath.Join(payload, "Outrider.app/Contents/MacOS/outrider"), cli)
			write(filepath.Join(payload, "Outrider.app/Contents/MacOS/outrider-dashboard"), "#!/bin/sh\nexit 0\n")
			if scenario == "different-cli" {
				write(filepath.Join(payload, "outrider"), cli+"# different\n")
			}
			if scenario == "existing-app" {
				write(filepath.Join(apps, "Outrider.app/keep"), "existing")
			}
			archive := filepath.Join(root, "outrider_desktop_darwin_arm64.tar.gz")
			if out, err := exec.Command("tar", "-czf", archive, "-C", payload, "outrider", "Outrider.app").CombinedOutput(); err != nil {
				t.Fatalf("tar: %v: %s", err, out)
			}
			data, err := os.ReadFile(archive)
			if err != nil {
				t.Fatal(err)
			}
			sum := fmt.Sprintf("%x", sha256.Sum256(data))
			if scenario == "bad-checksum" {
				sum = strings.Repeat("0", 64)
			}
			write(filepath.Join(root, "SHA256SUMS"), sum+"  "+filepath.Base(archive)+"\n")
			write(filepath.Join(bin, "uname"), "#!/bin/sh\ncase \"$1\" in -s) echo Darwin;; -m) echo arm64;; esac\n")
			write(filepath.Join(bin, "curl"), "#!/bin/sh\ncp \"$TEST_ROOT/${2##*/}\" \"$4\"\n")
			write(filepath.Join(bin, "ditto"), "#!/bin/sh\ncp -R \"$1\" \"$2\"\n")
			signExit := "0"
			if scenario == "bad-signature" {
				signExit = "1"
			}
			write(filepath.Join(bin, "codesign"), "#!/bin/sh\nexit "+signExit+"\n")
			cmd := exec.Command("sh", "install.sh")
			cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"), "TEST_ROOT="+root, "TEST_CALLED="+called, "OUTRIDER_APPLICATIONS_DIR="+apps)
			out, err := cmd.CombinedOutput()
			if scenario != "fresh" {
				if err == nil {
					t.Fatalf("expected rejection: %s", out)
				}
				if _, err := os.Stat(called); !os.IsNotExist(err) {
					t.Fatal("CLI install ran before preflight rejection")
				}
				if scenario == "existing-app" {
					data, err := os.ReadFile(filepath.Join(apps, "Outrider.app/keep"))
					if err != nil || string(data) != "existing" {
						t.Fatal("existing app changed")
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("install: %v: %s", err, out)
			}
			if _, err := os.Stat(called); err != nil {
				t.Fatal("CLI not installed")
			}
			if _, err := os.Stat(filepath.Join(apps, "Outrider.app/Contents/MacOS/outrider-dashboard")); err != nil {
				t.Fatal("app not installed")
			}
			if !strings.Contains(string(out), "not notarized") {
				t.Fatal("missing preview warning")
			}
		})
	}
}
