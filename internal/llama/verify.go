package llama

import (
	"context"
	"fmt"
	"os"

	"github.com/corvines/outrider/internal/manifest"
)

// VerifyCachedArtifact checks a file without downloading, moving or replacing it.
func VerifyCachedArtifact(ctx context.Context, artifact manifest.Artifact, path string, progress ProgressFunc) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if artifact.SHA256 == "" || artifact.SizeBytes <= 0 {
		return fmt.Errorf("artifact has no declared digest or size")
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("not a regular file: %s", path)
	}
	if info.Size() != artifact.SizeBytes {
		return fmt.Errorf("%s: size %d, expected %d bytes", path, info.Size(), artifact.SizeBytes)
	}
	valid, err := isValidGGUF(path)
	if err != nil {
		return err
	}
	if !valid {
		return fmt.Errorf("not a GGUF file: %s", path)
	}
	return verifySHA256WithProgress(ctx, path, artifact.SHA256, "cached artifact", "verify "+artifact.File, progress)
}
