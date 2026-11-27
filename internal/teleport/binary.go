package teleport

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// LocateTSH returns the tsh binary to run. When override is set it must be an absolute path to an executable
// file; otherwise tsh is looked up on PATH. A missing tsh returns ErrTSHNotInstalled. Nothing is downloaded.
// lookPath is nil in production and replaced in tests.
func LocateTSH(override string, lookPath func(string) (string, error)) (string, error) {
	if override != "" {
		if strings.ContainsAny(override, "\x00\r\n") || !filepath.IsAbs(override) {
			return "", fmt.Errorf("%w: tsh override must be an absolute path", ErrInvalidInput)
		}
		info, err := os.Stat(override)
		if err != nil || info.IsDir() || info.Mode()&0o111 == 0 {
			return "", fmt.Errorf("%w: tsh override %q is not an executable file", ErrTSHNotInstalled, override)
		}
		return override, nil
	}
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	path, err := lookPath("tsh")
	if err != nil {
		return "", ErrTSHNotInstalled
	}
	return path, nil
}
