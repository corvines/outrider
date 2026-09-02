package admission

import (
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
