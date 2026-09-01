package capabilities

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"

	"github.com/corvines/outrider/internal/manifest"
)

var (
	helpFlagPattern = regexp.MustCompile(`(?:^|[\s,])(-{1,2}[A-Za-z][A-Za-z0-9_-]*)`)
	argumentPattern = regexp.MustCompile(`^--?[A-Za-z][A-Za-z0-9_-]*$`)
)

type CommandResult struct {
	ExitCode int
	Stdout   string
	Stderr   string
}

type CommandRunner func(context.Context, []string, string) (CommandResult, error)

type Capabilities struct {
	Executable string
	HelpText   string
	Flags      map[string]struct{}
}

type CapabilityError struct {
	Message string
	Cause   error
}

func (e *CapabilityError) Error() string {
	return e.Message
}

func (e *CapabilityError) Unwrap() error {
	return e.Cause
}

func ParseHelpFlags(helpText string) []string {
	matches := helpFlagPattern.FindAllStringSubmatch(helpText, -1)
	flags := make([]string, 0, len(matches))
	seen := make(map[string]struct{}, len(matches))
	for _, match := range matches {
		flag := match[1]
		if _, exists := seen[flag]; exists {
			continue
		}
		seen[flag] = struct{}{}
		flags = append(flags, flag)
	}
	return flags
}

func Probe(ctx context.Context, executable string, runner CommandRunner) (Capabilities, error) {
	if runner == nil {
		runner = RunCommand
	}
	result, err := runner(ctx, []string{executable, "--help"}, "")
	if err != nil {
		return Capabilities{}, &CapabilityError{
			Message: fmt.Sprintf("could not probe %s --help: %v", executable, err),
			Cause:   err,
		}
	}
	helpText := result.Stdout + "\n" + result.Stderr
	if result.ExitCode != 0 || strings.TrimSpace(helpText) == "" {
		detail := firstUsefulLine(result.Stderr)
		if detail == "" {
			detail = "no help output"
		}
		return Capabilities{}, &CapabilityError{Message: fmt.Sprintf(
			"%s --help failed with exit code %d: %s", executable, result.ExitCode, detail,
		)}
	}
	parsed := ParseHelpFlags(helpText)
	if len(parsed) == 0 {
		return Capabilities{}, &CapabilityError{Message: fmt.Sprintf("%s --help did not advertise any flags", executable)}
	}
	flags := make(map[string]struct{}, len(parsed))
	for _, flag := range parsed {
		flags[flag] = struct{}{}
	}
	return Capabilities{Executable: executable, HelpText: helpText, Flags: flags}, nil
}

func Assert(capabilities Capabilities, argv []string) error {
	missing := make([]string, 0)
	seen := make(map[string]struct{})
	for _, token := range argv {
		if !argumentPattern.MatchString(token) {
			continue
		}
		if _, supported := capabilities.Flags[token]; supported {
			continue
		}
		if _, exists := seen[token]; exists {
			continue
		}
		seen[token] = struct{}{}
		missing = append(missing, token)
	}
	if len(missing) > 0 {
		return &CapabilityError{Message: fmt.Sprintf(
			"%s does not support required flag(s): %s", capabilities.Executable, strings.Join(missing, ", "),
		)}
	}

	for i, token := range argv {
		if token != "--spec-type" {
			continue
		}
		if i+1 >= len(argv) {
			return &manifest.ManifestError{Field: "--spec-type", Message: "is missing its value"}
		}
		for _, specType := range strings.Split(argv[i+1], ",") {
			if !strings.Contains(capabilities.HelpText, specType) {
				return &CapabilityError{Message: fmt.Sprintf(
					"%s does not advertise speculative type %q", capabilities.Executable, specType,
				)}
			}
		}
		break
	}
	return nil
}

func RunCommand(ctx context.Context, argv []string, cwd string) (CommandResult, error) {
	if len(argv) == 0 {
		return CommandResult{}, errors.New("command is empty")
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = cwd
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	result := CommandResult{Stdout: stdout.String(), Stderr: stderr.String()}
	if err == nil {
		return result, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		result.ExitCode = exitErr.ExitCode()
		return result, nil
	}
	return CommandResult{}, err
}

func firstUsefulLine(value string) string {
	for _, line := range strings.Split(value, "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line != "" {
			return line
		}
	}
	return ""
}
