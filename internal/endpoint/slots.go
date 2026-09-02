package endpoint

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type SlotResult struct {
	Slot       int                `json:"id_slot"`
	Filename   string             `json:"filename"`
	Saved      int                `json:"n_saved,omitempty"`
	Restored   int                `json:"n_restored,omitempty"`
	BytesWrote int64              `json:"n_written,omitempty"`
	BytesRead  int64              `json:"n_read,omitempty"`
	Timings    map[string]float64 `json:"timings,omitempty"`
}

func SaveSlot(ctx context.Context, endpointURL string, slot int, filename string) (SlotResult, error) {
	return requestSlotAction(ctx, endpointURL, slot, "save", filename)
}

func RestoreSlot(ctx context.Context, endpointURL string, slot int, filename string) (SlotResult, error) {
	return requestSlotAction(ctx, endpointURL, slot, "restore", filename)
}

func requestSlotAction(
	ctx context.Context,
	endpointURL string,
	slot int,
	action string,
	filename string,
) (SlotResult, error) {
	if slot < 0 {
		return SlotResult{}, fmt.Errorf("slot must be nonnegative")
	}
	if filename == "" || strings.ContainsAny(filename, "/\\\x00\n\r") {
		return SlotResult{}, fmt.Errorf("slot filename must be a plain filename")
	}
	body, err := json.Marshal(map[string]string{"filename": filename})
	if err != nil {
		return SlotResult{}, err
	}
	requestContext := ctx
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		requestContext, cancel = context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
	}
	requestURL := strings.TrimRight(endpointURL, "/") + "/slots/" + strconv.Itoa(slot) + "?action=" + action
	request, err := http.NewRequestWithContext(requestContext, http.MethodPost, requestURL, bytes.NewReader(body))
	if err != nil {
		return SlotResult{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return SlotResult{}, fmt.Errorf("slot %s request failed at %s: %w", action, requestURL, err)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return SlotResult{}, fmt.Errorf("slot %s response failed: %w", action, err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		detail := firstUsefulLine(string(responseBody))
		if detail == "" {
			detail = "empty response"
		}
		return SlotResult{}, fmt.Errorf("slot %s returned HTTP %d: %s", action, response.StatusCode, detail)
	}
	var result SlotResult
	if err := json.Unmarshal(responseBody, &result); err != nil {
		return SlotResult{}, fmt.Errorf("slot %s response was not valid JSON: %w", action, err)
	}
	if result.Slot != slot || result.Filename != filename {
		return SlotResult{}, fmt.Errorf("slot %s response did not match the request", action)
	}
	return result, nil
}
