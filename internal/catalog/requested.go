package catalog

import (
	"strconv"

	"github.com/corvines/outrider/internal/manifest"
)

// Requested is what outrider asked the backend for. It is free to report,
// because these are outrider's own launch flags, and it is worth reporting
// because a backend confirms only some of them.
type Requested struct {
	Context        int
	GPULayers      string
	FlashAttention bool
	KVKeyType      string
	KVValueType    string
	KVUnified      bool
	BatchSize      int
	MicroBatchSize int
	Parallel       int
	Mmap           bool
	Mlock          bool
	Speculation    string
	Sampling       Sampling
}

// Sampling is the sampler the profile asks for.
type Sampling struct {
	Temperature   float64
	TopP          float64
	TopK          int
	MinP          float64
	RepeatPenalty float64
}

// RequestedFrom reads the launch settings off a profile.
func RequestedFrom(profile manifest.Profile) Requested {
	return Requested{
		Context:        profile.Context.Size,
		GPULayers:      gpuLayers(profile.GPULayers),
		FlashAttention: profile.FlashAttention,
		KVKeyType:      profile.KVCache.KeyType,
		KVValueType:    profile.KVCache.ValueType,
		KVUnified:      profile.KVCache.Unified,
		BatchSize:      profile.Batch.Size,
		MicroBatchSize: profile.Batch.MicroSize,
		Parallel:       profile.Batch.Parallel,
		Mmap:           profile.Memory.Mmap,
		Mlock:          profile.Memory.Mlock,
		Speculation:    speculationMode(profile),
		Sampling: Sampling{
			Temperature:   profile.Sampling.Temperature,
			TopP:          profile.Sampling.TopP,
			TopK:          profile.Sampling.TopK,
			MinP:          profile.Sampling.MinP,
			RepeatPenalty: profile.Sampling.RepeatPenalty,
		},
	}
}

// gpuLayers renders the manifest's mode-or-count as one string, so a caller
// reads "all" and "32" from the same field rather than guessing at a type.
func gpuLayers(layers manifest.GPULayers) string {
	if layers.Mode != "" {
		return layers.Mode
	}
	return strconv.Itoa(layers.Count)
}
