package main

import (
	"os"
	"runtime/debug"
)

var (
	buildVersion = "dev"
	buildCommit  = ""
	buildDate    = ""
)

func currentExecutable() (string, error) {
	return os.Executable()
}

func currentVersion() versionOutput {
	output := versionOutput{Version: buildVersion, Commit: buildCommit, Date: buildDate}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return output
	}
	if output.Version == "dev" && info.Main.Version != "" && info.Main.Version != "(devel)" {
		output.Version = info.Main.Version
	}
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			if output.Commit == "" {
				output.Commit = setting.Value
			}
		case "vcs.time":
			if output.Date == "" {
				output.Date = setting.Value
			}
		case "vcs.modified":
			output.Dirty = setting.Value == "true"
		}
	}
	return output
}
