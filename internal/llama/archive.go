package llama

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/corvines/outrider/internal/manifest"
)

func installArchive(
	archive string,
	releaseParent string,
	releaseDirectory string,
	release manifest.Release,
) error {
	exists, err := pathExists(releaseDirectory)
	if err != nil {
		return err
	}
	if exists {
		return runnerErrorf(
			"llama.cpp release directory exists without a usable llama-server: %s",
			releaseDirectory,
		)
	}
	staging := filepath.Join(releaseParent, fmt.Sprintf(".staging-%d-%d", os.Getpid(), time.Now().UnixNano()))
	if err := os.MkdirAll(staging, 0o700); err != nil {
		return runnerError("could not create runtime staging directory", err)
	}
	defer os.RemoveAll(staging)

	serverFound, err := extractArchive(archive, staging, release.Directory)
	if err != nil {
		return err
	}
	expectedRoot := filepath.Join(staging, release.Directory)
	server := filepath.Join(expectedRoot, "llama-server")
	if !serverFound {
		return runnerErrorf("pinned archive does not contain %s/llama-server", release.Directory)
	}
	if err := os.Chmod(server, 0o755); err != nil {
		return runnerError("could not make llama-server executable", err)
	}
	if err := os.Rename(expectedRoot, releaseDirectory); err != nil {
		return runnerError("could not install pinned llama.cpp release", err)
	}
	return nil
}

func extractArchive(archive string, staging string, expectedDirectory string) (bool, error) {
	file, err := os.Open(archive)
	if err != nil {
		return false, runnerError("could not open pinned llama.cpp archive", err)
	}
	defer file.Close()
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return false, runnerError("could not read pinned llama.cpp archive", err)
	}
	defer gzipReader.Close()

	reader := tar.NewReader(gzipReader)
	expectedRoot := expectedDirectory + "/"
	serverFound := false
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return false, runnerError("could not inspect pinned llama.cpp archive", err)
		}
		name, err := safeArchivePath(header.Name, expectedRoot)
		if err != nil {
			return false, err
		}
		destination := filepath.Join(staging, filepath.FromSlash(name))
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(destination, 0o700); err != nil {
				return false, runnerError("could not extract runtime directory", err)
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := writeArchiveFile(reader, destination, os.FileMode(header.Mode)); err != nil {
				return false, err
			}
			if name == expectedRoot+"llama-server" {
				serverFound = true
			}
		case tar.TypeSymlink:
			if err := writeArchiveSymlink(destination, name, header.Linkname, expectedRoot); err != nil {
				return false, err
			}
		default:
			return false, runnerErrorf("refusing unsupported archive entry type for %s", header.Name)
		}
	}
	return serverFound, nil
}

func safeArchivePath(name string, expectedRoot string) (string, error) {
	if name == "" || strings.HasPrefix(name, "/") {
		return "", runnerErrorf("refusing archive path traversal entry: %s", name)
	}
	normalized := path.Clean(name)
	if normalized == "." || normalized == ".." || strings.HasPrefix(normalized, "../") {
		return "", runnerErrorf("refusing archive path traversal entry: %s", name)
	}
	if normalized != strings.TrimSuffix(expectedRoot, "/") && !strings.HasPrefix(normalized, expectedRoot) {
		return "", runnerErrorf("refusing unexpected archive entry outside %s: %s", expectedRoot, name)
	}
	return normalized, nil
}

func writeArchiveFile(reader io.Reader, destination string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return runnerError("could not create runtime archive parent", err)
	}
	file, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode.Perm())
	if err != nil {
		return runnerError("could not create runtime archive file", err)
	}
	if _, err := io.Copy(file, reader); err != nil {
		file.Close()
		return runnerError("could not extract runtime archive file", err)
	}
	if err := file.Close(); err != nil {
		return runnerError("could not close runtime archive file", err)
	}
	return nil
}

func writeArchiveSymlink(destination string, entryName string, target string, expectedRoot string) error {
	if target == "" || path.IsAbs(target) {
		return runnerErrorf("refusing unsafe archive symlink %s -> %s", entryName, target)
	}
	resolved := path.Clean(path.Join(path.Dir(entryName), target))
	if !strings.HasPrefix(resolved, expectedRoot) {
		return runnerErrorf("refusing unsafe archive symlink %s -> %s", entryName, target)
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return runnerError("could not create runtime symlink parent", err)
	}
	if err := os.Symlink(target, destination); err != nil {
		return runnerError("could not extract runtime symlink", err)
	}
	return nil
}
