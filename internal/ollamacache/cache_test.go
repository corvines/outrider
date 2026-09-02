package ollamacache

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverFindsGGUFModelLayers(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "granite4.2", "8b", "a", []byte("GGUFmodel"))

	models, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 {
		t.Fatalf("models = %#v", models)
	}
	if models[0].Name != "granite4.2:8b" || models[0].SizeBytes != 9 {
		t.Fatalf("model = %#v", models[0])
	}
	if models[0].Digest != "sha256:"+repeat("a", 64) {
		t.Fatalf("digest = %q", models[0].Digest)
	}
}

func TestDiscoverReadsSamplingParameters(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "qwen3", "1.7b", "a", []byte("GGUFmodel"))
	manifestPath := filepath.Join(root, "manifests", "registry.ollama.ai", "library", "qwen3", "1.7b")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var manifest manifestFile
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	parameters := []byte(`{"temperature":0.6,"top_k":20,"top_p":0.95,"repeat_penalty":1}`)
	digest := fmt.Sprintf("%x", sha256.Sum256(parameters))
	if err := os.WriteFile(filepath.Join(root, "blobs", "sha256-"+digest), parameters, 0o600); err != nil {
		t.Fatal(err)
	}
	manifest.Layers = append(manifest.Layers, manifestLayer{
		MediaType: parametersLayerMediaType, Digest: "sha256:" + digest, Size: int64(len(parameters)),
	})
	data, err = json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, data, 0o600); err != nil {
		t.Fatal(err)
	}

	models, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	parametersFound := models[0].Parameters
	if parametersFound == nil || parametersFound.Temperature == nil || *parametersFound.Temperature != 0.6 ||
		parametersFound.TopK == nil || *parametersFound.TopK != 20 {
		t.Fatalf("parameters = %#v", parametersFound)
	}
}

func TestDiscoverIgnoresMalformedAndNonGGUFModels(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "qwen3.5", "0.8b-mlx", "b", []byte("MLX-data"))
	manifestPath := filepath.Join(root, "manifests", "registry.ollama.ai", "library", "broken", "latest")
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}

	models, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 0 {
		t.Fatalf("models = %#v", models)
	}
}

func TestDiscoverReturnsEmptyWhenCacheIsAbsent(t *testing.T) {
	models, err := Discover(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 0 {
		t.Fatalf("models = %#v", models)
	}
}

func TestDefaultRootHonorsOverride(t *testing.T) {
	root, err := DefaultRoot("/home/example", "/tmp/ollama-models")
	if err != nil {
		t.Fatal(err)
	}
	if root != "/tmp/ollama-models" {
		t.Fatalf("root = %q", root)
	}
}

func TestVerifyChecksContentDigest(t *testing.T) {
	content := []byte("GGUFmodel")
	digest := fmt.Sprintf("%x", sha256.Sum256(content))
	path := filepath.Join(t.TempDir(), "model.gguf")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	model := Model{Name: "test:model", Digest: "sha256:" + digest, Path: path, SizeBytes: int64(len(content))}
	var final VerifyProgress
	if err := Verify(context.Background(), model, func(progress VerifyProgress) { final = progress }); err != nil {
		t.Fatal(err)
	}
	if !final.Done || final.Verified != int64(len(content)) {
		t.Fatalf("progress = %#v", final)
	}

	model.Digest = "sha256:" + repeat("f", 64)
	if err := Verify(context.Background(), model, nil); err == nil {
		t.Fatal("digest mismatch was accepted")
	}
}

func writeFixture(t *testing.T, root string, family string, tag string, digestCharacter string, model []byte) {
	t.Helper()
	digest := repeat(digestCharacter, 64)
	blobs := filepath.Join(root, "blobs")
	manifestPath := filepath.Join(root, "manifests", "registry.ollama.ai", "library", family, tag)
	if err := os.MkdirAll(blobs, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(blobs, "sha256-"+digest), model, 0o600); err != nil {
		t.Fatal(err)
	}
	payload := manifestFile{Layers: []manifestLayer{{
		MediaType: modelLayerMediaType, Digest: "sha256:" + digest, Size: int64(len(model)),
	}}}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func repeat(value string, count int) string {
	result := ""
	for range count {
		result += value
	}
	return result
}
