package manifest

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
)

const (
	DefaultHost = "127.0.0.1"
	DefaultPort = 11435
)

var LlamaRelease = Release{
	Tag:       "b10516",
	Commit:    "b95502b",
	Asset:     "llama-b10516-bin-macos-arm64.tar.gz",
	Directory: "llama-b10516",
	SHA256:    "ee3324327d621026ae80c24031670e65fa62a0b23a3a027dbe2f65f240affd30",
	URL:       "https://github.com/ggml-org/llama.cpp/releases/download/b10516/llama-b10516-bin-macos-arm64.tar.gz",
}

var protectedFlags = map[string]struct{}{
	"--host": {}, "--port": {}, "--model": {}, "-m": {},
	"--hf-repo": {}, "-hf": {}, "--hf-file": {}, "-hff": {},
	"--ctx-size": {}, "-c": {}, "--n-gpu-layers": {},
	"--gpu-layers": {}, "-ngl": {}, "--fit": {}, "--flash-attn": {},
	"--spec-type": {}, "--spec-draft-n-max": {}, "--spec-draft-model": {}, "-md": {},
}

//go:embed profiles.json
var profileFiles embed.FS

type Release struct {
	Tag       string
	Commit    string
	Asset     string
	Directory string
	SHA256    string
	URL       string
}

type ManifestError struct {
	Field   string
	Message string
}

func (e *ManifestError) Error() string {
	if e.Field == "" {
		return e.Message
	}
	return e.Field + ": " + e.Message
}

type Artifact struct {
	Repo      string `json:"repo,omitempty"`
	File      string `json:"file,omitempty"`
	Quant     string `json:"quant,omitempty"`
	LocalPath string `json:"localPath,omitempty"`
	SHA256    string `json:"sha256,omitempty"`
}

type Context struct {
	Size     int `json:"size"`
	Original int `json:"original"`
}

type GPULayers struct {
	Mode  string
	Count int
}

func (g *GPULayers) UnmarshalJSON(data []byte) error {
	var mode string
	if err := json.Unmarshal(data, &mode); err == nil {
		if mode != "auto" && mode != "all" {
			return fmt.Errorf("unknown mode %q", mode)
		}
		g.Mode = mode
		return nil
	}
	var count int
	if err := json.Unmarshal(data, &count); err != nil {
		return errors.New("must be auto, all, or an integer")
	}
	g.Count = count
	return nil
}

type KVCache struct {
	KeyType   string `json:"keyType"`
	ValueType string `json:"valueType"`
	Unified   bool   `json:"unified"`
}

type Batch struct {
	Size      int `json:"size"`
	MicroSize int `json:"microSize"`
	Parallel  int `json:"parallel"`
}

type Memory struct {
	Mmap               bool `json:"mmap"`
	Mlock              bool `json:"mlock"`
	CacheRAMMiB        *int `json:"cacheRamMiB,omitempty"`
	ContextCheckpoints *int `json:"contextCheckpoints,omitempty"`
	CheckpointMinStep  *int `json:"checkpointMinStep,omitempty"`
}

type Sampling struct {
	Temperature   float64 `json:"temperature"`
	TopP          float64 `json:"topP"`
	TopK          int     `json:"topK"`
	MinP          float64 `json:"minP"`
	RepeatPenalty float64 `json:"repeatPenalty"`
}

type Speculation struct {
	Mode             string    `json:"mode"`
	Draft            *Artifact `json:"draft,omitempty"`
	Tokens           int       `json:"tokens,omitempty"`
	MinTokens        *int      `json:"minTokens,omitempty"`
	MinProbability   *float64  `json:"minProbability,omitempty"`
	NgramMatchTokens int       `json:"ngramMatchTokens,omitempty"`
}

type Profile struct {
	ID                string      `json:"id"`
	Description       string      `json:"description"`
	Model             Artifact    `json:"model"`
	Context           Context     `json:"context"`
	GPULayers         GPULayers   `json:"gpuLayers"`
	Fit               string      `json:"fit"`
	FlashAttention    bool        `json:"flashAttention"`
	KVCache           KVCache     `json:"kvCache"`
	Batch             Batch       `json:"batch"`
	Memory            Memory      `json:"memory"`
	Jinja             bool        `json:"jinja"`
	ChatTemplate      string      `json:"chatTemplate,omitempty"`
	ChatTemplateFile  string      `json:"chatTemplateFile,omitempty"`
	MultimodalProject *Artifact   `json:"multimodalProject,omitempty"`
	SystemPrompt      string      `json:"systemPrompt"`
	Sampling          Sampling    `json:"sampling"`
	Speculation       Speculation `json:"speculation"`
	ExtraArgs         []string    `json:"extraArgs"`
}

type StatePaths struct {
	Root       string `json:"root"`
	Models     string `json:"models"`
	Model      string `json:"model"`
	Run        string `json:"run"`
	PID        string `json:"pid"`
	Log        string `json:"log"`
	Executable string `json:"executable"`
}

type Plan struct {
	Profile    Profile    `json:"profile"`
	Host       string     `json:"host"`
	Port       int        `json:"port"`
	Endpoint   string     `json:"endpoint"`
	Executable string     `json:"executable"`
	Args       []string   `json:"args"`
	State      StatePaths `json:"state"`
}

type BuildOptions struct {
	Port int
	CWD  string
}

type ResolveOptions struct {
	Root       string
	Executable string
	Port       *int
	CWD        string
}

func All() ([]Profile, error) {
	data, err := profileFiles.ReadFile("profiles.json")
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var profiles []Profile
	if err := decoder.Decode(&profiles); err != nil {
		return nil, fmt.Errorf("decode profiles: %w", err)
	}
	seen := make(map[string]struct{}, len(profiles))
	for i := range profiles {
		if err := Validate(profiles[i]); err != nil {
			return nil, fmt.Errorf("profile %q: %w", profiles[i].ID, err)
		}
		if _, exists := seen[profiles[i].ID]; exists {
			return nil, fmt.Errorf("duplicate profile %q", profiles[i].ID)
		}
		seen[profiles[i].ID] = struct{}{}
	}
	return profiles, nil
}

func Get(id string) (Profile, error) {
	profiles, err := All()
	if err != nil {
		return Profile{}, err
	}
	for _, profile := range profiles {
		if profile.ID == id {
			profile.ExtraArgs = append([]string(nil), profile.ExtraArgs...)
			return profile, nil
		}
	}
	return Profile{}, &ManifestError{Field: "id", Message: fmt.Sprintf("unknown profile %q", id)}
}

func Validate(profile Profile) error {
	if strings.TrimSpace(profile.ID) == "" {
		return manifestError("id", "is required")
	}
	if strings.TrimSpace(profile.Description) == "" {
		return manifestError("description", "is required")
	}
	if err := validateArtifact("model", profile.Model); err != nil {
		return err
	}
	if profile.Context.Size <= 0 || profile.Context.Original <= 0 {
		return manifestError("context", "sizes must be positive")
	}
	if profile.Context.Size > profile.Context.Original {
		return manifestError("context.size", "cannot exceed original context")
	}
	if profile.GPULayers.Mode == "" && profile.GPULayers.Count < 0 {
		return manifestError("gpuLayers", "must be nonnegative")
	}
	if profile.Fit != "on" && profile.Fit != "off" {
		return manifestError("fit", "must be on or off")
	}
	if profile.Batch.Size <= 0 || profile.Batch.MicroSize <= 0 || profile.Batch.Parallel <= 0 {
		return manifestError("batch", "values must be positive")
	}
	if profile.Batch.MicroSize > profile.Batch.Size {
		return manifestError("batch.microSize", "cannot exceed batch size")
	}
	for field, value := range map[string]*int{
		"memory.cacheRamMiB":        profile.Memory.CacheRAMMiB,
		"memory.contextCheckpoints": profile.Memory.ContextCheckpoints,
		"memory.checkpointMinStep":  profile.Memory.CheckpointMinStep,
	} {
		if value != nil && *value < 0 {
			return manifestError(field, "must be nonnegative")
		}
	}
	if profile.ChatTemplate != "" && profile.ChatTemplateFile != "" {
		return manifestError("chatTemplate", "cannot be combined with chatTemplateFile")
	}
	if profile.MultimodalProject != nil {
		if err := validateArtifact("multimodalProject", *profile.MultimodalProject); err != nil {
			return err
		}
	}
	if !finiteNonnegative(profile.Sampling.Temperature) {
		return manifestError("sampling.temperature", "must be finite and nonnegative")
	}
	if !probability(profile.Sampling.TopP) || !probability(profile.Sampling.MinP) {
		return manifestError("sampling", "probabilities must be between zero and one")
	}
	if profile.Sampling.TopK < 0 {
		return manifestError("sampling.topK", "must be nonnegative")
	}
	if math.IsNaN(profile.Sampling.RepeatPenalty) || math.IsInf(profile.Sampling.RepeatPenalty, 0) || profile.Sampling.RepeatPenalty <= 0 {
		return manifestError("sampling.repeatPenalty", "must be finite and positive")
	}
	if err := validateSpeculation(profile.Speculation); err != nil {
		return err
	}
	for i, arg := range profile.ExtraArgs {
		if arg == "" || strings.ContainsAny(arg, "\x00\n\r") {
			return manifestError(fmt.Sprintf("extraArgs[%d]", i), "must be a nonempty single argument")
		}
		flag := strings.SplitN(arg, "=", 2)[0]
		if _, protected := protectedFlags[flag]; protected {
			return manifestError(fmt.Sprintf("extraArgs[%d]", i), fmt.Sprintf("cannot override protected flag %s", flag))
		}
	}
	return nil
}

func validateArtifact(field string, artifact Artifact) error {
	if artifact.LocalPath != "" {
		if strings.ContainsRune(artifact.LocalPath, '\x00') {
			return manifestError(field+".localPath", "contains a NUL byte")
		}
		if artifact.Repo != "" || artifact.File != "" || artifact.Quant != "" {
			return manifestError(field, "localPath cannot be combined with repo, file, or quant")
		}
		return nil
	}
	for name, value := range map[string]string{"repo": artifact.Repo, "file": artifact.File, "quant": artifact.Quant} {
		if strings.TrimSpace(value) == "" || strings.ContainsRune(value, '\x00') {
			return manifestError(field+"."+name, "is required and cannot contain a NUL byte")
		}
	}
	return nil
}

func validateSpeculation(spec Speculation) error {
	if spec.Tokens < 0 || (spec.MinTokens != nil && (*spec.MinTokens < 0 || *spec.MinTokens > spec.Tokens)) {
		return manifestError("speculation", "token counts are invalid")
	}
	if spec.MinProbability != nil && !probability(*spec.MinProbability) {
		return manifestError("speculation.minProbability", "must be between zero and one")
	}
	switch spec.Mode {
	case "none":
		return nil
	case "mtp":
		if spec.Tokens <= 0 {
			return manifestError("speculation.tokens", "must be positive")
		}
		return nil
	case "ngram":
		if spec.Tokens <= 0 || spec.NgramMatchTokens <= 0 {
			return manifestError("speculation", "ngram requires positive draft and match token counts")
		}
		return nil
	case "dflash", "dspark":
		if spec.Tokens <= 0 || spec.Draft == nil {
			return manifestError("speculation", "requires a positive token count and draft artifact")
		}
		return validateArtifact("speculation.draft", *spec.Draft)
	default:
		return manifestError("speculation.mode", fmt.Sprintf("unknown mode %q", spec.Mode))
	}
}

func probability(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0 && value <= 1
}

func finiteNonnegative(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0
}

func manifestError(field string, message string) error {
	return &ManifestError{Field: field, Message: message}
}
