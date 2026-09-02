package endpoint

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSlotSaveAndRestore(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/slots/0" {
			t.Fatalf("request = %s %s", request.Method, request.URL.String())
		}
		var body map[string]string
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		action := request.URL.Query().Get("action")
		result := SlotResult{Slot: 0, Filename: body["filename"]}
		if action == "save" {
			result.Saved = 42
			result.BytesWrote = 4096
		} else {
			result.Restored = 42
			result.BytesRead = 4096
		}
		_ = json.NewEncoder(response).Encode(result)
	}))
	defer server.Close()
	saved, err := SaveSlot(context.Background(), server.URL, 0, "slot.bin")
	if err != nil || saved.Saved != 42 || saved.BytesWrote != 4096 {
		t.Fatalf("save = %#v, %v", saved, err)
	}
	restored, err := RestoreSlot(context.Background(), server.URL, 0, "slot.bin")
	if err != nil || restored.Restored != 42 || restored.BytesRead != 4096 {
		t.Fatalf("restore = %#v, %v", restored, err)
	}
}

func TestSlotActionRejectsUnsafeFilenameAndServerErrors(t *testing.T) {
	if _, err := SaveSlot(context.Background(), "http://invalid", 0, "../slot.bin"); err == nil {
		t.Fatal("unsafe filename was accepted")
	}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		http.Error(response, "slot is processing", http.StatusConflict)
	}))
	defer server.Close()
	_, err := SaveSlot(context.Background(), server.URL, 0, "slot.bin")
	if err == nil || !strings.Contains(err.Error(), "slot is processing") {
		t.Fatalf("save error = %v", err)
	}
}
