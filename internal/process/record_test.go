package process

import (
	"os/exec"
	"testing"
)

func TestProcessIdentitySurvivesTimezoneChange(t *testing.T) {
	command := exec.Command("sleep", "30")
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = command.Process.Kill(); _ = command.Wait() })
	t.Setenv("TZ", "America/Denver")
	before := inspectProcess(command.Process.Pid)
	if before == nil {
		t.Fatal("could not inspect child")
	}
	t.Setenv("TZ", "Asia/Tokyo")
	after := inspectProcess(command.Process.Pid)
	if after == nil {
		t.Fatal("could not inspect child after timezone change")
	}
	if before.ProcessStartedAt == after.ProcessStartedAt {
		t.Fatal("timezone control did not change local start time")
	}
	record := ProcessRecord{
		ProcessStartedAt: before.ProcessStartedAt, ProcessStartedUTC: before.ProcessStartedUTC,
		Command: before.Command, Argv: []string{"sleep", "30"},
	}
	record.ArgvSHA256 = ArgvSHA256(record.Argv)
	if !IdentityMatches(record, *after) {
		t.Fatalf("identity changed: %#v, %#v", before, after)
	}
	changed := *after
	changed.ProcessStartedUTC = "different start"
	if IdentityMatches(record, changed) {
		t.Fatal("accepted different start")
	}
	changed = *after
	changed.ProcessStartedUTC = ""
	if IdentityMatches(record, changed) {
		t.Fatal("accepted missing UTC observation")
	}
	changed = *after
	changed.Command = "different command"
	if IdentityMatches(record, changed) {
		t.Fatal("accepted different command")
	}
	record.ArgvSHA256 = "wrong hash"
	if IdentityMatches(record, *after) {
		t.Fatal("accepted invalid argv hash")
	}
	record.ArgvSHA256 = ArgvSHA256(record.Argv)
	record.ProcessStartedUTC = ""
	if !IdentityMatches(record, *before) {
		t.Fatal("legacy identity no longer works")
	}
	if IdentityMatches(record, *after) {
		t.Fatal("legacy identity was weakened")
	}
}
