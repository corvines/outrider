package switcher

import "github.com/corvines/outrider/internal/catalog"

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
