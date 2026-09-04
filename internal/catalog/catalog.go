// Package catalog is the one projection of a profile that every surface
// renders: the CLI listing, the OpenAI-shaped model listing, and the
// gateway dashboard. Each surface names these facts in its own convention,
// but none of them derives a fact a second time.
package catalog

import (
	"os"

	"github.com/corvines/outrider/internal/manifest"
)

// Capabilities a profile can carry. The vocabulary matches what other local
// runners publish on their proprietary routes, so a client that already
// reads one can read this one.
const (
	CapabilityCompletion  = "completion"
	CapabilityVision      = "vision"
	CapabilitySpeculation = "speculation"
)

// Weights states.
const (
	WeightsPresent    = "present"
	WeightsMismatched = "mismatched"
	WeightsMissing    = "missing"
)

// Weights is what is on disk for one profile. DeclaredBytes is the download
// size the manifest promises and is always set. OnDiskBytes is the observed
// file size and is zero when the state is missing.
type Weights struct {
	State         string
	Path          string
	DeclaredBytes int64
	OnDiskBytes   int64
}

// Entry is one model as every surface sees it.
type Entry struct {
	ID              string
	Description     string
	Runnable        bool
	Dev             bool
	Repo            string
	File            string
	Quant           string
	LocalPath       string
	Context         int
	TrainingContext int
	MinMemoryMiB    int
	Speculation     string
	Capabilities    []string
	Weights         Weights
}

// FromProfile builds an entry. Weights come from InspectWeights, which needs
// a resolved path the manifest alone does not have.
func FromProfile(profile manifest.Profile, weights Weights) Entry {
	return Entry{
		ID:              profile.ID,
		Description:     profile.Description,
		Runnable:        profile.Runnable,
		Dev:             profile.Dev,
		Repo:            profile.Model.Repo,
		File:            profile.Model.File,
		Quant:           profile.Model.Quant,
		LocalPath:       profile.Model.LocalPath,
		Context:         profile.Context.Size,
		TrainingContext: profile.Context.Original,
		MinMemoryMiB:    profile.Admission.ValidatedPhysicalMemoryMiB,
		Speculation:     speculationMode(profile),
		Capabilities:    capabilities(profile),
		Weights:         weights,
	}
}

// Speculating reports whether the profile draws tokens from a draft path,
// which is the fact the CLI has published as mtp.
func (entry Entry) Speculating() bool {
	return entry.Speculation != ""
}

// Has reports whether the entry carries a capability.
func (entry Entry) Has(capability string) bool {
	for _, candidate := range entry.Capabilities {
		if candidate == capability {
			return true
		}
	}
	return false
}

func speculationMode(profile manifest.Profile) string {
	if profile.Speculation.Mode == "" || profile.Speculation.Mode == "none" {
		return ""
	}
	return profile.Speculation.Mode
}

// capabilities reports only what the manifest proves. Tool calling and
// thinking are not inferable from a profile and are absent until a profile
// declares them.
func capabilities(profile manifest.Profile) []string {
	capabilities := []string{CapabilityCompletion}
	if profile.MultimodalProject != nil {
		capabilities = append(capabilities, CapabilityVision)
	}
	if speculationMode(profile) != "" {
		capabilities = append(capabilities, CapabilitySpeculation)
	}
	return capabilities
}

// InspectWeights reports the on-disk state of a profile's weights. A file
// whose size does not match the manifest is mismatched rather than present,
// so a truncated download is never mistaken for a usable model.
func InspectWeights(profile manifest.Profile, path string) (Weights, error) {
	weights := Weights{State: WeightsMissing, Path: path, DeclaredBytes: profile.Model.SizeBytes}
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return weights, nil
	}
	if err != nil {
		return Weights{}, err
	}
	weights.OnDiskBytes = info.Size()
	if !info.Mode().IsRegular() || (profile.Model.SizeBytes > 0 && info.Size() != profile.Model.SizeBytes) {
		weights.State = WeightsMismatched
		return weights, nil
	}
	weights.State = WeightsPresent
	return weights, nil
}

// Offered builds entries for every profile a caller may serve, in manifest
// order. resolve returns the weights path for a profile.
func Offered(resolve func(manifest.Profile) (string, error)) ([]Entry, error) {
	profiles, err := manifest.Offered()
	if err != nil {
		return nil, err
	}
	entries := make([]Entry, 0, len(profiles))
	for _, profile := range profiles {
		path, err := resolve(profile)
		if err != nil {
			return nil, err
		}
		weights, err := InspectWeights(profile, path)
		if err != nil {
			return nil, err
		}
		entries = append(entries, FromProfile(profile, weights))
	}
	return entries, nil
}
