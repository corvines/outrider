package installer

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	UserTargetRelative = ".local/bin/outrider"
	UserMarkerRelative = ".local/share/outrider/install.json"
)

type UserLayout struct {
	Target string
	Marker string
	Guide  string
}

type UserInstallOptions struct {
	ReplaceUnmanaged bool
	// Link points the install target at the source binary instead of staging
	// a copy of it, so the target tracks the source as it is rebuilt.
	Link bool
}

func ResolveUserLayout(home string) (UserLayout, error) {
	if strings.TrimSpace(home) == "" {
		return UserLayout{}, fmt.Errorf("HOME is required for a user-local install")
	}
	home, err := filepath.Abs(home)
	if err != nil {
		return UserLayout{}, err
	}
	return UserLayout{
		Target: filepath.Join(home, UserTargetRelative),
		Marker: filepath.Join(home, UserMarkerRelative),
		Guide:  filepath.Join(home, filepath.FromSlash(UserGuideRelative)),
	}, nil
}

func InstallUser(binary string, home string) (Marker, error) {
	return InstallUserWithOptions(binary, home, UserInstallOptions{})
}

func InstallUserWithOptions(binary string, home string, options UserInstallOptions) (Marker, error) {
	layout, err := ResolveUserLayout(home)
	if err != nil {
		return Marker{}, err
	}
	binary, err = filepath.Abs(binary)
	if err != nil {
		return Marker{}, err
	}
	info, err := os.Stat(binary)
	if err != nil {
		return Marker{}, fmt.Errorf("inspect install binary: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return Marker{}, fmt.Errorf("install binary is not an executable regular file: %s", binary)
	}
	targetExists, err := exists(layout.Target)
	if err != nil {
		return Marker{}, err
	}
	markerExists, err := exists(layout.Marker)
	if err != nil {
		return Marker{}, err
	}
	var existing *Marker
	if options.ReplaceUnmanaged && targetExists && !markerExists {
		existing = nil
	} else {
		existing, err = verifyOwnedUserInstall(layout)
	}
	if err != nil {
		return Marker{}, err
	}
	// Reinstalling the same binary the same way is a no-op. A request that
	// changes the kind of install always rewrites the target.
	if existing != nil && (existing.Link != "") == options.Link {
		targetInfo, statErr := os.Stat(layout.Target)
		if statErr == nil && os.SameFile(info, targetInfo) {
			return *existing, nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(layout.Target), 0o755); err != nil {
		return Marker{}, err
	}
	marker, err := placeUserBinary(binary, layout, options.Link)
	if err != nil {
		return Marker{}, err
	}
	if err := writeMarker(layout.Marker, marker); err != nil {
		if existing == nil {
			_ = os.Remove(layout.Target)
		}
		return Marker{}, err
	}
	if err := placeGuide(binary, layout.Guide); err != nil {
		return Marker{}, err
	}
	return marker, nil
}

func placeUserBinary(binary string, layout UserLayout, link bool) (Marker, error) {
	if link {
		if err := replaceWithSymlink(binary, layout.Target); err != nil {
			return Marker{}, err
		}
		return Marker{Schema: MarkerSchema, Target: layout.Target, Link: binary}, nil
	}
	if err := copyExecutable(binary, layout.Target); err != nil {
		return Marker{}, err
	}
	digest, err := fileSHA256(layout.Target)
	if err != nil {
		_ = os.Remove(layout.Target)
		return Marker{}, err
	}
	return Marker{Schema: MarkerSchema, Target: layout.Target, SHA256: digest}, nil
}

// replaceWithSymlink swaps the target for a link atomically: a symlink is
// created beside the target and renamed over it, so a failure part way through
// leaves the previous install intact.
func replaceWithSymlink(destination string, target string) error {
	temporary := filepath.Join(filepath.Dir(target), ".outrider-link")
	if err := os.Remove(temporary); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.Symlink(destination, temporary); err != nil {
		return err
	}
	if err := os.Rename(temporary, target); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return nil
}

func VerifyUser(home string) (Marker, error) {
	layout, err := ResolveUserLayout(home)
	if err != nil {
		return Marker{}, err
	}
	marker, err := verifyOwnedUserInstall(layout)
	if err != nil {
		return Marker{}, err
	}
	if marker == nil {
		return Marker{}, fmt.Errorf("Outrider is not installed at %s", layout.Target)
	}
	return *marker, nil
}

func UninstallUser(home string) error {
	layout, err := ResolveUserLayout(home)
	if err != nil {
		return err
	}
	marker, err := VerifyUser(home)
	if err != nil {
		return err
	}
	if marker.Target != layout.Target {
		return fmt.Errorf("refusing to remove unexpected install target %s", marker.Target)
	}
	if err := os.Remove(layout.Target); err != nil {
		return fmt.Errorf("remove installed Outrider artifact: %w", err)
	}
	if err := os.Remove(layout.Marker); err != nil {
		return fmt.Errorf("remove Outrider ownership marker: %w", err)
	}
	if err := os.Remove(layout.Guide); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove Outrider guide: %w", err)
	}
	removeEmptyParents(filepath.Dir(layout.Marker), home)
	removeEmptyParents(filepath.Dir(layout.Target), home)
	return nil
}

func verifyOwnedUserInstall(layout UserLayout) (*Marker, error) {
	targetExists, err := exists(layout.Target)
	if err != nil {
		return nil, err
	}
	markerExists, err := exists(layout.Marker)
	if err != nil {
		return nil, err
	}
	if !targetExists && !markerExists {
		return nil, nil
	}
	if !targetExists || !markerExists {
		return nil, fmt.Errorf(
			"refusing to replace incomplete user install: target=%t marker=%t", targetExists, markerExists,
		)
	}
	marker, err := readMarker(layout.Marker)
	if err != nil {
		return nil, err
	}
	if marker.Target != layout.Target {
		return nil, fmt.Errorf("ownership marker targets %s, expected %s", marker.Target, layout.Target)
	}
	if marker.Link != "" {
		destination, err := os.Readlink(layout.Target)
		if err != nil {
			return nil, fmt.Errorf("refusing to replace %s: it is no longer a symlink: %w", layout.Target, err)
		}
		if destination != marker.Link {
			return nil, fmt.Errorf(
				"refusing to replace %s: it points at %s, ownership marker requires %s",
				layout.Target, destination, marker.Link,
			)
		}
		return &marker, nil
	}
	digest, err := fileSHA256(layout.Target)
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(digest, marker.SHA256) {
		return nil, fmt.Errorf(
			"refusing to replace %s: installed SHA-256 is %s, ownership marker requires %s",
			layout.Target, digest, marker.SHA256,
		)
	}
	return &marker, nil
}

func exists(path string) (bool, error) {
	_, err := os.Lstat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}
