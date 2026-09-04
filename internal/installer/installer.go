package installer

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	MarkerSchema = 1
	TargetPath   = "/usr/local/bin/outrider"
	MarkerPath   = "/usr/local/share/outrider/install.json"
	PackageID    = "com.corvines.outrider"
)

// Marker records what Outrider installed. A staged copy is identified by its
// SHA-256; a symlink is identified by its destination. Exactly one of the two
// is set.
type Marker struct {
	Schema int    `json:"schema"`
	Target string `json:"target"`
	SHA256 string `json:"sha256,omitempty"`
	Link   string `json:"link,omitempty"`
}

func Stage(binary string, root string) (Marker, error) {
	binary, err := filepath.Abs(binary)
	if err != nil {
		return Marker{}, err
	}
	info, err := os.Stat(binary)
	if err != nil {
		return Marker{}, fmt.Errorf("inspect package binary: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return Marker{}, fmt.Errorf("package binary is not an executable regular file: %s", binary)
	}
	target, err := rootedPath(root, TargetPath)
	if err != nil {
		return Marker{}, err
	}
	markerPath, err := rootedPath(root, MarkerPath)
	if err != nil {
		return Marker{}, err
	}
	if err := refuseExisting(target, markerPath); err != nil {
		return Marker{}, err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return Marker{}, err
	}
	if err := copyExecutable(binary, target); err != nil {
		return Marker{}, err
	}
	digest, err := fileSHA256(target)
	if err != nil {
		_ = os.Remove(target)
		return Marker{}, err
	}
	marker := Marker{Schema: MarkerSchema, Target: TargetPath, SHA256: digest}
	if err := writeMarker(markerPath, marker); err != nil {
		_ = os.Remove(target)
		return Marker{}, err
	}
	return marker, nil
}

func Uninstall(root string) error {
	markerPath, err := rootedPath(root, MarkerPath)
	if err != nil {
		return err
	}
	marker, err := readMarker(markerPath)
	if err != nil {
		return err
	}
	target, err := rootedPath(root, marker.Target)
	if err != nil {
		return err
	}
	digest, err := fileSHA256(target)
	if err != nil {
		return fmt.Errorf("inspect installed Outrider artifact: %w", err)
	}
	if !strings.EqualFold(digest, marker.SHA256) {
		return fmt.Errorf(
			"refusing to remove %s: installed SHA-256 is %s, ownership marker requires %s",
			marker.Target, digest, marker.SHA256,
		)
	}
	if err := os.Remove(target); err != nil {
		return fmt.Errorf("remove installed Outrider artifact: %w", err)
	}
	if err := os.Remove(markerPath); err != nil {
		return fmt.Errorf("remove Outrider ownership marker: %w", err)
	}
	removeEmptyParents(filepath.Dir(markerPath), root)
	removeEmptyParents(filepath.Dir(target), root)
	return nil
}

func Verify(root string) (Marker, error) {
	markerPath, err := rootedPath(root, MarkerPath)
	if err != nil {
		return Marker{}, err
	}
	marker, err := readMarker(markerPath)
	if err != nil {
		return Marker{}, err
	}
	target, err := rootedPath(root, marker.Target)
	if err != nil {
		return Marker{}, err
	}
	digest, err := fileSHA256(target)
	if err != nil {
		return Marker{}, err
	}
	if !strings.EqualFold(digest, marker.SHA256) {
		return Marker{}, fmt.Errorf("installed artifact SHA-256 %s does not match marker %s", digest, marker.SHA256)
	}
	return marker, nil
}

func rootedPath(root string, target string) (string, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(target) {
		return "", fmt.Errorf("install target must be absolute: %s", target)
	}
	resolved := filepath.Join(root, strings.TrimPrefix(filepath.Clean(target), string(filepath.Separator)))
	relative, err := filepath.Rel(root, resolved)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("install target escapes root: %s", target)
	}
	return resolved, nil
}

func refuseExisting(target string, marker string) error {
	for _, path := range []string{target, marker} {
		if _, err := os.Lstat(path); err == nil {
			return fmt.Errorf("refusing to overwrite existing package root entry: %s", path)
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func copyExecutable(source string, target string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	file, err := os.CreateTemp(filepath.Dir(target), ".outrider-")
	if err != nil {
		return err
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	if err := file.Chmod(0o755); err != nil {
		file.Close()
		return err
	}
	if _, err := io.Copy(file, input); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(temporary, target)
}

func writeMarker(path string, marker Marker) error {
	data, err := json.MarshalIndent(marker, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".install-")
	if err != nil {
		return err
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	if err := file.Chmod(0o644); err != nil {
		file.Close()
		return err
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}

func readMarker(path string) (Marker, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Marker{}, fmt.Errorf("read Outrider ownership marker: %w", err)
	}
	var marker Marker
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&marker); err != nil {
		return Marker{}, fmt.Errorf("decode Outrider ownership marker: %w", err)
	}
	staged := len(marker.SHA256) == 64
	linked := marker.Link != ""
	if marker.Schema != MarkerSchema || marker.Target == "" || staged == linked {
		return Marker{}, fmt.Errorf("invalid Outrider ownership marker at %s", path)
	}
	return marker, nil
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func removeEmptyParents(directory string, root string) {
	root, err := filepath.Abs(root)
	if err != nil {
		return
	}
	for directory != root && strings.HasPrefix(directory, root+string(filepath.Separator)) {
		if err := os.Remove(directory); err != nil {
			return
		}
		directory = filepath.Dir(directory)
	}
}
