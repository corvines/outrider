package kvstate

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/corvines/outrider/internal/endpoint"
)

const metadataSchema = 1

var (
	sessionKeyPattern   = regexp.MustCompile(`^[0-9a-f]{64}$`)
	snapshotNamePattern = regexp.MustCompile(`^slot-[0-9a-f]{64}\.(?:bin|json)$`)
)

type Config struct {
	Enabled   bool
	Slot      int
	Key       string
	Directory string
	Filename  string
	Profile   string
}

type Result struct {
	Action   string  `json:"action"`
	Snapshot string  `json:"snapshot,omitempty"`
	Tokens   int     `json:"tokens,omitempty"`
	Bytes    int64   `json:"bytes,omitempty"`
	Elapsed  float64 `json:"elapsedMs,omitempty"`
	Detail   string  `json:"detail"`
}

type metadata struct {
	Schema  int    `json:"schema"`
	Key     string `json:"key"`
	Profile string `json:"profile"`
	Tokens  int    `json:"tokens"`
	Bytes   int64  `json:"bytes"`
	SavedAt string `json:"savedAt"`
}

func Prepare(config Config) error {
	if !config.Enabled {
		return nil
	}
	if err := validate(config); err != nil {
		return err
	}
	if err := os.MkdirAll(config.Directory, 0o700); err != nil {
		return fmt.Errorf("could not create persistent KV directory: %w", err)
	}
	info, err := os.Lstat(config.Directory)
	if err != nil {
		return fmt.Errorf("could not inspect persistent KV directory: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("persistent KV path is not an owned directory: %s", config.Directory)
	}
	if err := os.Chmod(config.Directory, 0o700); err != nil {
		return fmt.Errorf("could not protect persistent KV directory: %w", err)
	}
	return nil
}

func Restore(ctx context.Context, endpointURL string, config Config) (Result, error) {
	result := Result{Action: "restore", Detail: "no compatible snapshot"}
	if !config.Enabled {
		result.Detail = "disabled"
		return result, nil
	}
	if err := Prepare(config); err != nil {
		return Result{}, err
	}
	snapshot := filepath.Join(config.Directory, config.Filename)
	info, err := regularFile(snapshot)
	if os.IsNotExist(err) {
		return result, nil
	}
	if err != nil {
		return Result{}, err
	}
	started := time.Now()
	restored, err := endpoint.RestoreSlot(ctx, endpointURL, config.Slot, config.Filename)
	if err != nil {
		return Result{}, err
	}
	return Result{
		Action: "restore", Snapshot: snapshot, Tokens: restored.Restored,
		Bytes: info.Size(), Elapsed: milliseconds(started), Detail: "restored",
	}, nil
}

func Checkpoint(ctx context.Context, endpointURL string, config Config) (Result, error) {
	result := Result{Action: "checkpoint", Detail: "disabled"}
	if !config.Enabled {
		return result, nil
	}
	if err := Prepare(config); err != nil {
		return Result{}, err
	}
	temporaryName := strings.TrimSuffix(config.Filename, ".bin") + ".next.bin"
	temporary := filepath.Join(config.Directory, temporaryName)
	if err := removeTemporary(temporary); err != nil {
		return Result{}, err
	}
	started := time.Now()
	saved, err := endpoint.SaveSlot(ctx, endpointURL, config.Slot, temporaryName)
	if err != nil {
		return Result{}, err
	}
	info, err := regularFile(temporary)
	if err != nil {
		return Result{}, fmt.Errorf("persistent KV checkpoint was not written safely: %w", err)
	}
	if saved.Saved <= 0 || info.Size() <= 0 {
		_ = os.Remove(temporary)
		result.Detail = "slot was empty"
		return result, nil
	}
	if err := os.Chmod(temporary, 0o600); err != nil {
		return Result{}, fmt.Errorf("could not protect persistent KV checkpoint: %w", err)
	}
	if err := syncFile(temporary); err != nil {
		return Result{}, err
	}
	snapshot := filepath.Join(config.Directory, config.Filename)
	if err := os.Rename(temporary, snapshot); err != nil {
		return Result{}, fmt.Errorf("could not promote persistent KV checkpoint: %w", err)
	}
	if err := syncDirectory(config.Directory); err != nil {
		return Result{}, err
	}
	meta := metadata{
		Schema: metadataSchema, Key: config.Key, Profile: config.Profile,
		Tokens: saved.Saved, Bytes: info.Size(), SavedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	if err := writeMetadata(strings.TrimSuffix(snapshot, ".bin")+".json", meta); err != nil {
		return Result{}, err
	}
	if err := pruneOtherSnapshots(config, snapshot); err != nil {
		return Result{}, err
	}
	if err := syncDirectory(config.Directory); err != nil {
		return Result{}, err
	}
	return Result{
		Action: "checkpoint", Snapshot: snapshot, Tokens: saved.Saved,
		Bytes: info.Size(), Elapsed: milliseconds(started), Detail: "saved",
	}, nil
}

func validate(config Config) error {
	if config.Slot < 0 || !sessionKeyPattern.MatchString(config.Key) {
		return fmt.Errorf("persistent KV configuration is invalid")
	}
	if config.Directory == "" || filepath.Base(config.Filename) != config.Filename ||
		config.Filename != "slot-"+config.Key+".bin" {
		return fmt.Errorf("persistent KV paths are invalid")
	}
	return nil
}

func regularFile(path string) (os.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("path is not a regular file: %s", path)
	}
	return info, nil
}

func removeTemporary(path string) error {
	_, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("could not remove stale persistent KV temporary file: %w", err)
	}
	return nil
}

func syncFile(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("could not open persistent KV checkpoint for sync: %w", err)
	}
	defer file.Close()
	if err := file.Sync(); err != nil {
		return fmt.Errorf("could not sync persistent KV checkpoint: %w", err)
	}
	return nil
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("could not open persistent KV directory for sync: %w", err)
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil {
		return fmt.Errorf("could not sync persistent KV directory: %w", err)
	}
	return nil
}

func writeMetadata(path string, value metadata) error {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	file, err := os.CreateTemp(filepath.Dir(path), ".slot-meta-")
	if err != nil {
		return fmt.Errorf("could not create persistent KV metadata: %w", err)
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	if err := file.Chmod(0o600); err != nil {
		file.Close()
		return err
	}
	if _, err := file.Write(encoded); err != nil {
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
	if err := os.Rename(temporary, path); err != nil {
		return fmt.Errorf("could not promote persistent KV metadata: %w", err)
	}
	return syncDirectory(filepath.Dir(path))
}

func pruneOtherSnapshots(config Config, keep string) error {
	entries, err := os.ReadDir(config.Directory)
	if err != nil {
		return err
	}
	keepMetadata := strings.TrimSuffix(keep, ".bin") + ".json"
	for _, entry := range entries {
		path := filepath.Join(config.Directory, entry.Name())
		if path == keep || path == keepMetadata || !snapshotNamePattern.MatchString(entry.Name()) {
			continue
		}
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("could not prune obsolete persistent KV snapshot: %w", err)
		}
	}
	return nil
}

func milliseconds(start time.Time) float64 {
	return float64(time.Since(start).Microseconds()) / 1000
}
