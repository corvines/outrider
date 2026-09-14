package admission

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"testing"

	"github.com/corvines/outrider/internal/manifest"
)

func TestCachedArtifactsRequiresProjector(t *testing.T) {
	profile, err := manifest.Get("qwen35b-mtp")
	if err != nil {
		t.Fatal(err)
	}
	data := []byte("GGUFtest")
	profile.Model.SizeBytes = int64(len(data))
	profile.Model.SHA256 = fmt.Sprintf("%x", sha256.Sum256(data))
	profile.MultimodalProject.SizeBytes = profile.Model.SizeBytes
	profile.MultimodalProject.SHA256 = profile.Model.SHA256
	plan, err := manifest.ResolveCached(profile, manifest.ResolveOptions{Root: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(plan.State.Models, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(plan.State.Model, data, 0o600); err != nil {
		t.Fatal(err)
	}
	base := Report{Profile: profile.ID, Class: ClassReady}
	report := WithCachedArtifacts(context.Background(), base, plan, nil)
	if !report.Blocking() {
		t.Fatalf("missing projector passed: %#v", report)
	}
	if checkByID(t, report, "cached_model").Result != ResultPass {
		t.Fatal("model failed")
	}
	check := checkByID(t, report, "cached_"+manifest.RoleProjector)
	if check.Result != ResultFail || check.NextAction == "" {
		t.Fatalf("projector = %#v", check)
	}
	if err := os.WriteFile(plan.State.Artifacts[manifest.RoleProjector], data, 0o600); err != nil {
		t.Fatal(err)
	}
	report = WithCachedArtifacts(context.Background(), base, plan, nil)
	if report.Class != ClassReady {
		t.Fatalf("complete files = %#v", report)
	}
}
