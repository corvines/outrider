package ollamacache

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
)

const modelLayerMediaType = "application/vnd.ollama.image.model"

var digestPattern = regexp.MustCompile(`^sha256:([0-9a-f]{64})$`)

type Model struct {
	Name      string `json:"name"`
	Digest    string `json:"digest"`
	Path      string `json:"path"`
	SizeBytes int64  `json:"sizeBytes"`
}

type manifestFile struct {
	Layers []manifestLayer `json:"layers"`
}

type manifestLayer struct {
	MediaType string `json:"mediaType"`
	Digest    string `json:"digest"`
	Size      int64  `json:"size"`
}

func DefaultRoot(home string, override string) (string, error) {
	if override != "" {
		return filepath.Abs(override)
	}
	if home == "" {
		var err error
		home, err = os.UserHomeDir()
		if err != nil {
			return "", err
		}
	}
	return filepath.Abs(filepath.Join(home, ".ollama", "models"))
}

func Discover(root string) ([]Model, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	manifestsRoot := filepath.Join(root, "manifests", "registry.ollama.ai", "library")
	blobsRoot := filepath.Join(root, "blobs")
	families, err := os.ReadDir(manifestsRoot)
	if errors.Is(err, os.ErrNotExist) {
		return []Model{}, nil
	}
	if err != nil {
		return nil, err
	}

	models := make([]Model, 0)
	for _, family := range families {
		if !family.IsDir() {
			continue
		}
		tags, err := os.ReadDir(filepath.Join(manifestsRoot, family.Name()))
		if err != nil {
			continue
		}
		for _, tag := range tags {
			if tag.IsDir() {
				continue
			}
			model, ok := readModel(
				filepath.Join(manifestsRoot, family.Name(), tag.Name()),
				blobsRoot,
				family.Name()+":"+tag.Name(),
			)
			if ok {
				models = append(models, model)
			}
		}
	}
	sort.Slice(models, func(i, j int) bool { return models[i].Name < models[j].Name })
	return models, nil
}

func readModel(manifestPath string, blobsRoot string, name string) (Model, bool) {
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return Model{}, false
	}
	var manifest manifestFile
	if err := json.Unmarshal(data, &manifest); err != nil {
		return Model{}, false
	}
	for _, layer := range manifest.Layers {
		if layer.MediaType != modelLayerMediaType || layer.Size <= 0 {
			continue
		}
		match := digestPattern.FindStringSubmatch(layer.Digest)
		if match == nil {
			return Model{}, false
		}
		blobPath := filepath.Join(blobsRoot, "sha256-"+match[1])
		if !validGGUF(blobPath, layer.Size) {
			return Model{}, false
		}
		return Model{Name: name, Digest: layer.Digest, Path: blobPath, SizeBytes: layer.Size}, true
	}
	return Model{}, false
}

func validGGUF(path string, expectedSize int64) bool {
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() != expectedSize {
		return false
	}
	magic := make([]byte, 4)
	if _, err := io.ReadFull(file, magic); err != nil {
		return false
	}
	return string(magic) == "GGUF"
}
