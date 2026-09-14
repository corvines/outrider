package llama

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/corvines/outrider/internal/manifest"
)

func TestVerifyCachedArtifact(t *testing.T) {
	valid := []byte("GGUFtest")
	for _, tc := range []struct {
		name                                              string
		data                                              []byte
		missing, directory, noDigest, canceled, wantError bool
	}{
		{name: "valid", data: valid},
		{name: "missing", missing: true, wantError: true},
		{name: "directory", directory: true, wantError: true},
		{name: "wrong size", data: []byte("GGUF"), wantError: true},
		{name: "wrong header", data: []byte("NOPEtest"), wantError: true},
		{name: "wrong digest", data: []byte("GGUFfail"), wantError: true},
		{name: "no digest", data: valid, noDigest: true, wantError: true},
		{name: "canceled", data: valid, canceled: true, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "model.gguf")
			if tc.directory {
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatal(err)
				}
			} else if !tc.missing {
				if err := os.WriteFile(path, tc.data, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			artifact := manifest.Artifact{File: "model.gguf", SizeBytes: int64(len(valid)), SHA256: bytesSHA256(valid)}
			if tc.noDigest {
				artifact.SHA256 = ""
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tc.canceled {
				cancel()
			}
			err := VerifyCachedArtifact(ctx, artifact, path, nil)
			if (err != nil) != tc.wantError {
				t.Fatalf("error = %v", err)
			}
			if tc.canceled && !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation = %v", err)
			}
			entries, err := os.ReadDir(root)
			if err != nil {
				t.Fatal(err)
			}
			wantEntries := 1
			if tc.missing {
				wantEntries = 0
			}
			if len(entries) != wantEntries {
				t.Fatalf("files changed: %v", entries)
			}
			if !tc.missing && !tc.directory {
				data, err := os.ReadFile(path)
				if err != nil || string(data) != string(tc.data) {
					t.Fatalf("cache changed: %q, %v", data, err)
				}
			}
		})
	}
}
