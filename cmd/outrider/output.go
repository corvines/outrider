package main

import (
	"path/filepath"

	"github.com/corvines/outrider/internal/admission"
	"github.com/corvines/outrider/internal/catalog"
	"github.com/corvines/outrider/internal/endpoint"
	"github.com/corvines/outrider/internal/kvstate"
	"github.com/corvines/outrider/internal/manifest"
	"github.com/corvines/outrider/internal/ollamacache"
	runnerprocess "github.com/corvines/outrider/internal/process"
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

type profileCacheOutput struct {
	State     string `json:"state"`
	Path      string `json:"path"`
	SizeBytes int64  `json:"sizeBytes,omitempty"`
}

type profileSummaryOutput struct {
	ID              string             `json:"id"`
	Runnable        bool               `json:"runnable"`
	Dev             bool               `json:"dev,omitempty"`
	Description     string             `json:"description"`
	Capabilities    []string           `json:"capabilities"`
	Model           modelOutput        `json:"model"`
	SizeBytes       int64              `json:"sizeBytes,omitempty"`
	Context         int                `json:"context"`
	TrainingContext int                `json:"trainingContext,omitempty"`
	MinMemoryMiB    int                `json:"minMemoryMiB,omitempty"`
	Speculation     string             `json:"speculation,omitempty"`
	MTP             bool               `json:"mtp"`
	Cache           profileCacheOutput `json:"cache"`
}

type profileListOutput struct {
	Profiles          []profileSummaryOutput `json:"profiles"`
	DevelopmentModels []ollamacache.Model    `json:"developmentModels"`
}

type profileDetailOutput struct {
	Profile manifest.Profile   `json:"profile"`
	Cache   profileCacheOutput `json:"cache"`
}

type pullOutput struct {
	Profile   string           `json:"profile"`
	Runtime   string           `json:"runtime"`
	Model     string           `json:"model"`
	SizeBytes int64            `json:"sizeBytes"`
	Admission admission.Report `json:"admission"`
}

type logOutput struct {
	LogFile string   `json:"logFile"`
	Lines   []string `json:"lines"`
}

type serviceStatusOutput struct {
	Gateway runnerprocess.Status `json:"gateway"`
	Model   runnerprocess.Status `json:"model"`
}

type useOutput struct {
	Profile  string               `json:"profile"`
	Endpoint string               `json:"endpoint"`
	Model    runnerprocess.Status `json:"model"`
}

type installOutput struct {
	Status        string `json:"status"`
	Target        string `json:"target"`
	Marker        string `json:"marker,omitempty"`
	SHA256        string `json:"sha256,omitempty"`
	Link          string `json:"link,omitempty"`
	StateRoot     string `json:"stateRoot,omitempty"`
	StateBytes    int64  `json:"stateBytes,omitempty"`
	StateRemoved  bool   `json:"stateRemoved,omitempty"`
	StatePrompted bool   `json:"statePrompted,omitempty"`
}

type versionOutput struct {
	Version string `json:"version"`
	Commit  string `json:"commit,omitempty"`
	Date    string `json:"date,omitempty"`
	Dirty   bool   `json:"dirty,omitempty"`
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
	SlotDirectory string `json:"slotDirectory"`
	SlotSnapshot  string `json:"slotSnapshot"`
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
	Profile    string             `json:"profile"`
	PID        int                `json:"pid,omitempty"`
	Endpoint   string             `json:"endpoint"`
	Health     *bool              `json:"health,omitempty"`
	Detail     string             `json:"detail,omitempty"`
	LogFile    string             `json:"logFile"`
	Timings    map[string]float64 `json:"timings,omitempty"`
	Executable string             `json:"executable"`
	Model      string             `json:"model"`
	Argv       []string           `json:"argv"`
	Admission  admission.Report   `json:"admission"`
	Session    *kvstate.Result    `json:"session,omitempty"`
}

type demoOutput struct {
	Endpoint               string             `json:"endpoint"`
	Model                  modelOutput        `json:"model"`
	Result                 string             `json:"result"`
	Timings                map[string]float64 `json:"timings"`
	GenerationTimingSource string             `json:"generationTimingSource,omitempty"`
	LogFile                string             `json:"logFile"`
	Cleanup                string             `json:"cleanup"`
	Admission              admission.Report   `json:"admission"`
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
			SlotDirectory: plan.State.Slots,
			SlotSnapshot:  filepath.Join(plan.State.Slots, plan.Session.Filename),
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

func newProfileSummary(profile manifest.Profile, state manifest.StatePaths) (profileSummaryOutput, error) {
	weights, err := catalog.InspectWeights(profile, state.Model)
	if err != nil {
		return profileSummaryOutput{}, err
	}
	return newProfileSummaryFromEntry(catalog.FromProfile(profile, weights)), nil
}

func newProfileSummaryFromEntry(entry catalog.Entry) profileSummaryOutput {
	return profileSummaryOutput{
		ID: entry.ID, Runnable: entry.Runnable, Dev: entry.Dev, Description: entry.Description,
		Capabilities: entry.Capabilities,
		Model: modelOutput{
			Repository: entry.Repo, File: entry.File, Quant: entry.Quant, LocalPath: entry.LocalPath,
		},
		SizeBytes: entry.Weights.DeclaredBytes, Context: entry.Context,
		TrainingContext: entry.TrainingContext, MinMemoryMiB: entry.MinMemoryMiB,
		Speculation: entry.Speculation, MTP: entry.Speculation == "mtp",
		Cache: newProfileCache(entry.Weights),
	}
}

func newProfileCache(weights catalog.Weights) profileCacheOutput {
	return profileCacheOutput{
		State: weights.State, Path: weights.Path, SizeBytes: weights.OnDiskBytes,
	}
}

func inspectProfileCache(profile manifest.Profile, path string) (profileCacheOutput, error) {
	weights, err := catalog.InspectWeights(profile, path)
	if err != nil {
		return profileCacheOutput{}, err
	}
	return newProfileCache(weights), nil
}

func newUpOutput(session runSession) upOutput {
	status := session.Status
	output := upOutput{
		Kind: string(status.Kind), Profile: session.Preparation.Profile.ID,
		PID: status.PID, Endpoint: status.Endpoint,
		Health: status.Health, Detail: status.Detail, LogFile: status.LogFile,
		Executable: session.Preparation.Plan.Executable, Model: session.Preparation.Plan.State.Model,
		Argv:      append([]string{session.Preparation.Plan.Executable}, session.Preparation.Plan.Args...),
		Admission: session.Preparation.Admission, Session: status.Session,
	}
	if status.Timings != nil || session.ColdStartMS != nil || session.Preparation.TotalReadyMS > 0 {
		output.Timings = make(map[string]float64)
		if status.Timings != nil {
			output.Timings["timeToHealthMs"] = status.Timings.TimeToHealthMS
		}
		if session.ColdStartMS != nil {
			output.Timings["coldStartMs"] = *session.ColdStartMS
		}
		addPreparationTimings(output.Timings, session.Preparation)
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
	addPreparationTimings(timings, session.Preparation)
	cleanup := "preserved-existing"
	if shouldCleanup(session) {
		cleanup = "stopped"
	}
	output := demoOutput{
		Endpoint: session.Preparation.Plan.Endpoint,
		Model:    newModelOutput(session.Preparation.Plan.Profile.Model),
		Result:   result, Timings: timings, LogFile: session.Preparation.Plan.State.Log,
		Cleanup:   cleanup,
		Admission: session.Preparation.Admission,
	}
	if generationTiming != nil {
		output.Timings["generationTokensPerSecond"] = generationTiming.TokensPerSecond
		output.GenerationTimingSource = generationTiming.Source
	}
	return output
}

func addPreparationTimings(timings map[string]float64, preparation runPreparation) {
	if preparation.RuntimePreparationMS > 0 {
		timings["runtimePreparationMs"] = preparation.RuntimePreparationMS
	}
	if preparation.ModelPreparationMS > 0 {
		timings["modelPreparationMs"] = preparation.ModelPreparationMS
	}
	if preparation.RuntimeDownloadMS > 0 {
		timings["runtimeDownloadMs"] = preparation.RuntimeDownloadMS
	}
	if preparation.ModelDownloadMS > 0 {
		timings["modelDownloadMs"] = preparation.ModelDownloadMS
	}
	if preparation.TotalReadyMS > 0 {
		timings["totalReadyMs"] = preparation.TotalReadyMS
	}
}
