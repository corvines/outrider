package llama

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/corvines/outrider/internal/manifest"
)

func ModelDownloadURL(profile manifest.Profile) (string, error) {
	if profile.Model.Repo == "" || profile.Model.File == "" {
		return "", runnerErrorf("model reference for %s is incomplete", profile.ID)
	}
	parts := strings.Split(profile.Model.Repo, "/")
	for i := range parts {
		parts[i] = url.PathEscape(parts[i])
	}
	return "https://huggingface.co/" + strings.Join(parts, "/") +
		"/resolve/main/" + url.PathEscape(profile.Model.File) + "?download=true", nil
}

func DownloadFile(ctx context.Context, sourceURL string, destination string) error {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
		if err != nil {
			return err
		}
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			lastErr = err
			continue
		}
		if response.StatusCode >= 200 && response.StatusCode < 300 {
			err := writeDownload(response.Body, destination)
			response.Body.Close()
			return err
		}
		lastErr = fmt.Errorf("GET %s returned %s", sourceURL, response.Status)
		_, _ = io.Copy(io.Discard, response.Body)
		response.Body.Close()
		if response.StatusCode != http.StatusRequestTimeout && response.StatusCode != http.StatusTooManyRequests && response.StatusCode < 500 {
			return lastErr
		}
	}
	return lastErr
}

func writeDownload(source io.Reader, destination string) error {
	file, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	closed := false
	succeeded := false
	defer func() {
		if !closed {
			file.Close()
		}
		if !succeeded {
			os.Remove(destination)
		}
	}()
	if _, err := io.Copy(file, source); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	closed = true
	succeeded = true
	return nil
}

func ensureArchive(ctx context.Context, archive string, release manifest.Release, download Downloader) error {
	exists, err := pathExists(archive)
	if err != nil {
		return err
	}
	if exists {
		if err := verifySHA256(archive, release.SHA256, "cached llama.cpp archive"); err != nil {
			return runnerErrorf("%v; refusing to overwrite %s", err, archive)
		}
		return nil
	}
	partial := fmt.Sprintf("%s.part-%d", archive, os.Getpid())
	if err := os.Remove(partial); err != nil && !os.IsNotExist(err) {
		return runnerError("could not clear partial runtime download", err)
	}
	defer os.Remove(partial)
	if download == nil {
		download = DownloadFile
	}
	if err := download(ctx, release.URL, partial); err != nil {
		return runnerError("could not download pinned llama.cpp", err)
	}
	if err := verifySHA256(partial, release.SHA256, "llama.cpp archive"); err != nil {
		return err
	}
	if err := os.Rename(partial, archive); err != nil {
		return runnerError("could not install runtime archive in cache", err)
	}
	return nil
}

func verifySHA256(path string, expected string, label string) error {
	digest, err := sha256File(path)
	if err != nil {
		return runnerError("could not hash "+label, err)
	}
	if !strings.EqualFold(digest, expected) {
		return runnerErrorf("%s checksum mismatch: expected %s, got %s", label, expected, digest)
	}
	return nil
}

func sha256File(path string) (string, error) {
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

func isValidGGUF(path string) (bool, error) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, runnerError("could not inspect model", err)
	}
	defer file.Close()
	header := make([]byte, 4)
	if _, err := io.ReadFull(file, header); err != nil {
		return false, nil
	}
	return string(header) == "GGUF", nil
}

func pathExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, runnerError("could not inspect "+filepath.Base(path), err)
}

func assertExecutable(path string, label string) error {
	details, err := os.Stat(path)
	if err != nil {
		return runnerError(fmt.Sprintf("%s is not executable at %s", label, path), err)
	}
	if !details.Mode().IsRegular() {
		return runnerErrorf("%s is not a regular file: %s", label, path)
	}
	if details.Mode().Perm()&0o111 == 0 {
		return runnerErrorf("%s is not executable at %s", label, path)
	}
	return nil
}

func isExecutable(path string) bool {
	return assertExecutable(path, "cached llama-server") == nil
}
