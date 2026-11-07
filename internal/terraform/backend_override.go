package terraform

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const backendOverrideFile = "pantau_backend_override.tf"

// writeBackendOverride pins the backend to a local state file under root.
// Override files replace any backend declared by the user's configuration.
// It replaces the deprecated -state flag.
func writeBackendOverride(workDir, root string) error {
	abs, err := filepath.Abs(filepath.Join(root, "terraform.tfstate"))
	if err != nil {
		return err
	}
	// strconv.Quote handles backslashes/quotes; escape HCL template sequences too.
	q := strconv.Quote(filepath.ToSlash(abs))
	q = strings.ReplaceAll(strings.ReplaceAll(q, "${", "$${"), "%{", "%%{")
	body := fmt.Sprintf("terraform {\n  backend \"local\" {\n    path = %s\n  }\n}\n", q)
	if err := os.WriteFile(filepath.Join(workDir, backendOverrideFile), []byte(body), 0600); err != nil {
		return fmt.Errorf("failed to write backend override: %w", err)
	}
	return nil
}
