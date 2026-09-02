package ollamacache

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const modelLayerMediaType = "application/vnd.ollama.image.model"

var digestPattern = regexp.MustCompile(`^sha256:([0-9a-f]{64})$`)

type Model struct {
	Name      string `json:"name"`
	Digest    string `json:"digest"`
	Path      string `json:"path"`
	SizeBytes int64  `json:"sizeBytes"`
}

type VerifyProgress struct {
	Name           string
	Verified       int64
	Total          int64
	BytesPerSecond float64
	ETA            time.Duration
	Done           bool
}

type VerifyProgressFunc func(VerifyProgress)

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

func Find(root string, name string) (Model, bool, error) {
	models, err := Discover(root)
	if err != nil {
		return Model{}, false, err
	}
	for _, model := range models {
		if model.Name == name {
			return model, true, nil
		}
	}
	return Model{}, false, nil
}

func Verify(ctx context.Context, model Model, progress VerifyProgressFunc) error {
	expected := strings.TrimPrefix(model.Digest, "sha256:")
	if len(expected) != sha256.Size*2 || !digestPattern.MatchString(model.Digest) {
		return fmt.Errorf("model %s has an invalid SHA-256 digest", model.Name)
	}
	file, err := os.Open(model.Path)
	if err != nil {
		return fmt.Errorf("open development model %s: %w", model.Name, err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("inspect development model %s: %w", model.Name, err)
	}
	if !info.Mode().IsRegular() || info.Size() != model.SizeBytes {
		return fmt.Errorf("development model %s changed after discovery", model.Name)
	}

	hash := sha256.New()
	buffer := make([]byte, 4<<20)
	startedAt := time.Now()
	lastReport := startedAt
	var verified int64
	report := func(done bool) {
		if progress == nil {
			return
		}
		elapsed := time.Since(startedAt).Seconds()
		rate := float64(0)
		if elapsed > 0 {
			rate = float64(verified) / elapsed
		}
		eta := time.Duration(0)
		if rate > 0 && model.SizeBytes > verified {
			eta = time.Duration(float64(model.SizeBytes-verified)/rate) * time.Second
		}
		progress(VerifyProgress{
			Name: model.Name, Verified: verified, Total: model.SizeBytes,
			BytesPerSecond: rate, ETA: eta, Done: done,
		})
	}
	report(false)
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("verify development model %s: %w", model.Name, err)
		}
		count, readErr := file.Read(buffer)
		if count > 0 {
			_, _ = hash.Write(buffer[:count])
			verified += int64(count)
		}
		if time.Since(lastReport) >= 100*time.Millisecond {
			report(false)
			lastReport = time.Now()
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return fmt.Errorf("verify development model %s: %w", model.Name, readErr)
		}
	}
	report(true)
	actual := fmt.Sprintf("%x", hash.Sum(nil))
	if actual != expected {
		return fmt.Errorf(
			"development model %s failed SHA-256 verification: expected %s, got %s",
			model.Name, expected, actual,
		)
	}
	return nil
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
