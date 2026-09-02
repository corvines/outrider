package main

import "testing"

func TestCurrentVersionUsesLinkedValues(t *testing.T) {
	previousVersion, previousCommit, previousDate := buildVersion, buildCommit, buildDate
	t.Cleanup(func() {
		buildVersion, buildCommit, buildDate = previousVersion, previousCommit, previousDate
	})
	buildVersion = "1.2.3"
	buildCommit = "abcdef"
	buildDate = "2026-09-02T00:00:00Z"
	version := currentVersion()
	if version.Version != buildVersion || version.Commit != buildCommit || version.Date != buildDate {
		t.Fatalf("version = %#v", version)
	}
}
