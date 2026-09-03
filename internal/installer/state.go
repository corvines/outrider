package installer

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// StateLayoutEntries are the directories the runner creates under its state
// root. Removal requires at least one of them to be present.
var StateLayoutEntries = []string{"models", "llama.cpp", "runs", "sessions", "downloads"}

type StateRootReport struct {
	Root    string
	Exists  bool
	Bytes   int64
	Entries []string
}

func InspectStateRoot(root string) (StateRootReport, error) {
	root, err := normalizeStateRoot(root)
	if err != nil {
		return StateRootReport{}, err
	}
	report := StateRootReport{Root: root}
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return report, nil
	}
	if err != nil {
		return StateRootReport{}, err
	}
	report.Exists = true
	for _, entry := range entries {
		report.Entries = append(report.Entries, entry.Name())
	}
	report.Bytes, err = directorySize(root)
	if err != nil {
		return StateRootReport{}, err
	}
	return report, nil
}

// RemoveStateRoot deletes the runner state root once it is recognisable as
// one. It refuses a symlink, a path shallow enough to be a volume or home
// root, and a directory carrying none of the layout it expects to own.
func RemoveStateRoot(root string) error {
	root, err := normalizeStateRoot(root)
	if err != nil {
		return err
	}
	info, err := os.Lstat(root)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing to remove state root %s: it is a symlink", root)
	}
	if !info.IsDir() {
		return fmt.Errorf("refusing to remove state root %s: it is not a directory", root)
	}
	if err := requireOwnedLayout(root); err != nil {
		return err
	}
	if err := os.RemoveAll(root); err != nil {
		return fmt.Errorf("remove Outrider state root: %w", err)
	}
	return nil
}

func normalizeStateRoot(root string) (string, error) {
	if strings.TrimSpace(root) == "" {
		return "", fmt.Errorf("a state root is required")
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	root = filepath.Clean(root)
	if depth(root) < 2 {
		return "", fmt.Errorf("refusing to treat %s as a state root: too close to the volume root", root)
	}
	return root, nil
}

func requireOwnedLayout(root string) error {
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return nil
	}
	for _, entry := range entries {
		for _, known := range StateLayoutEntries {
			if entry.Name() == known {
				return nil
			}
		}
	}
	return fmt.Errorf(
		"refusing to remove %s: it holds none of %s",
		root, strings.Join(StateLayoutEntries, ", "),
	)
}

func depth(path string) int {
	trimmed := strings.Trim(filepath.ToSlash(path), "/")
	if trimmed == "" {
		return 0
	}
	return len(strings.Split(trimmed, "/"))
}

func directorySize(root string) (int64, error) {
	var total int64
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) || os.IsPermission(err) {
				return nil
			}
			return err
		}
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		total += info.Size()
		return nil
	})
	return total, err
}
