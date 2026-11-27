package teleport

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// The sources a tsh binary can come from, in the order they are tried (ADR 0081).
const (
	SourceEnv     = "env"     // PANTAU_TSH_BIN names the binary
	SourceManaged = "managed" // Pantau installed it into <data>/cli
	SourceSystem  = "system"  // found on PATH
	SourceMissing = "missing" // no tsh anywhere
)

// managedTSHName is the file name of the managed tsh inside the CLI directory (<data>/cli/tsh).
const managedTSHName = "tsh"

var (
	releaseVersion = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)
	versionInText  = regexp.MustCompile(`[0-9]+\.[0-9]+\.[0-9]+`)
)

// TSHLocation is the tsh binary Pantau runs and the source it was taken from.
type TSHLocation struct {
	Source string
	Path   string
}

// ResolveTSH picks the tsh binary to run, in this order:
//
//  1. override (PANTAU_TSH_BIN): an absolute path to an executable. When set it always wins, and a bad
//     value is an error, never a silent fallback to another binary.
//  2. the managed binary cliDir/tsh, when Pantau installed one and it is executable.
//  3. tsh on PATH. lookPath is nil in production (exec.LookPath) and replaced in tests.
//
// No binary anywhere returns ErrTSHNotInstalled with Source "missing". Nothing is downloaded here.
func ResolveTSH(override, cliDir string, lookPath func(string) (string, error)) (TSHLocation, error) {
	if override != "" {
		if strings.ContainsAny(override, "\x00\r\n") || !filepath.IsAbs(override) {
			return TSHLocation{Source: SourceMissing}, fmt.Errorf("%w: tsh override must be an absolute path", ErrInvalidInput)
		}
		if !isExecutableFile(override) {
			return TSHLocation{Source: SourceMissing}, fmt.Errorf("%w: tsh override %q is not an executable file", ErrTSHNotInstalled, override)
		}
		return TSHLocation{Source: SourceEnv, Path: override}, nil
	}
	if cliDir != "" {
		if p := filepath.Join(cliDir, managedTSHName); isExecutableFile(p) {
			return TSHLocation{Source: SourceManaged, Path: p}, nil
		}
	}
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	path, err := lookPath("tsh")
	if err != nil {
		return TSHLocation{Source: SourceMissing}, ErrTSHNotInstalled
	}
	return TSHLocation{Source: SourceSystem, Path: path}, nil
}

// isExecutableFile reports whether p is a regular file with an execute bit set.
func isExecutableFile(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir() && info.Mode()&0o111 != 0
}

// ParseTSHVersion reads the client version from `tsh version --client --format=json`, which prints
// {"version":"18.11.10",...}. When the output is not that JSON (an older tsh, or the text form
// "Teleport v18.11.10 git:..."), the first MAJOR.MINOR.PATCH on the first line is used.
func ParseTSHVersion(raw []byte) (string, bool) {
	var doc struct {
		Version string `json:"version"`
	}
	if json.Unmarshal(raw, &doc) == nil && releaseVersion.MatchString(doc.Version) {
		return doc.Version, true
	}
	line := raw
	if i := bytes.IndexByte(line, '\n'); i >= 0 {
		line = line[:i]
	}
	if v := versionInText.Find(line); v != nil {
		return string(v), true
	}
	return "", false
}
