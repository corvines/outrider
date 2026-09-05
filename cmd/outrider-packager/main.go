package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/corvines/outrider/internal/guide"
	"github.com/corvines/outrider/internal/installer"
)

const usage = `outrider-packager

  outrider-packager stage --binary PATH --root PATH
  outrider-packager verify --root PATH
  outrider-packager uninstall --root PATH
  outrider-packager build --binary PATH --output PATH [options]

Build options:
  --version VERSION
  --application-identity IDENTITY
  --installer-identity IDENTITY
  --notary-profile KEYCHAIN_PROFILE
`

type buildOptions struct {
	Binary              string
	Output              string
	Version             string
	ApplicationIdentity string
	InstallerIdentity   string
	NotaryProfile       string
}

type buildResult struct {
	DMG        string `json:"dmg"`
	Package    string `json:"package"`
	Signed     bool   `json:"signed"`
	Notarized  bool   `json:"notarized"`
	MarkerHash string `json:"markerHash"`
}

func main() {
	if len(os.Args) < 2 {
		fail(errors.New(usage))
	}
	var result any
	var err error
	switch os.Args[1] {
	case "stage":
		result, err = runStage(os.Args[2:])
	case "verify":
		result, err = runVerify(os.Args[2:])
	case "uninstall":
		err = runUninstall(os.Args[2:])
		result = map[string]string{"status": "uninstalled"}
	case "build":
		result, err = runBuild(os.Args[2:])
	default:
		err = fmt.Errorf("unknown command %q\n\n%s", os.Args[1], usage)
	}
	if err != nil {
		fail(err)
	}
	encoded, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		fail(err)
	}
	_, _ = os.Stdout.Write(append(encoded, '\n'))
}

func runStage(arguments []string) (installer.Marker, error) {
	flags := flag.NewFlagSet("stage", flag.ContinueOnError)
	binary := flags.String("binary", "", "binary to stage")
	root := flags.String("root", "", "package root")
	if err := flags.Parse(arguments); err != nil {
		return installer.Marker{}, err
	}
	if *binary == "" || *root == "" || flags.NArg() != 0 {
		return installer.Marker{}, errors.New("stage requires --binary and --root")
	}
	return installer.Stage(*binary, *root)
}

func runVerify(arguments []string) (installer.Marker, error) {
	flags := flag.NewFlagSet("verify", flag.ContinueOnError)
	root := flags.String("root", "", "package root")
	if err := flags.Parse(arguments); err != nil {
		return installer.Marker{}, err
	}
	if *root == "" || flags.NArg() != 0 {
		return installer.Marker{}, errors.New("verify requires --root")
	}
	return installer.Verify(*root)
}

func runUninstall(arguments []string) error {
	flags := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	root := flags.String("root", "", "installed root")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if *root == "" || flags.NArg() != 0 {
		return errors.New("uninstall requires --root")
	}
	return installer.Uninstall(*root)
}

func runBuild(arguments []string) (buildResult, error) {
	flags := flag.NewFlagSet("build", flag.ContinueOnError)
	options := buildOptions{}
	flags.StringVar(&options.Binary, "binary", "", "binary to package")
	flags.StringVar(&options.Output, "output", "", "destination DMG")
	flags.StringVar(&options.Version, "version", "0.1.0", "package version")
	flags.StringVar(&options.ApplicationIdentity, "application-identity", "", "Developer ID Application identity")
	flags.StringVar(&options.InstallerIdentity, "installer-identity", "", "Developer ID Installer identity")
	flags.StringVar(&options.NotaryProfile, "notary-profile", "", "notarytool keychain profile")
	if err := flags.Parse(arguments); err != nil {
		return buildResult{}, err
	}
	if options.Binary == "" || options.Output == "" || flags.NArg() != 0 {
		return buildResult{}, errors.New("build requires --binary and --output")
	}
	if options.NotaryProfile != "" && (options.ApplicationIdentity == "" || options.InstallerIdentity == "") {
		return buildResult{}, errors.New("notarization requires both Developer ID identities")
	}
	return buildPackage(options)
}

func buildPackage(options buildOptions) (buildResult, error) {
	output, err := filepath.Abs(options.Output)
	if err != nil {
		return buildResult{}, err
	}
	if _, err := os.Lstat(output); err == nil {
		return buildResult{}, fmt.Errorf("refusing to overwrite existing output: %s", output)
	} else if !os.IsNotExist(err) {
		return buildResult{}, err
	}
	tools := []string{"codesign", "pkgbuild", "productbuild", "hdiutil"}
	if options.InstallerIdentity != "" {
		tools = append(tools, "pkgutil")
	}
	if options.NotaryProfile != "" {
		tools = append(tools, "xcrun")
	}
	for _, tool := range tools {
		if _, err := exec.LookPath(tool); err != nil {
			return buildResult{}, fmt.Errorf("required packaging tool %s is unavailable", tool)
		}
	}
	workspace, err := os.MkdirTemp("", "outrider-package-")
	if err != nil {
		return buildResult{}, err
	}
	defer os.RemoveAll(workspace)
	prepared := filepath.Join(workspace, "outrider")
	if err := copyBinary(options.Binary, prepared); err != nil {
		return buildResult{}, err
	}
	shippedGuide := installer.FindGuide(options.Binary)
	if shippedGuide == "" {
		return buildResult{}, fmt.Errorf("no %s found beside %s", guide.Filename, options.Binary)
	}
	if err := copyFile(shippedGuide, filepath.Join(workspace, guide.Filename)); err != nil {
		return buildResult{}, err
	}
	identity := options.ApplicationIdentity
	if identity == "" {
		identity = "-"
	}
	if err := command("codesign", binarySignArguments(identity, prepared)...); err != nil {
		return buildResult{}, err
	}
	if err := command("codesign", "--verify", "--strict", prepared); err != nil {
		return buildResult{}, err
	}
	payload := filepath.Join(workspace, "payload")
	marker, err := installer.Stage(prepared, payload)
	if err != nil {
		return buildResult{}, err
	}
	componentPackage := filepath.Join(workspace, "Outrider-component.pkg")
	if err := command(
		"pkgbuild", "--root", payload, "--identifier", installer.PackageID,
		"--version", options.Version, "--install-location", "/", componentPackage,
	); err != nil {
		return buildResult{}, err
	}
	diskRoot := filepath.Join(workspace, "disk")
	if err := os.MkdirAll(diskRoot, 0o755); err != nil {
		return buildResult{}, err
	}
	productPackage := filepath.Join(diskRoot, "Install Outrider.pkg")
	productArguments := []string{"--package", componentPackage}
	if options.InstallerIdentity != "" {
		productArguments = append(productArguments, "--sign", options.InstallerIdentity)
	}
	productArguments = append(productArguments, productPackage)
	if err := command("productbuild", productArguments...); err != nil {
		return buildResult{}, err
	}
	if options.InstallerIdentity != "" {
		if err := command("pkgutil", "--check-signature", productPackage); err != nil {
			return buildResult{}, err
		}
	}
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		return buildResult{}, err
	}
	if err := command(
		"hdiutil", "create", "-volname", "Outrider", "-srcfolder", diskRoot,
		"-format", "UDZO", output,
	); err != nil {
		return buildResult{}, err
	}
	if err := command("codesign", diskImageSignArguments(identity, output)...); err != nil {
		return buildResult{}, err
	}
	if err := command("codesign", "--verify", "--strict", output); err != nil {
		return buildResult{}, err
	}
	if err := command("hdiutil", "verify", output); err != nil {
		return buildResult{}, err
	}
	notarized := false
	if options.NotaryProfile != "" {
		if err := command(
			"xcrun", "notarytool", "submit", output,
			"--keychain-profile", options.NotaryProfile, "--wait",
		); err != nil {
			return buildResult{}, err
		}
		if err := command("xcrun", "stapler", "staple", output); err != nil {
			return buildResult{}, err
		}
		if err := command("xcrun", "stapler", "validate", output); err != nil {
			return buildResult{}, err
		}
		notarized = true
	}
	return buildResult{
		DMG: output, Package: filepath.Base(productPackage),
		Signed:    options.ApplicationIdentity != "" && options.InstallerIdentity != "",
		Notarized: notarized, MarkerHash: marker.SHA256,
	}, nil
}

func binarySignArguments(identity string, target string) []string {
	arguments := []string{"--force", "--sign", identity}
	if identity == "-" {
		arguments = append(arguments, "--timestamp=none")
	} else {
		arguments = append(arguments, "--options", "runtime", "--timestamp")
	}
	return append(arguments, target)
}

func diskImageSignArguments(identity string, target string) []string {
	arguments := []string{"--force", "--sign", identity}
	if identity == "-" {
		arguments = append(arguments, "--timestamp=none")
	} else {
		arguments = append(arguments, "--timestamp")
	}
	return append(arguments, target)
}

func copyFile(source string, destination string) error {
	contents, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	return os.WriteFile(destination, contents, 0o644)
}

func copyBinary(source string, destination string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(output, input); err != nil {
		output.Close()
		return err
	}
	return output.Close()
}

func command(name string, arguments ...string) error {
	cmd := exec.Command(name, arguments...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s failed: %w", name, err)
	}
	return nil
}

func fail(err error) {
	_, _ = fmt.Fprintf(os.Stderr, "outrider-packager: %v\n", err)
	os.Exit(1)
}
