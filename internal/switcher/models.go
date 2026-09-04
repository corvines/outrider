package switcher

import (
	"github.com/corvines/outrider/internal/catalog"
	"github.com/corvines/outrider/internal/endpoint"
)

// modelEntry is the OpenAI model object plus the facts that object leaves
// out. The four required keys come first and keep their spec meaning; a
// client that knows nothing about outrider reads those and ignores the rest.
type modelEntry struct {
	ID             string    `json:"id"`
	Object         string    `json:"object"`
	OwnedBy        string    `json:"owned_by"`
	Description    string    `json:"description,omitempty"`
	Capabilities   []string  `json:"capabilities"`
	Quantization   string    `json:"quantization,omitempty"`
	Meta           modelMeta `json:"meta"`
	Repo           string    `json:"repo,omitempty"`
	File           string    `json:"file,omitempty"`
	MinMemoryBytes int64     `json:"min_memory_bytes,omitempty"`
	Speculation    string    `json:"speculation,omitempty"`
	Weights        string    `json:"weights,omitempty"`
	SizeBytes      int64     `json:"size_bytes,omitempty"`
	OnDiskBytes    int64     `json:"on_disk_bytes,omitempty"`
}

type modelMeta struct {
	Context         int `json:"n_ctx"`
	TrainingContext int `json:"n_ctx_train,omitempty"`
}

func newModelEntry(entry catalog.Entry, weights catalog.Weights) modelEntry {
	item := modelEntry{
		ID:           entry.ID,
		Object:       "model",
		OwnedBy:      "outrider",
		Description:  entry.Description,
		Capabilities: entry.Capabilities,
		Quantization: entry.Quant,
		Meta: modelMeta{
			Context:         entry.Context,
			TrainingContext: entry.TrainingContext,
		},
		Repo:        entry.Repo,
		File:        entry.File,
		Speculation: entry.Speculation,
		Weights:     weights.State,
		SizeBytes:   weights.DeclaredBytes,
		OnDiskBytes: weights.OnDiskBytes,
	}
	if entry.MinMemoryMiB > 0 {
		item.MinMemoryBytes = int64(entry.MinMemoryMiB) << 20
	}
	if item.Capabilities == nil {
		item.Capabilities = []string{}
	}
	return item
}

// modelDetail is one model on its own route: the listing entry, the launch
// settings outrider asked for, and what the running backend reports back.
// A caller compares the two halves rather than trusting either alone.
type modelDetail struct {
	modelEntry
	Requested requestedSettings `json:"requested"`
	Resolved  resolvedState     `json:"resolved"`
}

type requestedSettings struct {
	Context        int              `json:"n_ctx"`
	GPULayers      string           `json:"n_gpu_layers,omitempty"`
	FlashAttention bool             `json:"flash_attn"`
	KVKeyType      string           `json:"kv_key_type,omitempty"`
	KVValueType    string           `json:"kv_value_type,omitempty"`
	KVUnified      bool             `json:"kv_unified"`
	BatchSize      int              `json:"n_batch,omitempty"`
	MicroBatchSize int              `json:"n_ubatch,omitempty"`
	Parallel       int              `json:"n_parallel,omitempty"`
	Mmap           bool             `json:"mmap"`
	Mlock          bool             `json:"mlock"`
	Speculation    string           `json:"speculation,omitempty"`
	Sampling       samplingSettings `json:"sampling"`
}

type samplingSettings struct {
	Temperature   float64 `json:"temperature"`
	TopP          float64 `json:"top_p"`
	TopK          int     `json:"top_k"`
	MinP          float64 `json:"min_p"`
	RepeatPenalty float64 `json:"repeat_penalty"`
}

type resolvedState struct {
	Loaded          bool              `json:"loaded"`
	Endpoint        string            `json:"endpoint,omitempty"`
	Build           string            `json:"build,omitempty"`
	Context         int               `json:"n_ctx,omitempty"`
	TrainingContext int               `json:"n_ctx_train,omitempty"`
	Quantization    string            `json:"quantization,omitempty"`
	ModelPath       string            `json:"model_path,omitempty"`
	Slots           int               `json:"n_slots,omitempty"`
	Modalities      *modalities       `json:"modalities,omitempty"`
	SupportsTools   bool              `json:"supports_tools,omitempty"`
	Speculation     string            `json:"speculation,omitempty"`
	Sampling        *samplingSettings `json:"sampling,omitempty"`
	Samplers        []string          `json:"samplers,omitempty"`
}

type modalities struct {
	Vision bool `json:"vision"`
	Audio  bool `json:"audio"`
	Video  bool `json:"video"`
}

func newRequestedSettings(requested catalog.Requested) requestedSettings {
	return requestedSettings{
		Context: requested.Context, GPULayers: requested.GPULayers,
		FlashAttention: requested.FlashAttention,
		KVKeyType:      requested.KVKeyType, KVValueType: requested.KVValueType,
		KVUnified: requested.KVUnified,
		BatchSize: requested.BatchSize, MicroBatchSize: requested.MicroBatchSize,
		Parallel: requested.Parallel, Mmap: requested.Mmap, Mlock: requested.Mlock,
		Speculation: requested.Speculation,
		Sampling: samplingSettings{
			Temperature: requested.Sampling.Temperature, TopP: requested.Sampling.TopP,
			TopK: requested.Sampling.TopK, MinP: requested.Sampling.MinP,
			RepeatPenalty: requested.Sampling.RepeatPenalty,
		},
	}
}

func newResolvedState(resolved endpoint.Resolved) resolvedState {
	return resolvedState{
		Loaded: true, Endpoint: resolved.Endpoint, Build: resolved.Build,
		Context: resolved.Context, TrainingContext: resolved.TrainingContext,
		Quantization: resolved.Quantization, ModelPath: resolved.ModelPath,
		Slots: resolved.Slots, SupportsTools: resolved.SupportsTools,
		Speculation: resolved.Speculation, Samplers: resolved.Samplers,
		Modalities: &modalities{
			Vision: resolved.Modalities.Vision,
			Audio:  resolved.Modalities.Audio,
			Video:  resolved.Modalities.Video,
		},
		Sampling: &samplingSettings{
			Temperature: resolved.Sampling.Temperature, TopP: resolved.Sampling.TopP,
			TopK: resolved.Sampling.TopK, MinP: resolved.Sampling.MinP,
			RepeatPenalty: resolved.Sampling.RepeatPenalty,
		},
	}
}
