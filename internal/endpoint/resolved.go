package endpoint

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Resolved is what a running backend reports about itself. Every field is
// measured off the live process, so a value here can disagree with the
// profile that asked for it, which is the whole reason to publish it.
type Resolved struct {
	Endpoint        string
	Build           string
	Context         int
	TrainingContext int
	Quantization    string
	ModelPath       string
	Slots           int
	Modalities      Modalities
	SupportsTools   bool
	Speculation     string
	Sampling        ResolvedSampling
	Samplers        []string
}

// Modalities is what the loaded process can actually accept. A profile can
// declare a projector and still load without vision, so this is the answer
// that counts.
type Modalities struct {
	Vision bool
	Audio  bool
	Video  bool
}

// ResolvedSampling is the sampler in force, which is not necessarily the one
// the profile asked for.
type ResolvedSampling struct {
	Temperature   float64
	TopP          float64
	TopK          int
	MinP          float64
	RepeatPenalty float64
}

type propsResponse struct {
	DefaultGenerationSettings struct {
		Context int `json:"n_ctx"`
		Params  struct {
			Temperature      float64  `json:"temperature"`
			TopP             float64  `json:"top_p"`
			TopK             int      `json:"top_k"`
			MinP             float64  `json:"min_p"`
			RepeatPenalty    float64  `json:"repeat_penalty"`
			Samplers         []string `json:"samplers"`
			SpeculativeTypes string   `json:"speculative.types"`
		} `json:"params"`
	} `json:"default_generation_settings"`
	TotalSlots int    `json:"total_slots"`
	ModelFtype string `json:"model_ftype"`
	ModelPath  string `json:"model_path"`
	BuildInfo  string `json:"build_info"`
	Modalities struct {
		Vision bool `json:"vision"`
		Audio  bool `json:"audio"`
		Video  bool `json:"video"`
	} `json:"modalities"`
	ChatTemplateCaps struct {
		SupportsTools bool `json:"supports_tools"`
	} `json:"chat_template_caps"`
}

// FetchResolved reads the running backend's own view of itself. The training
// context comes from the backend's model listing, which is the only place it
// is reported; everything else comes from its properties.
func FetchResolved(ctx context.Context, endpointURL string, modelID string) (Resolved, error) {
	var props propsResponse
	if err := fetchJSON(ctx, endpointURL, "/props", &props); err != nil {
		return Resolved{}, err
	}
	resolved := Resolved{
		Endpoint:      endpointURL,
		Build:         props.BuildInfo,
		Context:       props.DefaultGenerationSettings.Context,
		Quantization:  props.ModelFtype,
		ModelPath:     props.ModelPath,
		Slots:         props.TotalSlots,
		SupportsTools: props.ChatTemplateCaps.SupportsTools,
		Speculation:   normalizeSpeculation(props.DefaultGenerationSettings.Params.SpeculativeTypes),
		Samplers:      props.DefaultGenerationSettings.Params.Samplers,
		Modalities: Modalities{
			Vision: props.Modalities.Vision,
			Audio:  props.Modalities.Audio,
			Video:  props.Modalities.Video,
		},
		Sampling: ResolvedSampling{
			Temperature:   props.DefaultGenerationSettings.Params.Temperature,
			TopP:          props.DefaultGenerationSettings.Params.TopP,
			TopK:          props.DefaultGenerationSettings.Params.TopK,
			MinP:          props.DefaultGenerationSettings.Params.MinP,
			RepeatPenalty: props.DefaultGenerationSettings.Params.RepeatPenalty,
		},
	}
	var listing modelsResponse
	if err := fetchJSON(ctx, endpointURL, "/v1/models", &listing); err != nil {
		return Resolved{}, err
	}
	for index := range listing.Data {
		if listing.Data[index].ID != modelID {
			continue
		}
		resolved.TrainingContext = listing.Data[index].Meta.TrainingContext
		if resolved.Context == 0 {
			resolved.Context = listing.Data[index].Meta.Context
		}
	}
	return resolved, nil
}

// normalizeSpeculation drops the backend's word for "no draft path" so the
// field is empty in the same case the profile's own is.
func normalizeSpeculation(mode string) string {
	if mode == "none" {
		return ""
	}
	return mode
}

func fetchJSON(ctx context.Context, endpointURL string, path string, target any) error {
	url := strings.TrimRight(endpointURL, "/") + path
	requestContext, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(requestContext, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return fmt.Errorf("request to %s failed: %w", url, err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 8<<20))
	if err != nil {
		return fmt.Errorf("response from %s failed: %w", url, err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("%s returned HTTP %d", url, response.StatusCode)
	}
	if err := json.Unmarshal(body, target); err != nil {
		return fmt.Errorf("response from %s was not valid JSON: %w", url, err)
	}
	return nil
}
