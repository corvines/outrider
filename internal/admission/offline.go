package admission

import (
	"context"
	"fmt"
	"slices"

	"github.com/corvines/outrider/internal/llama"
	"github.com/corvines/outrider/internal/manifest"
)

func WithCachedArtifacts(ctx context.Context, report Report, plan manifest.Plan, progress llama.ProgressFunc) Report {
	artifacts := plan.Profile.Artifacts()
	roles := make([]string, 0, len(artifacts))
	for role := range artifacts {
		roles = append(roles, role)
	}
	slices.Sort(roles)
	for _, role := range roles {
		path := plan.State.Artifacts[role]
		check := Check{
			ID: "cached_" + role, Result: ResultPass,
			Measured: path + ": size and SHA-256 verified",
			Required: "complete local GGUF matching the profile digest and size",
		}
		if err := llama.VerifyCachedArtifact(ctx, artifacts[role], path, progress); err != nil {
			check.Result = ResultFail
			check.Measured = err.Error()
			check.NextAction = fmt.Sprintf("while online, run outrider pull %s; if the cache is damaged, follow its repair message and retry", plan.Profile.ID)
			check.class = ClassBlocked
		}
		report.add(check)
		if ctx.Err() != nil {
			break
		}
	}
	return report
}
