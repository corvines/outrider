package catalog

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/corvines/outrider/internal/manifest"
)

// Vision is proved by the multimodal projector, so a profile that gains one
// gains the capability without a second declaration to keep in step.
func TestVisionFollowsTheMultimodalProjector(t *testing.T) {
	profile := manifest.Profile{ID: "seeing"}
	if FromProfile(profile, Weights{}).Has(CapabilityVision) {
		t.Fatal("a profile with no projector reports vision")
	}
	profile.MultimodalProject = &manifest.Artifact{Repo: "org/model", File: "mmproj.gguf"}
	entry := FromProfile(profile, Weights{})
	if !entry.Has(CapabilityVision) {
		t.Fatalf("capabilities = %v", entry.Capabilities)
	}
}

// Every profile answers completions, and speculation is reported only when a
// draft path is actually configured.
func TestCapabilitiesReportCompletionAndSpeculation(t *testing.T) {
	plain := FromProfile(manifest.Profile{ID: "plain"}, Weights{})
	if !plain.Has(CapabilityCompletion) || plain.Has(CapabilitySpeculation) {
		t.Fatalf("capabilities = %v", plain.Capabilities)
	}
	none := FromProfile(manifest.Profile{
		ID: "declared-none", Speculation: manifest.Speculation{Mode: "none"},
	}, Weights{})
	if none.Has(CapabilitySpeculation) || none.Speculating() {
		t.Fatalf("capabilities = %v speculation = %q", none.Capabilities, none.Speculation)
	}
	drafting := FromProfile(manifest.Profile{
		ID: "drafting", Speculation: manifest.Speculation{Mode: "mtp"},
	}, Weights{})
	if !drafting.Has(CapabilitySpeculation) || drafting.Speculation != "mtp" {
		t.Fatalf("capabilities = %v speculation = %q", drafting.Capabilities, drafting.Speculation)
	}
}

// A truncated download is the same size question a caller asks before
// deciding to resume, so it must not read as a usable model.
func TestInspectWeightsSeparatesPartialFromComplete(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "model.gguf")
	profile := manifest.Profile{ID: "sized", Model: manifest.Artifact{SizeBytes: 8}}

	weights, err := InspectWeights(profile, path)
	if err != nil {
		t.Fatal(err)
	}
	if weights.State != WeightsMissing || weights.OnDiskBytes != 0 || weights.DeclaredBytes != 8 {
		t.Fatalf("missing = %#v", weights)
	}

	if err := os.WriteFile(path, []byte("abc"), 0o600); err != nil {
		t.Fatal(err)
	}
	if weights, err = InspectWeights(profile, path); err != nil {
		t.Fatal(err)
	}
	if weights.State != WeightsMismatched || weights.OnDiskBytes != 3 {
		t.Fatalf("partial = %#v", weights)
	}

	if err := os.WriteFile(path, []byte("abcdefgh"), 0o600); err != nil {
		t.Fatal(err)
	}
	if weights, err = InspectWeights(profile, path); err != nil {
		t.Fatal(err)
	}
	if weights.State != WeightsPresent || weights.OnDiskBytes != 8 {
		t.Fatalf("complete = %#v", weights)
	}
}

// The shipped catalog is what a client sees by default, so development
// profiles stay out of it.
func TestOfferedSkipsDevelopmentProfiles(t *testing.T) {
	t.Setenv("OUTRIDER_DEV", "")
	entries, err := Offered(func(profile manifest.Profile) (string, error) {
		return filepath.Join(t.TempDir(), profile.ID), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatal("catalog is empty")
	}
	for _, entry := range entries {
		if entry.Dev {
			t.Errorf("%s is a development profile", entry.ID)
		}
	}
}
