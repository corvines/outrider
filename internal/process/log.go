package process

import (
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/corvines/outrider/internal/manifest"
)

func startupExitError(waitErr error, plan manifest.Plan) error {
	detail := lastUsefulLogLine(plan.State.Log)
	cause := explainLoadingFailure(detail)
	status := "without an exit status"
	if waitErr != nil {
		status = waitErr.Error()
	}
	return runnerErrorf(
		"llama-server exited before becoming healthy: %s\n%s\nLikely cause: %s\nLog: %s",
		status, detail, cause, plan.State.Log,
	)
}

func lastUsefulLogLine(path string) string {
	content, err := os.ReadFile(path)
	if err != nil {
		return "No server output was available."
	}
	lines := strings.Split(string(content), "\n")
	for _, marker := range []string{"error loading model:", "error:", "failed", "unknown", "unsupported"} {
		for index := len(lines) - 1; index >= 0; index-- {
			line := strings.TrimSpace(lines[index])
			if line != "" && strings.Contains(strings.ToLower(line), marker) &&
				!strings.Contains(strings.ToLower(line), "exiting due to model loading error") {
				return line
			}
		}
	}
	for index := len(lines) - 1; index >= 0; index-- {
		if line := strings.TrimSpace(lines[index]); line != "" {
			return line
		}
	}
	return "No server output was available."
}

func explainLoadingFailure(detail string) string {
	lower := strings.ToLower(detail)
	switch {
	case strings.Contains(lower, "rope.dimension_sections has wrong array length"):
		return "this GGUF uses a Qwen3.5 metadata layout that the pinned runtime cannot read. Use a tested profile or another development model."
	case strings.Contains(lower, "wrong number of tensors"):
		return "this model likely uses a conversion layout or companion assets that the pinned runtime cannot load directly. Try another development model."
	case strings.Contains(lower, "unsupported model architecture"):
		return "the pinned runtime does not support this model architecture. Try another development model."
	default:
		return fmt.Sprintf("the model is incompatible with the pinned runtime or its launch settings. The loader reported %q.", detail)
	}
}

const serverLogMaxBytes int64 = 8 * 1024 * 1024

type rotatingLog struct {
	mu       sync.Mutex
	path     string
	maxBytes int64
	file     *os.File
	size     int64
}

func openRotatingLog(path string, maxBytes int64) (*rotatingLog, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, err
	}
	return &rotatingLog{path: path, maxBytes: maxBytes, file: file, size: info.Size()}, nil
}

func (log *rotatingLog) Write(data []byte) (int, error) {
	log.mu.Lock()
	defer log.mu.Unlock()
	if int64(len(data)) > log.maxBytes {
		data = data[len(data)-int(log.maxBytes):]
	}
	if log.size+int64(len(data)) > log.maxBytes {
		if err := log.rotate(); err != nil {
			return 0, err
		}
	}
	written, err := log.file.Write(data)
	log.size += int64(written)
	return written, err
}

func (log *rotatingLog) Close() error {
	log.mu.Lock()
	defer log.mu.Unlock()
	if log.file == nil {
		return nil
	}
	err := log.file.Close()
	log.file = nil
	return err
}

func (log *rotatingLog) rotate() error {
	if err := log.file.Close(); err != nil {
		return err
	}
	backup := log.path + ".1"
	if err := os.Remove(backup); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(log.path, backup); err != nil && !os.IsNotExist(err) {
		return err
	}
	file, err := os.OpenFile(log.path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	log.file = file
	log.size = 0
	return nil
}
