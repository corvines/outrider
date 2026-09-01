package llama

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
	profile, err := manifest.Get("tiny")
	if err != nil {
		t.Fatal(err)
	}
	model := []byte("GGUF\x00test")
	profile.Model.SHA256 = bytesSHA256(model)
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
	if downloads != 1 {
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
	if !strings.Contains(modelURL, "/ggml-org/Qwen3.5-0.8B-GGUF/resolve/main/Qwen3.5-0.8B-Q4_0.gguf") {
		t.Fatalf("model URL = %q", modelURL)
	}
}

func TestEnsureModelRejectsMismatchedCachedContent(t *testing.T) {
	root := t.TempDir()
	profile, _ := manifest.Get("tiny")
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
}

func TestEnsureModelRejectsMismatchedDownloadAndCleansPartial(t *testing.T) {
	root := t.TempDir()
	profile, _ := manifest.Get("tiny")
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
	partials, err := filepath.Glob(plan.State.Model + ".part-*")
	if err != nil {
		t.Fatal(err)
	}
	if len(partials) != 0 {
		t.Fatalf("partial downloads remain: %v", partials)
	}
}

func TestEnsureModelRefusesPlanOnlyProfile(t *testing.T) {
	root := t.TempDir()
	profile, _ := manifest.Get("qwen35b-mtp")
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
	if err := DownloadFile(context.Background(), server.URL, destination); err != nil {
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
