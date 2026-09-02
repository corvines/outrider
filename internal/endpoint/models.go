package endpoint

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/corvines/outrider/internal/manifest"
)

type ModelContract struct {
	Profile         string `json:"profile"`
	ModelID         string `json:"modelId"`
	LoadedContext   int    `json:"loadedContext"`
	TrainingContext int    `json:"trainingContext,omitempty"`
	Quantization    string `json:"quantization"`
	MTPEnabled      bool   `json:"mtpEnabled"`
	Endpoint        string `json:"endpoint"`
}

type modelsResponse struct {
	Data []modelEntry `json:"data"`
}

type modelEntry struct {
	ID   string    `json:"id"`
	Meta modelMeta `json:"meta"`
}

type modelMeta struct {
	Context         int `json:"n_ctx"`
	TrainingContext int `json:"n_ctx_train"`
}

func VerifyModelContract(
	ctx context.Context,
	endpointURL string,
	profile manifest.Profile,
) (ModelContract, error) {
	modelsURL := strings.TrimRight(endpointURL, "/") + "/v1/models"
	requestContext, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(requestContext, http.MethodGet, modelsURL, nil)
	if err != nil {
		return ModelContract{}, err
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return ModelContract{}, fmt.Errorf("model contract request failed at %s: %w", modelsURL, err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return ModelContract{}, fmt.Errorf("model contract response failed at %s: %w", modelsURL, err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return ModelContract{}, fmt.Errorf("model contract request returned HTTP %d", response.StatusCode)
	}
	var parsed modelsResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return ModelContract{}, fmt.Errorf("model contract response was not valid JSON: %w", err)
	}
	var matched *modelEntry
	for index := range parsed.Data {
		entry := &parsed.Data[index]
		if entry.ID != profile.ID {
			continue
		}
		if matched != nil {
			return ModelContract{}, fmt.Errorf("model contract returned duplicate stable ID %q", profile.ID)
		}
		matched = entry
	}
	if matched == nil {
		return ModelContract{}, fmt.Errorf("model contract did not return stable ID %q", profile.ID)
	}
	if matched.Meta.Context != profile.Context.Size {
		return ModelContract{}, fmt.Errorf(
			"model contract loaded context is %d; profile %s requires %d",
			matched.Meta.Context, profile.ID, profile.Context.Size,
		)
	}
	return ModelContract{
		Profile: profile.ID, ModelID: matched.ID, LoadedContext: matched.Meta.Context,
		TrainingContext: matched.Meta.TrainingContext, Quantization: profile.Model.Quant,
		MTPEnabled: profile.Speculation.Mode == "mtp", Endpoint: endpointURL,
	}, nil
}
