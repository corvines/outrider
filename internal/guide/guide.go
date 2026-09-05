// Package guide finds the documentation the chat window answers from. The
// guide is product copy, so it ships as a file beside the installed binary
// rather than inside it, and a correction reaches a user without a release.
package guide

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Filename is the guide's name wherever it is placed.
const Filename = "llms.txt"

// UserRelative and SystemPath sit beside the two install markers, so the guide
// travels with the install that owns it.
const (
	UserRelative = ".local/share/outrider/" + Filename
	SystemPath   = "/usr/local/share/outrider/" + Filename
)

// EnvOverride names a guide to read instead of the installed one.
const EnvOverride = "OUTRIDER_GUIDE"

// Source is a guide that was found, and where.
type Source struct {
	Text string
	Path string
}

// Load returns the first guide it finds. The search runs override, user
// install, system install, then the repository copy beside a `go build`
// binary, so a development tree needs no install to answer questions.
func Load() (Source, error) {
	for _, candidate := range searchPaths() {
		text, err := os.ReadFile(candidate)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return Source{}, fmt.Errorf("read guide %s: %w", candidate, err)
		}
		if strings.TrimSpace(string(text)) == "" {
			continue
		}
		return Source{Text: string(text), Path: candidate}, nil
	}
	return Source{}, &NotFoundError{Looked: searchPaths()}
}

// NotFoundError reports every path that was tried, because the fix is always
// to put the file at one of them.
type NotFoundError struct {
	Looked []string
}

func (e *NotFoundError) Error() string {
	return "no outrider guide found; looked in " + strings.Join(e.Looked, ", ")
}

func searchPaths() []string {
	var paths []string
	if override := strings.TrimSpace(os.Getenv(EnvOverride)); override != "" {
		paths = append(paths, override)
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		paths = append(paths, filepath.Join(home, filepath.FromSlash(UserRelative)))
	}
	paths = append(paths, SystemPath)
	return append(paths, repositoryPaths()...)
}

// repositoryPaths covers a binary that was built but never installed. `go
// build ./cmd/outrider` leaves it at the repository root, and `go run` leaves
// it in a temporary directory where neither candidate resolves.
func repositoryPaths() []string {
	executable, err := os.Executable()
	if err != nil {
		return nil
	}
	if resolved, err := filepath.EvalSymlinks(executable); err == nil {
		executable = resolved
	}
	directory := filepath.Dir(executable)
	return []string{
		filepath.Join(directory, "docs", Filename),
		filepath.Join(filepath.Dir(directory), "docs", Filename),
	}
}
