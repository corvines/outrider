package installer

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/corvines/outrider/internal/guide"
)

// UserGuideRelative and GuidePath sit beside the two install markers.
const (
	UserGuideRelative = ".local/share/outrider/" + guide.Filename
	GuidePath         = guide.SystemPath
)

// FindGuide locates the documentation shipped with a binary. A release archive
// puts it beside the binary; a repository build leaves it under docs/. It
// returns an empty string when neither holds.
func FindGuide(binary string) string {
	directory := filepath.Dir(binary)
	for _, candidate := range []string{
		filepath.Join(directory, guide.Filename),
		filepath.Join(directory, "docs", guide.Filename),
		filepath.Join(filepath.Dir(directory), "docs", guide.Filename),
	} {
		if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() {
			return candidate
		}
	}
	return ""
}

// placeGuide copies the shipped documentation to destination. A binary that
// ships without it installs anyway: help mode is the only caller and it
// reports the absence itself.
func placeGuide(binary string, destination string) error {
	source := FindGuide(binary)
	if source == "" {
		return nil
	}
	contents, err := os.ReadFile(source)
	if err != nil {
		return fmt.Errorf("read shipped guide %s: %w", source, err)
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(destination, contents, 0o644); err != nil {
		return fmt.Errorf("write guide %s: %w", destination, err)
	}
	return nil
}
