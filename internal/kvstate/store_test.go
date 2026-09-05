package kvstate

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckpointAndRestoreKeepOneCompatibleSnapshot(t *testing.T) {
	directory := t.TempDir()
	server := slotServer(t, directory)
	defer server.Close()
	first := testConfig(directory, strings.Repeat("a", 64))
	saved, err := Checkpoint(context.Background(), server.URL, first)
	if err != nil || saved.Tokens != 42 || saved.Bytes == 0 {
		t.Fatalf("checkpoint = %#v, %v", saved, err)
	}
	restored, err := Restore(context.Background(), server.URL, first)
	if err != nil || restored.Tokens != 42 || restored.Detail != "restored" {
		t.Fatalf("restore = %#v, %v", restored, err)
	}
	second := testConfig(directory, strings.Repeat("b", 64))
	if _, err := Checkpoint(context.Background(), server.URL, second); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(directory, first.Filename)); !os.IsNotExist(err) {
		t.Fatalf("obsolete snapshot remains: %v", err)
	}
	if _, err := os.Stat(filepath.Join(directory, second.Filename)); err != nil {
		t.Fatal(err)
	}
}

func TestRestoreSkipsMissingSnapshot(t *testing.T) {
	config := testConfig(t.TempDir(), strings.Repeat("c", 64))
	result, err := Restore(context.Background(), "http://127.0.0.1:1", config)
	if err != nil || result.Detail != "no compatible snapshot" {
		t.Fatalf("restore = %#v, %v", result, err)
	}
}

func TestPrepareRejectsSymlinkDirectory(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	config := testConfig(link, strings.Repeat("d", 64))
	if err := Prepare(config); err == nil || !strings.Contains(err.Error(), "not an owned directory") {
		t.Fatalf("prepare error = %v", err)
	}
}

func slotServer(t *testing.T, directory string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		var body map[string]string
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		filename := body["filename"]
		action := request.URL.Query().Get("action")
		if action == "save" {
			if err := os.WriteFile(filepath.Join(directory, filename), []byte("snapshot"), 0o600); err != nil {
				t.Fatal(err)
			}
			_ = json.NewEncoder(response).Encode(map[string]any{
				"id_slot": 0, "filename": filename, "n_saved": 42, "n_written": 8,
			})
			return
		}
		if _, err := os.Stat(filepath.Join(directory, filename)); err != nil {
			http.Error(response, err.Error(), http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(response).Encode(map[string]any{
			"id_slot": 0, "filename": filename, "n_restored": 42, "n_read": 8,
		})
	}))
}

func testConfig(directory string, key string) Config {
	return Config{
		Enabled: true, Slot: 0, Key: key, Directory: directory,
		Filename: "slot-" + key + ".bin", Profile: "qwen35-0.8b",
	}
}
