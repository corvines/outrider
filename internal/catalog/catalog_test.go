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

// A caller reads "all" and "32" out of the same field, so the manifest's
// mode-or-count is rendered as one string.
func TestRequestedRendersGPULayersAsOneField(t *testing.T) {
	byMode := RequestedFrom(manifest.Profile{GPULayers: manifest.GPULayers{Mode: "all"}})
	if byMode.GPULayers != "all" {
		t.Fatalf("mode = %q", byMode.GPULayers)
	}
	byCount := RequestedFrom(manifest.Profile{GPULayers: manifest.GPULayers{Count: 32}})
	if byCount.GPULayers != "32" {
		t.Fatalf("count = %q", byCount.GPULayers)
	}
}

// Requested carries the launch flags a backend does not report back, which
// is the only place they are visible at all.
func TestRequestedCarriesWhatTheBackendWillNotReport(t *testing.T) {
	requested := RequestedFrom(manifest.Profile{
		Context:        manifest.Context{Size: 32768},
		FlashAttention: true,
		KVCache:        manifest.KVCache{KeyType: "q8_0", ValueType: "q8_0", Unified: true},
		Speculation:    manifest.Speculation{Mode: "none"},
	})
	if !requested.FlashAttention || requested.KVKeyType != "q8_0" || !requested.KVUnified {
		t.Fatalf("requested = %#v", requested)
	}
	if requested.Context != 32768 {
		t.Fatalf("context = %d", requested.Context)
	}
	if requested.Speculation != "" {
		t.Fatalf("speculation = %q", requested.Speculation)
	}
}
