package ollamacache

import (
	"encoding/json"
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
