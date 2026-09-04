package manifest

import (
	"path/filepath"
	"testing"
)

// The projector needs its own cache path, and the launch flag must point at
// that path rather than at a bare filename in the working directory.
func TestCachedPlanPointsMmprojAtTheCache(t *testing.T) {
	profile, err := Get("qwen35-2b")
	if err != nil {
		t.Fatal(err)
	}
	if profile.MultimodalProject == nil {
		t.Skip("profile declares no projector")
	}
	root := t.TempDir()
	plan, err := ResolveCached(profile, ResolveOptions{Root: root, Executable: "/fake/llama-server"})
	if err != nil {
		t.Fatal(err)
	}
	projector := plan.State.Artifacts[RoleProjector]
	if projector == "" || !filepath.IsAbs(projector) {
		t.Fatalf("projector cache path = %q", projector)
	}
	if projector == plan.State.Model {
		t.Fatal("the projector shares the model's cache path")
	}
	index := -1
	for position, arg := range plan.Args {
		if arg == "--mmproj" {
			index = position
		}
	}
	if index < 0 {
		t.Fatal("--mmproj is absent")
	}
	if plan.Args[index+1] != projector {
		t.Fatalf("--mmproj = %q, want %q", plan.Args[index+1], projector)
	}
}

// Every artifact the profile declares gets a cache path, so nothing is left
// for a caller to guess at.
func TestPathsCoverEveryDeclaredArtifact(t *testing.T) {
	profile, err := Get("qwen35b-mtp")
	if err != nil {
		t.Fatal(err)
	}
	state, err := Paths(t.TempDir(), profile, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Artifacts) != len(profile.Artifacts()) {
		t.Fatalf("artifacts = %v", state.Artifacts)
	}
	if state.Artifacts[RoleModel] != state.Model {
		t.Fatalf("model = %q, artifacts[model] = %q", state.Model, state.Artifacts[RoleModel])
	}
	for role, path := range state.Artifacts {
		if path == "" {
			t.Fatalf("role %q has no cache path", role)
		}
	}
}
