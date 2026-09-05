package llama

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/corvines/outrider/internal/manifest"
)

func TestEnsureServerUsesExecutableOverride(t *testing.T) {
	root := t.TempDir()
	executable := filepath.Join(root, "llama-server")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := EnsureServer(context.Background(), EnsureServerOptions{
		StateRoot: root, ExecutableOverride: executable,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != executable {
		t.Fatalf("executable = %q", got)
	}
}

func TestEnsureModelDownloadsOnceAndVerifiesCache(t *testing.T) {
	root := t.TempDir()
	profile, err := manifest.Get("qwen35-0.8b")
	if err != nil {
		t.Fatal(err)
	}
	model := []byte("GGUF\x00test")
	profile.Model.SHA256 = bytesSHA256(model)
	profile.MultimodalProject.SHA256 = bytesSHA256(model)
	plan, err := manifest.Resolve(profile, manifest.ResolveOptions{Root: root, Executable: "/fake/llama-server"})
	if err != nil {
		t.Fatal(err)
	}
	downloads := 0
	download := func(_ context.Context, _ string, destination string) error {
		downloads++
		return os.WriteFile(destination, model, 0o600)
	}
	for range 2 {
		got, err := EnsureModelCached(context.Background(), profile, plan, EnsureModelOptions{Download: download})
		if err != nil {
			t.Fatal(err)
		}
		if got != plan.State.Model {
			t.Fatalf("model path = %q", got)
		}
	}
	if downloads != 2 {
		t.Fatalf("downloads = %d", downloads)
	}
	contents, err := os.ReadFile(plan.State.Model)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents[:4]) != "GGUF" {
		t.Fatalf("model header = %q", contents[:4])
	}
	modelURL, err := ModelDownloadURL(profile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(modelURL, "/unsloth/Qwen3.5-0.8B-MTP-GGUF/resolve/main/Qwen3.5-0.8B-Q4_K_M.gguf") {
		t.Fatalf("model URL = %q", modelURL)
	}
}

func TestEnsureModelRejectsMismatchedCachedContent(t *testing.T) {
	root := t.TempDir()
	profile, _ := manifest.Get("qwen35-0.8b")
	plan, err := manifest.Resolve(profile, manifest.ResolveOptions{Root: root, Executable: "/fake/llama-server"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(plan.State.Models, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(plan.State.Model, []byte("GGUFwrong model"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = EnsureModelCached(context.Background(), profile, plan, EnsureModelOptions{
		Download: func(context.Context, string, string) error {
			t.Fatal("download must not replace a mismatched cache entry")
			return nil
		},
	})
	if err == nil || !strings.Contains(err.Error(), "cached model checksum mismatch") {
		t.Fatalf("error = %v", err)
	}
	if _, statErr := os.Stat(plan.State.Model); !os.IsNotExist(statErr) {
		t.Fatalf("invalid cache entry remains: %v", statErr)
	}
	if _, statErr := os.Stat(plan.State.Model + ".corrupt"); statErr != nil {
		t.Fatalf("model quarantine is missing: %v", statErr)
	}
}

func TestVerifySHA256ReportsProgress(t *testing.T) {
	path := filepath.Join(t.TempDir(), "model.gguf")
	payload := []byte("GGUFmodel")
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	expected := sha256.Sum256(payload)
	var events []DownloadProgress
	if err := verifySHA256WithProgress(
		context.Background(), path, hex.EncodeToString(expected[:]), "cached model", "verify tiny",
		func(progress DownloadProgress) { events = append(events, progress) },
	); err != nil {
		t.Fatal(err)
	}
	last := events[len(events)-1]
	if last.Name != "verify tiny" || last.Downloaded != int64(len(payload)) || last.Total != int64(len(payload)) || !last.Done {
		t.Fatalf("last progress = %#v", last)
	}
}

func TestEnsureModelRejectsMismatchedDownloadAndCleansPartial(t *testing.T) {
	root := t.TempDir()
	profile, _ := manifest.Get("qwen35-0.8b")
	plan, err := manifest.Resolve(profile, manifest.ResolveOptions{Root: root, Executable: "/fake/llama-server"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = EnsureModelCached(context.Background(), profile, plan, EnsureModelOptions{
		Download: func(_ context.Context, _ string, destination string) error {
			return os.WriteFile(destination, []byte("GGUFwrong model"), 0o600)
		},
	})
	if err == nil || !strings.Contains(err.Error(), "downloaded model checksum mismatch") {
		t.Fatalf("error = %v", err)
	}
	if _, statErr := os.Stat(plan.State.Model + ".part"); !os.IsNotExist(statErr) {
		t.Fatalf("corrupt partial remains: %v", statErr)
	}
}

func TestEnsureModelRefusesPlanOnlyProfile(t *testing.T) {
	root := t.TempDir()
	profile, _ := manifest.Get("qwen35b-mtp")
	profile.Runnable = false
	plan, err := manifest.Resolve(profile, manifest.ResolveOptions{Root: root, Executable: "/fake/llama-server"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = EnsureModelCached(context.Background(), profile, plan, EnsureModelOptions{
		Download: func(context.Context, string, string) error {
			t.Fatal("plan-only profile must not download")
			return nil
		},
	})
	if err == nil || !strings.Contains(err.Error(), "plan-only preset") {
		t.Fatalf("error = %v", err)
	}
}

func TestEnsureServerInstallsVerifiedArchiveOnce(t *testing.T) {
	root := t.TempDir()
	archive := filepath.Join(t.TempDir(), "runtime.tar.gz")
	writeRuntimeArchive(t, archive, []tarEntry{
		{name: "test-release/", kind: tar.TypeDir, mode: 0o755},
		{name: "test-release/llama-server", body: "#!/bin/sh\nexit 0\n", mode: 0o755},
		{name: "test-release/libcurrent.dylib", link: "libversion.dylib", kind: tar.TypeSymlink},
		{name: "test-release/libversion.dylib", body: "library", mode: 0o644},
	})
	release := manifest.Release{
		Tag: "test", Asset: "runtime.tar.gz", Directory: "test-release",
		SHA256: fileSHA256(t, archive), URL: "https://example.invalid/runtime.tar.gz",
	}
	downloads := 0
	download := func(_ context.Context, _ string, destination string) error {
		downloads++
		return copyFile(archive, destination)
	}
	options := EnsureServerOptions{
		StateRoot: root, Release: release, Platform: Platform{OS: "darwin", Arch: "arm64"}, Download: download,
	}
	var executable string
	for range 2 {
		var err error
		executable, err = EnsureServer(context.Background(), options)
		if err != nil {
			t.Fatal(err)
		}
	}
	if downloads != 1 {
		t.Fatalf("downloads = %d", downloads)
	}
	if err := assertExecutable(executable, "test server"); err != nil {
		t.Fatal(err)
	}
	target, err := os.Readlink(filepath.Join(filepath.Dir(executable), "libcurrent.dylib"))
	if err != nil {
		t.Fatal(err)
	}
	if target != "libversion.dylib" {
		t.Fatalf("symlink target = %q", target)
	}
}

func TestEnsureServerRejectsArchiveTraversal(t *testing.T) {
	root := t.TempDir()
	archive := filepath.Join(t.TempDir(), "runtime.tar.gz")
	writeRuntimeArchive(t, archive, []tarEntry{
		{name: "test-release/llama-server", body: "server", mode: 0o755},
		{name: "test-release/../../escape", body: "bad", mode: 0o644},
	})
	release := manifest.Release{
		Tag: "test", Asset: "runtime.tar.gz", Directory: "test-release",
		SHA256: fileSHA256(t, archive), URL: "https://example.invalid/runtime.tar.gz",
	}
	_, err := EnsureServer(context.Background(), EnsureServerOptions{
		StateRoot: root, Release: release, Platform: Platform{OS: "darwin", Arch: "arm64"},
		Download: func(_ context.Context, _ string, destination string) error {
			return copyFile(archive, destination)
		},
	})
	if err == nil || !strings.Contains(err.Error(), "path traversal") {
		t.Fatalf("error = %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "escape")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("escape file stat error = %v", statErr)
	}
}

func TestEnsureServerRepairsUnusableInstalledRuntime(t *testing.T) {
	root := t.TempDir()
	archive := filepath.Join(t.TempDir(), "runtime.tar.gz")
	writeRuntimeArchive(t, archive, []tarEntry{
		{name: "test-release/", kind: tar.TypeDir, mode: 0o755},
		{name: "test-release/llama-server", body: "#!/bin/sh\nexit 0\n", mode: 0o755},
	})
	release := manifest.Release{
		Tag: "test", Asset: "runtime.tar.gz", Directory: "test-release",
		SHA256: fileSHA256(t, archive), URL: "https://example.invalid/runtime.tar.gz",
	}
	downloads := 0
	options := EnsureServerOptions{
		StateRoot: root, Release: release, Platform: Platform{OS: "darwin", Arch: "arm64"},
		Download: func(_ context.Context, _ string, destination string) error {
			downloads++
			return copyFile(archive, destination)
		},
	}
	executable, err := EnsureServer(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(executable); err != nil {
		t.Fatal(err)
	}
	repaired, err := EnsureServer(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if repaired != executable || downloads != 1 {
		t.Fatalf("repaired = %q, downloads = %d", repaired, downloads)
	}
	if _, err := os.Stat(filepath.Dir(executable) + ".corrupt"); err != nil {
		t.Fatalf("runtime quarantine is missing: %v", err)
	}
}

func TestCachedPinnedArchive(t *testing.T) {
	archive := os.Getenv("OUTRIDER_TEST_ARCHIVE")
	if archive == "" {
		t.Skip("OUTRIDER_TEST_ARCHIVE is not set")
	}
	if err := verifySHA256(archive, manifest.LlamaRelease.SHA256, "cached test archive"); err != nil {
		t.Fatal(err)
	}
	releaseParent := filepath.Join(t.TempDir(), manifest.LlamaRelease.Tag)
	if err := os.MkdirAll(releaseParent, 0o700); err != nil {
		t.Fatal(err)
	}
	releaseDirectory := filepath.Join(releaseParent, manifest.LlamaRelease.Directory)
	if err := installArchive(archive, releaseParent, releaseDirectory, manifest.LlamaRelease); err != nil {
		t.Fatal(err)
	}
	if err := assertExecutable(filepath.Join(releaseDirectory, "llama-server"), "extracted test server"); err != nil {
		t.Fatal(err)
	}
}

func TestDownloadRetriesTransientResponses(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		attempts++
		if attempts < 3 {
			response.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = response.Write([]byte("complete"))
	}))
	defer server.Close()

	destination := filepath.Join(t.TempDir(), "download")
	progress := make([]DownloadProgress, 0)
	if err := DownloadFileWithProgress(context.Background(), server.URL, destination, func(update DownloadProgress) {
		progress = append(progress, update)
	}); err != nil {
		t.Fatal(err)
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d", attempts)
	}
	contents, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "complete" {
		t.Fatalf("download = %q", contents)
	}
	last := progress[len(progress)-1]
	if !last.Done || last.Downloaded != int64(len(contents)) || last.Total != int64(len(contents)) {
		t.Fatalf("last progress = %#v", last)
	}
}

func TestDownloadPreservesAndResumesInterruptedPartial(t *testing.T) {
	payload := bytes.Repeat([]byte("outrider-resume-"), 32768)
	var cancel context.CancelFunc
	requests := 0
	resumedRange := ""
	resumedValidator := ""
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests++
		response.Header().Set("ETag", `"artifact-v1"`)
		if request.Header.Get("Range") == "" {
			response.Header().Set("Content-Length", strconv.Itoa(len(payload)))
			_, _ = response.Write(payload[:len(payload)/2])
			if flusher, ok := response.(http.Flusher); ok {
				flusher.Flush()
			}
			time.Sleep(25 * time.Millisecond)
			cancel()
			return
		}
		resumedRange = request.Header.Get("Range")
		resumedValidator = request.Header.Get("If-Range")
		startText := strings.TrimSuffix(strings.TrimPrefix(resumedRange, "bytes="), "-")
		start, err := strconv.Atoi(startText)
		if err != nil || start <= 0 || start >= len(payload) {
			http.Error(response, "invalid range", http.StatusBadRequest)
			return
		}
		response.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, len(payload)-1, len(payload)))
		response.Header().Set("Content-Length", strconv.Itoa(len(payload)-start))
		response.WriteHeader(http.StatusPartialContent)
		_, _ = response.Write(payload[start:])
	}))
	defer server.Close()

	destination := filepath.Join(t.TempDir(), "artifact.part")
	ctx, stop := context.WithCancel(context.Background())
	cancel = stop
	if err := DownloadFile(ctx, server.URL, destination); !errors.Is(err, context.Canceled) {
		t.Fatalf("interrupted download error = %v", err)
	}
	partial, err := os.Stat(destination)
	if err != nil {
		t.Fatal(err)
	}
	if partial.Size() <= 0 || partial.Size() >= int64(len(payload)) {
		t.Fatalf("partial size = %d", partial.Size())
	}
	if err := DownloadFile(context.Background(), server.URL, destination); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("resumed payload length = %d", len(got))
	}
	if requests != 2 || resumedRange == "" || resumedValidator != `"artifact-v1"` {
		t.Fatalf("requests = %d, range = %q, validator = %q", requests, resumedRange, resumedValidator)
	}
	if _, err := os.Stat(resumeMetadataPath(destination)); !os.IsNotExist(err) {
		t.Fatalf("resume metadata remains: %v", err)
	}
}

type tarEntry struct {
	name string
	body string
	link string
	kind byte
	mode int64
}

func writeRuntimeArchive(t *testing.T, destination string, entries []tarEntry) {
	t.Helper()
	file, err := os.Create(destination)
	if err != nil {
		t.Fatal(err)
	}
	gzipWriter := gzip.NewWriter(file)
	tarWriter := tar.NewWriter(gzipWriter)
	for _, entry := range entries {
		kind := entry.kind
		if kind == 0 {
			kind = tar.TypeReg
		}
		header := &tar.Header{
			Name: entry.name, Mode: entry.mode, Typeflag: kind,
			Size: int64(len(entry.body)), Linkname: entry.link,
		}
		if kind != tar.TypeReg {
			header.Size = 0
		}
		if err := tarWriter.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if header.Size > 0 {
			if _, err := io.WriteString(tarWriter, entry.body); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func copyFile(source string, destination string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(output, input); err != nil {
		output.Close()
		return err
	}
	return output.Close()
}

func fileSHA256(t *testing.T, path string) string {
	t.Helper()
	digest, err := sha256File(path)
	if err != nil {
		t.Fatal(err)
	}
	return digest
}

func bytesSHA256(value []byte) string {
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:])
}

// A profile needing a projector needs both files on disk, so the fetch runs
// per artifact rather than once for the model.
func TestEnsureModelCachesEveryArtifact(t *testing.T) {
	root := t.TempDir()
	profile, err := manifest.Get("qwen35-0.8b")
	if err != nil {
		t.Fatal(err)
	}
	model := []byte("GGUF\x00model")
	projector := []byte("GGUF\x00projector")
	profile.Model.SHA256 = bytesSHA256(model)
	profile.MultimodalProject = &manifest.Artifact{
		Repo: "org/model", File: "mmproj-F16.gguf", Quant: "F16",
		SHA256: bytesSHA256(projector), SizeBytes: int64(len(projector)),
	}
	plan, err := manifest.Resolve(profile, manifest.ResolveOptions{Root: root, Executable: "/fake/llama-server"})
	if err != nil {
		t.Fatal(err)
	}
	fetched := map[string]int{}
	download := func(_ context.Context, sourceURL string, destination string) error {
		payload := model
		if strings.Contains(sourceURL, "mmproj") {
			payload = projector
			fetched["projector"]++
		} else {
			fetched["model"]++
		}
		return os.WriteFile(destination, payload, 0o600)
	}
	for range 2 {
		got, err := EnsureModelCached(context.Background(), profile, plan, EnsureModelOptions{Download: download})
		if err != nil {
			t.Fatal(err)
		}
		if got != plan.State.Model {
			t.Fatalf("returned path = %q, want the model", got)
		}
	}
	if fetched["model"] != 1 || fetched["projector"] != 1 {
		t.Fatalf("fetched = %v", fetched)
	}
	projectorPath := plan.State.Artifacts[manifest.RoleProjector]
	contents, err := os.ReadFile(projectorPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(contents, projector) {
		t.Fatalf("projector contents = %q", contents)
	}
	if projectorPath == plan.State.Model {
		t.Fatal("the projector shares the model's cache path")
	}
}

// A projector that fails verification fails the run. It is not treated as an
// optional extra just because the model itself arrived intact.
func TestEnsureModelRejectsAMismatchedProjector(t *testing.T) {
	root := t.TempDir()
	profile, _ := manifest.Get("qwen35-0.8b")
	model := []byte("GGUF\x00model")
	profile.Model.SHA256 = bytesSHA256(model)
	profile.MultimodalProject = &manifest.Artifact{
		Repo: "org/model", File: "mmproj-F16.gguf", Quant: "F16",
		SHA256: bytesSHA256([]byte("GGUF\x00expected")), SizeBytes: 16,
	}
	plan, err := manifest.Resolve(profile, manifest.ResolveOptions{Root: root, Executable: "/fake/llama-server"})
	if err != nil {
		t.Fatal(err)
	}
	download := func(_ context.Context, sourceURL string, destination string) error {
		if strings.Contains(sourceURL, "mmproj") {
			return os.WriteFile(destination, []byte("GGUF\x00wrong"), 0o600)
		}
		return os.WriteFile(destination, model, 0o600)
	}
	if _, err := EnsureModelCached(
		context.Background(), profile, plan, EnsureModelOptions{Download: download},
	); err == nil {
		t.Fatal("expected a verification failure")
	}
}
