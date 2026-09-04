package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/corvines/outrider/internal/manifest"
)

type cacheCleanupEntry struct {
	Path      string `json:"path"`
	Reason    string `json:"reason"`
	SizeBytes int64  `json:"sizeBytes"`
}

type cacheCleanupOutput struct {
	Root           string              `json:"root"`
	DryRun         bool                `json:"dryRun"`
	Candidates     []cacheCleanupEntry `json:"candidates,omitempty"`
	Protected      []cacheCleanupEntry `json:"protected,omitempty"`
	Removed        []cacheCleanupEntry `json:"removed,omitempty"`
	ReclaimedBytes int64               `json:"reclaimedBytes,omitempty"`
}

func parseCacheCleanArguments(arguments []string) (bool, error) {
	flags := flag.NewFlagSet("cache clean", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	apply := flags.Bool("apply", false, "remove cleanup candidates")
	if err := flags.Parse(arguments); err != nil {
		return false, usageError(err.Error())
	}
	if flags.NArg() != 0 {
		return false, usageError("cache clean accepts only --apply")
	}
	return *apply, nil
}

func cleanCache(environment map[string]string, apply bool) (cacheCleanupOutput, error) {
	root, err := manifest.StateRoot(environment["OUTRIDER_HOME"])
	if err != nil {
		return cacheCleanupOutput{}, err
	}
	profiles, err := manifest.All()
	if err != nil {
		return cacheCleanupOutput{}, err
	}
	// A partial download is resumable, so every profile's keeps its own.
	protected := map[string]string{}
	for _, profile := range profiles {
		paths, err := manifest.Paths(root, profile, "")
		if err != nil {
			return cacheCleanupOutput{}, err
		}
		protected[filepath.Clean(paths.Model+".part")] = "protected " + profile.ID + " partial"
		protected[filepath.Clean(paths.Model+".part.resume.json")] = "protected " + profile.ID + " resume metadata"
	}
	output := cacheCleanupOutput{Root: root, DryRun: !apply}
	for _, dir := range []string{filepath.Join(root, "models"), filepath.Join(root, "downloads")} {
		if err := collectCacheCleanup(dir, protected, &output); err != nil {
			return cacheCleanupOutput{}, err
		}
	}
	sort.Slice(output.Candidates, func(i, j int) bool { return output.Candidates[i].Path < output.Candidates[j].Path })
	sort.Slice(output.Protected, func(i, j int) bool { return output.Protected[i].Path < output.Protected[j].Path })
	if !apply {
		return output, nil
	}
	for _, candidate := range output.Candidates {
		if err := os.Remove(candidate.Path); err != nil {
			return cacheCleanupOutput{}, fmt.Errorf("remove %s: %w", candidate.Path, err)
		}
		output.Removed = append(output.Removed, candidate)
		output.ReclaimedBytes += candidate.SizeBytes
	}
	return output, nil
}

func collectCacheCleanup(root string, protected map[string]string, output *cacheCleanupOutput) error {
	info, err := os.Stat(root)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("cache path %s is not a directory", root)
	}
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		reason := cacheCleanupReason(entry.Name())
		if reason == "" {
			return nil
		}
		fileInfo, err := entry.Info()
		if err != nil {
			return err
		}
		if !fileInfo.Mode().IsRegular() {
			return nil
		}
		entryOutput := cacheCleanupEntry{Path: path, Reason: reason, SizeBytes: fileInfo.Size()}
		if protectedReason, ok := protected[filepath.Clean(path)]; ok {
			entryOutput.Reason = protectedReason
			output.Protected = append(output.Protected, entryOutput)
			return nil
		}
		output.Candidates = append(output.Candidates, entryOutput)
		return nil
	})
}

func cacheCleanupReason(name string) string {
	switch {
	case strings.HasSuffix(name, ".part.resume.json"):
		return "resume metadata"
	case strings.HasSuffix(name, ".part"):
		return "incomplete download"
	case strings.HasSuffix(name, ".corrupt"):
		return "quarantined artifact"
	default:
		return ""
	}
}
