package process

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/corvines/outrider/internal/llama"
)

const ProcessRecordSchemaVersion = 1

type ProcessRecord struct {
	SchemaVersion    int      `json:"schemaVersion"`
	PID              int      `json:"pid"`
	StartedAt        string   `json:"startedAt"`
	ProcessStartedAt string   `json:"processStartedAt"`
	Executable       string   `json:"executable"`
	Command          string   `json:"command"`
	Argv             []string `json:"argv"`
	ArgvSHA256       string   `json:"argvSha256"`
	Preset           string   `json:"preset"`
	Port             int      `json:"port"`
	LogFile          string   `json:"logFile"`
	SessionEnabled   bool     `json:"sessionEnabled,omitempty"`
	SessionSlot      int      `json:"sessionSlot,omitempty"`
	SessionKey       string   `json:"sessionKey,omitempty"`
	SessionDirectory string   `json:"sessionDirectory,omitempty"`
	SessionFilename  string   `json:"sessionFilename,omitempty"`
}

type ObservedProcess struct {
	ProcessStartedAt string
	Command          string
}

func ArgvSHA256(argv []string) string {
	var encoded bytes.Buffer
	encoder := json.NewEncoder(&encoded)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(argv); err != nil {
		panic(err)
	}
	digest := sha256.Sum256(bytes.TrimSuffix(encoded.Bytes(), []byte("\n")))
	return hex.EncodeToString(digest[:])
}

func IdentityMatches(record ProcessRecord, observed ObservedProcess) bool {
	return record.ProcessStartedAt == observed.ProcessStartedAt &&
		record.Command == observed.Command &&
		record.ArgvSHA256 == ArgvSHA256(record.Argv)
}

func ReadProcessRecord(path string) (*ProcessRecord, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, runnerError(fmt.Sprintf("cannot read process record %s", path), err)
	}
	var record ProcessRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return nil, runnerError(fmt.Sprintf("cannot read process record %s", path), err)
	}
	if !validProcessRecord(record) {
		return nil, runnerErrorf("invalid process record; refusing to act on %s", path)
	}
	return &record, nil
}

func writeProcessRecord(path string, record ProcessRecord) error {
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return runnerError("could not encode process record", err)
	}
	data = append(data, '\n')
	directory := filepath.Dir(path)
	file, err := os.CreateTemp(directory, ".pid.json.tmp-")
	if err != nil {
		return runnerError("could not create temporary process record", err)
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	if err := file.Chmod(0o600); err != nil {
		file.Close()
		return runnerError("could not protect temporary process record", err)
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return runnerError("could not write temporary process record", err)
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return runnerError("could not sync temporary process record", err)
	}
	if err := file.Close(); err != nil {
		return runnerError("could not close temporary process record", err)
	}
	if err := os.Rename(temporary, path); err != nil {
		return runnerError("could not install process record", err)
	}
	return nil
}

func inspectProcess(pid int) *ObservedProcess {
	startedAt := ps(pid, "lstart=")
	command := ps(pid, "command=")
	if startedAt == "" || command == "" {
		return nil
	}
	return &ObservedProcess{ProcessStartedAt: strings.TrimSpace(startedAt), Command: strings.TrimSpace(command)}
}

func ps(pid int, field string) string {
	result, err := exec.Command("ps", "-ww", "-p", strconv.Itoa(pid), "-o", field).Output()
	if err != nil {
		return ""
	}
	return string(result)
}

func validProcessRecord(record ProcessRecord) bool {
	return record.SchemaVersion == ProcessRecordSchemaVersion &&
		record.PID > 0 &&
		record.StartedAt != "" &&
		record.ProcessStartedAt != "" &&
		record.Executable != "" &&
		record.Command != "" &&
		len(record.Argv) > 0 &&
		record.ArgvSHA256 != "" &&
		record.Preset != "" &&
		record.Port > 0 && record.Port <= 65535 &&
		record.LogFile != "" && validSessionRecord(record)
}

func validSessionRecord(record ProcessRecord) bool {
	if !record.SessionEnabled {
		return true
	}
	decodedKey, err := hex.DecodeString(record.SessionKey)
	return err == nil && len(decodedKey) == sha256.Size && record.SessionKey == strings.ToLower(record.SessionKey) &&
		record.SessionSlot >= 0 &&
		filepath.IsAbs(record.SessionDirectory) &&
		record.SessionFilename == "slot-"+record.SessionKey+".bin"
}

func identityMismatchError(record ProcessRecord, observed ObservedProcess) error {
	return runnerErrorf(
		"refusing to act on PID %d: process identity mismatch (expected start %s and command %q, observed start %s and command %q)",
		record.PID, record.ProcessStartedAt, record.Command, observed.ProcessStartedAt, observed.Command,
	)
}

func runnerError(message string, cause error) error {
	return &llama.RunnerError{Message: message + ": " + cause.Error(), Cause: cause}
}

func runnerErrorf(format string, args ...any) error {
	return &llama.RunnerError{Message: fmt.Sprintf(format, args...)}
}
