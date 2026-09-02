package admission

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/corvines/outrider/internal/manifest"
)

func TestAdmissionClasses(t *testing.T) {
	profile, _ := manifest.Get("tiny")
	plan, err := manifest.Resolve(profile, manifest.ResolveOptions{Root: t.TempDir(), Executable: "/runtime"})
	if err != nil {
		t.Fatal(err)
	}
	ready := Snapshot{
		OS: "darwin", Arch: "arm64", PhysicalMemoryBytes: 64 * 1024 * 1024 * 1024,
		MemoryFreePercent: 50, AvailableDiskBytes: 10 * 1024 * 1024 * 1024,
		StateWritable: true, StateOwned: true, PortAvailable: true,
	}
	if report := Evaluate(profile, plan, ready); report.Class != ClassReady {
		t.Fatalf("ready report = %#v", report)
	}

	degraded := ready
	degraded.PhysicalMemoryBytes = 32 * 1024 * 1024 * 1024
	if report := Evaluate(profile, plan, degraded); report.Class != ClassDegraded {
		t.Fatalf("degraded report = %#v", report)
	}

	blocked := ready
	blocked.PortAvailable = false
	if report := Evaluate(profile, plan, blocked); report.Class != ClassBlocked {
		t.Fatalf("blocked report = %#v", report)
	}

	impossible := ready
	impossible.OS = "linux"
	if report := Evaluate(profile, plan, impossible); report.Class != ClassImpossible {
		t.Fatalf("impossible report = %#v", report)
	}
}

func TestRuntimeCapabilityFailureUsesAdmissionContract(t *testing.T) {
	profile, _ := manifest.Get("tiny")
	root := t.TempDir()
	executable := filepath.Join(root, "llama-server")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\necho '  --host HOST'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	plan, _ := manifest.ResolveCached(profile, manifest.ResolveOptions{Root: root, Executable: executable})
	report := WithRuntimeCapabilities(context.Background(), Report{Profile: profile.ID, Class: ClassReady}, plan, true)
	if report.Class != ClassBlocked {
		t.Fatalf("report = %#v", report)
	}
	last := report.Checks[len(report.Checks)-1]
	if last.ID != "runtime_capabilities" || last.Result != ResultFail || last.NextAction == "" {
		t.Fatalf("capability check = %#v", last)
	}
}

func TestBlockingErrorNamesMeasurementRequirementAndAction(t *testing.T) {
	profile, _ := manifest.Get("tiny")
	plan, _ := manifest.Resolve(profile, manifest.ResolveOptions{Root: t.TempDir(), Executable: "/runtime"})
	report := Evaluate(profile, plan, Snapshot{
		OS: "darwin", Arch: "arm64", PhysicalMemoryBytes: 64 * 1024 * 1024 * 1024,
		MemoryFreePercent: 50, AvailableDiskBytes: 10 * 1024 * 1024 * 1024,
		StateWritable: true, StateOwned: true, PortAvailable: false,
	})
	err := (&Error{Report: report}).Error()
	for _, want := range []string{"port", "measured", "requires", "stop the existing listener"} {
		if !strings.Contains(err, want) {
			t.Fatalf("error %q does not contain %q", err, want)
		}
	}
}
