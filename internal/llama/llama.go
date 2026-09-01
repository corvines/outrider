package llama

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/corvines/outrider/internal/manifest"
)

type RunnerError struct {
	Message string
	Cause   error
}

func (e *RunnerError) Error() string {
	return e.Message
}

func (e *RunnerError) Unwrap() error {
	return e.Cause
}

type Downloader func(context.Context, string, string) error

type Platform struct {
	OS   string
	Arch string
}

type EnsureServerOptions struct {
	StateRoot          string
	ExecutableOverride string
	Release            manifest.Release
	Platform           Platform
	Download           Downloader
}

type EnsureModelOptions struct {
	AllowPreset func(string) bool
	Download    Downloader
}

func EnsureServer(ctx context.Context, options EnsureServerOptions) (string, error) {
	if options.ExecutableOverride != "" {
		executable, err := filepath.Abs(options.ExecutableOverride)
		if err != nil {
			return "", err
		}
		if err := assertExecutable(executable, "LLAMA_SERVER_BIN override"); err != nil {
			return "", err
		}
		return executable, nil
	}

	platform := options.Platform
	if platform.OS == "" {
		platform = Platform{OS: runtime.GOOS, Arch: runtime.GOARCH}
	}
	if platform.OS != "darwin" || platform.Arch != "arm64" {
		return "", runnerErrorf(
			"the pinned release is macOS arm64 only; found %s/%s; set LLAMA_SERVER_BIN to an appropriate verified binary",
			platform.OS, platform.Arch,
		)
	}

	release := options.Release
	if release.Tag == "" {
		release = manifest.LlamaRelease
	}
	stateRoot, err := filepath.Abs(options.StateRoot)
	if err != nil {
		return "", err
	}
	releaseParent := filepath.Join(stateRoot, "llama.cpp", release.Tag)
	releaseDirectory := filepath.Join(releaseParent, release.Directory)
	executable := filepath.Join(releaseDirectory, "llama-server")
	if isExecutable(executable) {
		return executable, nil
	}

	archiveDirectory := filepath.Join(stateRoot, "downloads")
	if err := os.MkdirAll(releaseParent, 0o700); err != nil {
		return "", runnerError("could not create runtime directory", err)
	}
	if err := os.MkdirAll(archiveDirectory, 0o700); err != nil {
		return "", runnerError("could not create download directory", err)
	}
	archive := filepath.Join(archiveDirectory, release.Asset)
	if err := ensureArchive(ctx, archive, release, options.Download); err != nil {
		return "", err
	}
	if err := installArchive(archive, releaseParent, releaseDirectory, release); err != nil {
		return "", err
	}
	if err := assertExecutable(executable, "pinned llama-server release"); err != nil {
		return "", err
	}
	return executable, nil
}

func EnsureModelCached(
	ctx context.Context,
	profile manifest.Profile,
	plan manifest.Plan,
	options EnsureModelOptions,
) (string, error) {
	allowed := options.AllowPreset
	if allowed == nil {
		allowed = func(id string) bool { return id == "tiny" }
	}
	if !allowed(profile.ID) {
		return "", runnerErrorf(
			"refusing to download model for plan-only preset %s; only the tiny live proof is enabled",
			profile.ID,
		)
	}
	if profile.Model.SHA256 == "" {
		return "", runnerErrorf("model %s does not declare a SHA-256 digest", profile.ID)
	}

	modelPath, err := filepath.Abs(plan.State.Model)
	if err != nil {
		return "", err
	}
	exists, err := pathExists(modelPath)
	if err != nil {
		return "", err
	}
	if exists {
		valid, err := isValidGGUF(modelPath)
		if err != nil {
			return "", err
		}
		if !valid {
			return "", runnerErrorf("cached model is not a valid GGUF file: %s", modelPath)
		}
		if err := verifySHA256(modelPath, profile.Model.SHA256, "cached model"); err != nil {
			return "", err
		}
		return modelPath, nil
	}

	if err := os.MkdirAll(filepath.Dir(modelPath), 0o700); err != nil {
		return "", runnerError("could not create model cache", err)
	}
	partial := fmt.Sprintf("%s.part-%d", modelPath, os.Getpid())
	if err := os.Remove(partial); err != nil && !os.IsNotExist(err) {
		return "", runnerError("could not clear partial model download", err)
	}
	defer os.Remove(partial)

	download := options.Download
	if download == nil {
		download = DownloadFile
	}
	modelURL, err := ModelDownloadURL(profile)
	if err != nil {
		return "", err
	}
	if err := download(ctx, modelURL, partial); err != nil {
		return "", runnerError(fmt.Sprintf("could not cache model %s", profile.Model.File), err)
	}
	valid, err := isValidGGUF(partial)
	if err != nil {
		return "", err
	}
	if !valid {
		return "", runnerErrorf("downloaded model is not a valid GGUF file: %s", partial)
	}
	if err := verifySHA256(partial, profile.Model.SHA256, "downloaded model"); err != nil {
		return "", err
	}
	if err := os.Rename(partial, modelPath); err != nil {
		return "", runnerError("could not install model in cache", err)
	}
	return modelPath, nil
}

func runnerError(message string, cause error) error {
	return &RunnerError{Message: message + ": " + cause.Error(), Cause: cause}
}

func runnerErrorf(format string, args ...any) error {
	return &RunnerError{Message: fmt.Sprintf(format, args...)}
}
