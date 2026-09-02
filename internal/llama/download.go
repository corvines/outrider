package llama

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/corvines/outrider/internal/manifest"
)

type resumeMetadata struct {
	URL          string `json:"url"`
	ETag         string `json:"etag,omitempty"`
	LastModified string `json:"lastModified,omitempty"`
}

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
		lastErr = downloadAttempt(ctx, http.DefaultClient, sourceURL, destination)
		if lastErr == nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
	return lastErr
}

func downloadAttempt(ctx context.Context, client *http.Client, sourceURL string, destination string) error {
	offset, metadata := resumableOffset(destination, sourceURL)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return err
	}
	if offset > 0 {
		request.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
		if metadata.ETag != "" {
			request.Header.Set("If-Range", metadata.ETag)
		} else {
			request.Header.Set("If-Range", metadata.LastModified)
		}
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	appendResponse := false
	total := response.ContentLength
	switch response.StatusCode {
	case http.StatusOK:
		offset = 0
	case http.StatusPartialContent:
		start, contentTotal, parseErr := parseContentRange(response.Header.Get("Content-Range"))
		if parseErr != nil || offset == 0 || start != offset {
			return fmt.Errorf("GET %s returned an invalid resume response: %s", sourceURL, response.Status)
		}
		appendResponse = true
		total = contentTotal
	case http.StatusRequestedRangeNotSatisfiable:
		contentTotal, parseErr := parseUnsatisfiedRange(response.Header.Get("Content-Range"))
		if parseErr == nil && offset == contentTotal {
			_ = os.Remove(resumeMetadataPath(destination))
			return nil
		}
		return fmt.Errorf("GET %s returned %s for a %d-byte partial", sourceURL, response.Status, offset)
	default:
		_, _ = io.Copy(io.Discard, response.Body)
		return fmt.Errorf("GET %s returned %s", sourceURL, response.Status)
	}

	metadata = responseMetadata(sourceURL, response, metadata)
	if metadata.ETag != "" || metadata.LastModified != "" {
		if err := writeResumeMetadata(destination, metadata); err != nil {
			return err
		}
	}
	flags := os.O_CREATE | os.O_WRONLY
	if appendResponse {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}
	file, err := os.OpenFile(destination, flags, 0o600)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(file, response.Body)
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if copyErr != nil {
		return copyErr
	}
	if total >= 0 {
		info, err := os.Stat(destination)
		if err != nil {
			return err
		}
		if info.Size() != total {
			return fmt.Errorf("GET %s ended at %d bytes; expected %d", sourceURL, info.Size(), total)
		}
	}
	if err := os.Remove(resumeMetadataPath(destination)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func resumableOffset(destination string, sourceURL string) (int64, resumeMetadata) {
	info, err := os.Stat(destination)
	if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
		return 0, resumeMetadata{}
	}
	metadata, err := readResumeMetadata(destination)
	if err != nil || metadata.URL != sourceURL || metadata.ETag == "" && metadata.LastModified == "" {
		_ = os.Remove(destination)
		_ = os.Remove(resumeMetadataPath(destination))
		return 0, resumeMetadata{}
	}
	return info.Size(), metadata
}

func responseMetadata(sourceURL string, response *http.Response, previous resumeMetadata) resumeMetadata {
	metadata := resumeMetadata{
		URL: sourceURL, ETag: response.Header.Get("ETag"), LastModified: response.Header.Get("Last-Modified"),
	}
	if metadata.ETag == "" && metadata.LastModified == "" {
		metadata.ETag = previous.ETag
		metadata.LastModified = previous.LastModified
	}
	return metadata
}

func readResumeMetadata(destination string) (resumeMetadata, error) {
	data, err := os.ReadFile(resumeMetadataPath(destination))
	if err != nil {
		return resumeMetadata{}, err
	}
	var metadata resumeMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return resumeMetadata{}, err
	}
	return metadata, nil
}

func writeResumeMetadata(destination string, metadata resumeMetadata) error {
	data, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	path := resumeMetadataPath(destination)
	file, err := os.CreateTemp(filepath.Dir(path), ".resume-")
	if err != nil {
		return err
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	if err := file.Chmod(0o600); err != nil {
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

func resumeMetadataPath(destination string) string {
	return destination + ".resume.json"
}

func parseContentRange(value string) (int64, int64, error) {
	var start, end, total int64
	if _, err := fmt.Sscanf(value, "bytes %d-%d/%d", &start, &end, &total); err != nil {
		return 0, 0, err
	}
	if start < 0 || end < start || total <= end {
		return 0, 0, fmt.Errorf("invalid Content-Range %q", value)
	}
	return start, total, nil
}

func parseUnsatisfiedRange(value string) (int64, error) {
	var total int64
	if _, err := fmt.Sscanf(value, "bytes */%d", &total); err != nil {
		return 0, err
	}
	if total < 0 {
		return 0, fmt.Errorf("invalid Content-Range %q", value)
	}
	return total, nil
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
	partial := archive + ".part"
	if download == nil {
		download = DownloadFile
	}
	if err := download(ctx, release.URL, partial); err != nil {
		return runnerError("could not download pinned llama.cpp", err)
	}
	if err := verifySHA256(partial, release.SHA256, "llama.cpp archive"); err != nil {
		_ = os.Remove(partial)
		_ = os.Remove(resumeMetadataPath(partial))
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
