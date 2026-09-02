package manifest

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

func BuildServerArgs(profile Profile, options BuildOptions) ([]string, error) {
	if err := Validate(profile); err != nil {
		return nil, err
	}
	if options.Port <= 0 || options.Port > 65535 {
		return nil, manifestError("port", "must be between 1 and 65535")
	}
	cwd := options.CWD
	if cwd == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			return nil, err
		}
	}
	args := []string{
		"--host", DefaultHost,
		"--port", strconv.Itoa(options.Port),
		"--alias", profile.ID,
		"--cors-origins", DeniedBrowserOrigin,
		"--no-cors-credentials",
	}
	modelArgs, err := artifactArgs(profile.Model, cwd, "--model")
	if err != nil {
		return nil, err
	}
	args = append(args, modelArgs...)
	args = append(args, "--ctx-size", strconv.Itoa(profile.Context.Size))
	switch profile.GPULayers.Mode {
	case "all", "auto":
		args = append(args, "--n-gpu-layers", profile.GPULayers.Mode)
	default:
		args = append(args, "--n-gpu-layers", strconv.Itoa(profile.GPULayers.Count))
	}
	args = append(args, "--fit", profile.Fit)
	args = append(args, "--flash-attn", onOff(profile.FlashAttention))
	args = append(args, "--cache-type-k", profile.KVCache.KeyType, "--cache-type-v", profile.KVCache.ValueType)
	args = boolFlag(args, profile.KVCache.Unified, "--kv-unified", "--no-kv-unified")
	args = append(args,
		"--batch-size", strconv.Itoa(profile.Batch.Size),
		"--ubatch-size", strconv.Itoa(profile.Batch.MicroSize),
		"--parallel", strconv.Itoa(profile.Batch.Parallel),
	)
	args = boolFlag(args, profile.Memory.Mmap, "--mmap", "--no-mmap")
	if profile.Memory.Mlock {
		args = append(args, "--mlock")
	}
	if profile.Memory.CacheRAMMiB != nil {
		args = append(args, "--cache-ram", strconv.Itoa(*profile.Memory.CacheRAMMiB))
	}
	if profile.Memory.ContextCheckpoints != nil {
		args = append(args, "--ctx-checkpoints", strconv.Itoa(*profile.Memory.ContextCheckpoints))
	}
	if profile.Memory.CheckpointMinStep != nil {
		args = append(args, "--checkpoint-min-step", strconv.Itoa(*profile.Memory.CheckpointMinStep))
	}
	args = boolFlag(args, profile.Jinja, "--jinja", "--no-jinja")
	if profile.ChatTemplate != "" {
		args = append(args, "--chat-template", profile.ChatTemplate)
	}
	if profile.ChatTemplateFile != "" {
		path, err := filepath.Abs(filepath.Join(cwd, profile.ChatTemplateFile))
		if err != nil {
			return nil, err
		}
		args = append(args, "--chat-template-file", path)
	}
	if profile.MultimodalProject != nil {
		path, err := artifactPath(*profile.MultimodalProject, cwd)
		if err != nil {
			return nil, err
		}
		args = append(args, "--mmproj", path)
	}
	args = append(args,
		"--temp", formatFloat(profile.Sampling.Temperature),
		"--top-p", formatFloat(profile.Sampling.TopP),
		"--top-k", strconv.Itoa(profile.Sampling.TopK),
		"--min-p", formatFloat(profile.Sampling.MinP),
		"--repeat-penalty", formatFloat(profile.Sampling.RepeatPenalty),
	)
	if profile.Persistence.Enabled {
		if options.SlotSavePath == "" {
			return nil, manifestError("persistence", "slot save path is required")
		}
		slotPath, err := filepath.Abs(options.SlotSavePath)
		if err != nil {
			return nil, err
		}
		args = append(args, "--slots", "--slot-save-path", slotPath)
	}
	args, err = appendSpeculation(args, profile.Speculation, cwd)
	if err != nil {
		return nil, err
	}
	return append(args, profile.ExtraArgs...), nil
}

func Resolve(profile Profile, options ResolveOptions) (Plan, error) {
	root, err := resolveRoot(options.Root)
	if err != nil {
		return Plan{}, err
	}
	port := DefaultPort
	if options.Port != nil {
		port = *options.Port
	}
	executable := options.Executable
	if executable == "" {
		executable = os.Getenv("LLAMA_SERVER_BIN")
	}
	if executable == "" {
		executable = filepath.Join(root, "llama.cpp", LlamaRelease.Tag, LlamaRelease.Directory, "llama-server")
	}
	if !filepath.IsAbs(executable) {
		executable, err = filepath.Abs(executable)
		if err != nil {
			return Plan{}, err
		}
	}
	state, err := Paths(root, profile, options.CWD)
	if err != nil {
		return Plan{}, err
	}
	args, err := BuildServerArgs(profile, BuildOptions{
		Port: port, CWD: options.CWD, SlotSavePath: state.Slots,
	})
	if err != nil {
		return Plan{}, err
	}
	session := SessionState{Enabled: profile.Persistence.Enabled, Slot: 0}
	if session.Enabled {
		session.Key, err = SessionKey(profile)
		if err != nil {
			return Plan{}, err
		}
		session.Filename = "slot-" + session.Key + ".bin"
	}
	return Plan{
		Profile: profile, Host: DefaultHost, Port: port,
		Endpoint:       "http://" + DefaultHost + ":" + strconv.Itoa(port),
		HealthEndpoint: "http://" + DefaultHost + ":" + strconv.Itoa(port) + "/health",
		Executable:     executable, Args: args, State: state, Session: session,
	}, nil
}

func ResolveCached(profile Profile, options ResolveOptions) (Plan, error) {
	state, err := Paths(options.Root, profile, options.CWD)
	if err != nil {
		return Plan{}, err
	}
	profile.Model.LocalPath = state.Model
	return Resolve(profile, options)
}

func Paths(root string, profile Profile, cwd string) (StatePaths, error) {
	root, err := resolveRoot(root)
	if err != nil {
		return StatePaths{}, err
	}
	models := filepath.Join(root, "models")
	model, err := artifactPath(profile.Model, cwd)
	if err != nil {
		return StatePaths{}, err
	}
	if profile.Model.LocalPath == "" {
		model = filepath.Join(models, profile.ID+"-"+profile.Model.File)
	}
	run := filepath.Join(root, "runs", profile.ID)
	runs := filepath.Join(root, "runs")
	slots := filepath.Join(root, "sessions")
	return StatePaths{
		Root: root, Models: models, Model: model, Run: run,
		PID: filepath.Join(runs, "active.json"), Lock: filepath.Join(runs, "lifecycle.lock"),
		Log:        filepath.Join(run, "server.log"),
		Executable: filepath.Join(root, "llama.cpp", LlamaRelease.Tag, LlamaRelease.Directory, "llama-server"),
		Slots:      slots,
	}, nil
}

func DefaultRunnerHome() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "Caches", "Outrider"), nil
}

func resolveRoot(root string) (string, error) {
	if root == "" {
		root = os.Getenv("OUTRIDER_HOME")
	}
	if root == "" {
		var err error
		root, err = DefaultRunnerHome()
		if err != nil {
			return "", err
		}
	}
	return filepath.Abs(root)
}

func artifactArgs(artifact Artifact, cwd string, localFlag string) ([]string, error) {
	if artifact.LocalPath != "" {
		path, err := artifactPath(artifact, cwd)
		if err != nil {
			return nil, err
		}
		return []string{localFlag, path}, nil
	}
	return []string{"--hf-repo", artifact.Repo + ":" + artifact.Quant, "--hf-file", artifact.File}, nil
}

func artifactPath(artifact Artifact, cwd string) (string, error) {
	if artifact.LocalPath == "" {
		return artifact.File, nil
	}
	if filepath.IsAbs(artifact.LocalPath) {
		return filepath.Clean(artifact.LocalPath), nil
	}
	if cwd == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			return "", err
		}
	}
	return filepath.Abs(filepath.Join(cwd, artifact.LocalPath))
}

func appendSpeculation(args []string, spec Speculation, cwd string) ([]string, error) {
	switch spec.Mode {
	case "none":
		return append(args, "--spec-type", "none"), nil
	case "mtp":
		return appendSpecOptions(append(args, "--spec-type", "draft-mtp"), spec), nil
	case "ngram":
		return append(args,
			"--spec-type", "ngram-mod",
			"--spec-ngram-mod-n-max", strconv.Itoa(spec.Tokens),
			"--spec-ngram-mod-n-match", strconv.Itoa(spec.NgramMatchTokens),
		), nil
	case "dflash", "dspark":
		draftArgs, err := draftArtifactArgs(*spec.Draft, cwd)
		if err != nil {
			return nil, err
		}
		args = append(args, "--spec-type", "draft-"+spec.Mode)
		return appendSpecOptions(append(args, draftArgs...), spec), nil
	default:
		return nil, manifestError("speculation.mode", fmt.Sprintf("unknown mode %q", spec.Mode))
	}
}

func appendSpecOptions(args []string, spec Speculation) []string {
	args = append(args, "--spec-draft-n-max", strconv.Itoa(spec.Tokens))
	if spec.MinTokens != nil {
		args = append(args, "--spec-draft-n-min", strconv.Itoa(*spec.MinTokens))
	}
	if spec.MinProbability != nil {
		args = append(args, "--spec-draft-p-min", formatFloat(*spec.MinProbability))
	}
	return args
}

func boolFlag(args []string, value bool, positive string, negative string) []string {
	if value {
		return append(args, positive)
	}
	return append(args, negative)
}

func onOff(value bool) string {
	if value {
		return "on"
	}
	return "off"
}

func draftArtifactArgs(artifact Artifact, cwd string) ([]string, error) {
	if artifact.LocalPath != "" {
		path, err := artifactPath(artifact, cwd)
		if err != nil {
			return nil, err
		}
		return []string{"--spec-draft-model", path}, nil
	}
	return []string{"--spec-draft-hf", artifact.Repo + ":" + artifact.Quant}, nil
}

func formatFloat(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}
