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
	}, nil
}

func InstallUser(binary string, home string) (Marker, error) {
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
	existing, err := verifyOwnedUserInstall(layout)
	if err != nil {
		return Marker{}, err
	}
	if existing != nil {
		targetInfo, statErr := os.Stat(layout.Target)
		if statErr == nil && os.SameFile(info, targetInfo) {
			return *existing, nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(layout.Target), 0o755); err != nil {
		return Marker{}, err
	}
	if err := copyExecutable(binary, layout.Target); err != nil {
		return Marker{}, err
	}
	digest, err := fileSHA256(layout.Target)
	if err != nil {
		if existing == nil {
			_ = os.Remove(layout.Target)
		}
		return Marker{}, err
	}
	marker := Marker{Schema: MarkerSchema, Target: layout.Target, SHA256: digest}
	if err := writeMarker(layout.Marker, marker); err != nil {
		if existing == nil {
			_ = os.Remove(layout.Target)
		}
		return Marker{}, err
	}
	return marker, nil
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
