package llama

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"

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
	Progress           ProgressFunc
}

type EnsureModelOptions struct {
	AllowPreset func(string) bool
	Download    Downloader
	Progress    ProgressFunc
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
	download := options.Download
	if download == nil && options.Progress != nil {
		download = func(ctx context.Context, sourceURL string, destination string) error {
			return DownloadFileWithProgress(ctx, sourceURL, destination, options.Progress)
		}
	}
	if err := ensureArchive(ctx, archive, release, download); err != nil {
		return "", err
	}
	if exists, err := pathExists(releaseDirectory); err != nil {
		return "", err
	} else if exists {
		if err := quarantineReleaseDirectory(releaseDirectory); err != nil {
			return "", err
		}
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
		allowed = func(string) bool { return profile.Runnable }
	}
	if !allowed(profile.ID) {
		return "", runnerErrorf(
			"refusing to download model for plan-only preset %s",
			profile.ID,
		)
	}
	artifacts := profile.Artifacts()
	modelPath := ""
	for _, role := range sortedRoles(artifacts) {
		path, err := ensureArtifact(
			ctx, profile, role, artifacts[role], plan.State.Artifacts[role], options,
		)
		if err != nil {
			return "", err
		}
		if role == manifest.RoleModel {
			modelPath = path
		}
	}
	return modelPath, nil
}

// sortedRoles fixes the fetch order, so a run downloads the same files in the
// same sequence every time and its progress output is reproducible.
func sortedRoles(artifacts map[string]manifest.Artifact) []string {
	roles := make([]string, 0, len(artifacts))
	for role := range artifacts {
		roles = append(roles, role)
	}
	sort.Strings(roles)
	return roles
}

func ensureArtifact(
	ctx context.Context,
	profile manifest.Profile,
	role string,
	artifact manifest.Artifact,
	cachePath string,
	options EnsureModelOptions,
) (string, error) {
	if artifact.SHA256 == "" {
		return "", runnerErrorf("%s for %s does not declare a SHA-256 digest", role, profile.ID)
	}

	cached, err := filepath.Abs(cachePath)
	if err != nil {
		return "", err
	}
	exists, err := pathExists(cached)
	if err != nil {
		return "", err
	}
	if exists {
		valid, err := isValidGGUF(cached)
		if err != nil {
			return "", err
		}
		if !valid {
			cause := runnerErrorf("cached model is not a valid GGUF file: %s", cached)
			return "", quarantineCachedModel(cached, cause)
		}
		if err := verifySHA256WithProgress(
			ctx, cached, artifact.SHA256, "cached "+role, "verify "+profile.ID, options.Progress,
		); err != nil {
			return "", quarantineCachedModel(cached, err)
		}
		return cached, nil
	}

	if err := os.MkdirAll(filepath.Dir(cached), 0o700); err != nil {
		return "", runnerError("could not create model cache", err)
	}
	partial := cached + ".part"

	download := options.Download
	if download == nil {
		download = func(ctx context.Context, sourceURL string, destination string) error {
			return DownloadFileWithProgress(ctx, sourceURL, destination, options.Progress)
		}
	}
	sourceURL, err := ArtifactDownloadURL(artifact, profile.ID, role)
	if err != nil {
		return "", err
	}
	if err := download(ctx, sourceURL, partial); err != nil {
		return "", runnerError(fmt.Sprintf("could not cache %s %s", role, artifact.File), err)
	}
	valid, err := isValidGGUF(partial)
	if err != nil {
		return "", err
	}
	if !valid {
		_ = os.Remove(partial)
		_ = os.Remove(resumeMetadataPath(partial))
		return "", runnerErrorf("downloaded model is not a valid GGUF file: %s", partial)
	}
	if err := verifySHA256WithProgress(
		ctx, partial, artifact.SHA256, "downloaded "+role, "verify "+profile.ID, options.Progress,
	); err != nil {
		_ = os.Remove(partial)
		_ = os.Remove(resumeMetadataPath(partial))
		return "", err
	}
	if err := os.Rename(partial, cached); err != nil {
		return "", runnerError("could not install model in cache", err)
	}
	return cached, nil
}

func quarantineCachedModel(modelPath string, cause error) error {
	quarantine := modelPath + ".corrupt"
	if err := os.Remove(quarantine); err != nil && !os.IsNotExist(err) {
		return runnerError("could not replace prior model quarantine", err)
	}
	if err := os.Rename(modelPath, quarantine); err != nil {
		return runnerError("could not quarantine invalid cached model", err)
	}
	return runnerErrorf("%v; moved invalid cache entry to %s; retry to download a verified copy", cause, quarantine)
}

func runnerError(message string, cause error) error {
	return &RunnerError{Message: message + ": " + cause.Error(), Cause: cause}
}

func runnerErrorf(format string, args ...any) error {
	return &RunnerError{Message: fmt.Sprintf(format, args...)}
}
