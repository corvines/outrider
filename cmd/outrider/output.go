package main

import (
	"github.com/corvines/outrider/internal/endpoint"
	"github.com/corvines/outrider/internal/manifest"
)

type releaseOutput struct {
	Tag                 string `json:"tag"`
	Commit              string `json:"commit"`
	MacOSArm64Asset     string `json:"macosArm64Asset"`
	MacOSArm64Directory string `json:"macosArm64Directory"`
	MacOSArm64SHA256    string `json:"macosArm64Sha256"`
	MacOSArm64URL       string `json:"macosArm64Url"`
}

type modelOutput struct {
	Repository string `json:"repository"`
	File       string `json:"file"`
	Quant      string `json:"quant"`
	LocalPath  string `json:"localPath,omitempty"`
}

type stateOutput struct {
	Root          string `json:"root"`
	Binary        string `json:"binary"`
	ModelCache    string `json:"modelCache"`
	Model         string `json:"model"`
	RunDirectory  string `json:"runDirectory"`
	PIDFile       string `json:"pidFile"`
	LifecycleLock string `json:"lifecycleLock"`
	LogFile       string `json:"logFile"`
}

type planOutput struct {
	Preset         string        `json:"preset"`
	Description    string        `json:"description"`
	Release        releaseOutput `json:"release"`
	Port           int           `json:"port"`
	Executable     string        `json:"executable"`
	Model          modelOutput   `json:"model"`
	State          stateOutput   `json:"state"`
	Endpoint       string        `json:"endpoint"`
	HealthEndpoint string        `json:"healthEndpoint"`
	Argv           []string      `json:"argv"`
}

type upOutput struct {
	Kind       string             `json:"kind"`
	PID        int                `json:"pid,omitempty"`
	Endpoint   string             `json:"endpoint"`
	Health     *bool              `json:"health,omitempty"`
	Detail     string             `json:"detail,omitempty"`
	LogFile    string             `json:"logFile"`
	Timings    map[string]float64 `json:"timings,omitempty"`
	Executable string             `json:"executable"`
	Model      string             `json:"model"`
	Argv       []string           `json:"argv"`
}

type demoOutput struct {
	Endpoint               string             `json:"endpoint"`
	Model                  modelOutput        `json:"model"`
	Result                 string             `json:"result"`
	Timings                map[string]float64 `json:"timings"`
	GenerationTimingSource string             `json:"generationTimingSource,omitempty"`
	LogFile                string             `json:"logFile"`
	Cleanup                string             `json:"cleanup"`
}

func newPlanOutput(plan manifest.Plan) planOutput {
	release := manifest.LlamaRelease
	return planOutput{
		Preset: plan.Profile.ID, Description: plan.Profile.Description,
		Release: releaseOutput{
			Tag: release.Tag, Commit: release.Commit, MacOSArm64Asset: release.Asset,
			MacOSArm64Directory: release.Directory, MacOSArm64SHA256: release.SHA256,
			MacOSArm64URL: release.URL,
		},
		Port: plan.Port, Executable: plan.Executable, Model: newModelOutput(plan.Profile.Model),
		State: stateOutput{
			Root: plan.State.Root, Binary: plan.State.Executable, ModelCache: plan.State.Models,
			Model: plan.State.Model, RunDirectory: plan.State.Run, PIDFile: plan.State.PID,
			LifecycleLock: plan.State.Lock, LogFile: plan.State.Log,
		},
		Endpoint: plan.Endpoint, HealthEndpoint: plan.HealthEndpoint,
		Argv: append([]string{plan.Executable}, plan.Args...),
	}
}

func newModelOutput(model manifest.Artifact) modelOutput {
	return modelOutput{
		Repository: model.Repo, File: model.File, Quant: model.Quant, LocalPath: model.LocalPath,
	}
}

func newUpOutput(session runSession) upOutput {
	status := session.Status
	output := upOutput{
		Kind: string(status.Kind), PID: status.PID, Endpoint: status.Endpoint,
		Health: status.Health, Detail: status.Detail, LogFile: status.LogFile,
		Executable: session.Preparation.Plan.Executable, Model: session.Preparation.Plan.State.Model,
		Argv: append([]string{session.Preparation.Plan.Executable}, session.Preparation.Plan.Args...),
	}
	if status.Timings != nil || session.ColdStartMS != nil {
		output.Timings = make(map[string]float64)
		if status.Timings != nil {
			output.Timings["timeToHealthMs"] = status.Timings.TimeToHealthMS
		}
		if session.ColdStartMS != nil {
			output.Timings["coldStartMs"] = *session.ColdStartMS
		}
	}
	return output
}

func newDemoOutput(
	session runSession,
	result string,
	requestMS float64,
	generationTiming *endpoint.GenerationTiming,
) demoOutput {
	timings := map[string]float64{"requestMs": requestMS}
	if session.ColdStartMS != nil {
		timings["coldStartMs"] = *session.ColdStartMS
	}
	if session.Status.Timings != nil {
		timings["timeToHealthMs"] = session.Status.Timings.TimeToHealthMS
	}
	cleanup := "preserved-existing"
	if shouldCleanup(session) {
		cleanup = "stopped"
	}
	output := demoOutput{
		Endpoint: session.Preparation.Plan.Endpoint,
		Model:    newModelOutput(session.Preparation.Plan.Profile.Model),
		Result:   result, Timings: timings, LogFile: session.Preparation.Plan.State.Log,
		Cleanup: cleanup,
	}
	if generationTiming != nil {
		output.Timings["generationTokensPerSecond"] = generationTiming.TokensPerSecond
		output.GenerationTimingSource = generationTiming.Source
	}
	return output
}
