package teleport

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// Profile is the subset of a Teleport profile the runner needs. Every field is checked by ValidateProfile
// before it reaches an argv.
type Profile struct {
	ID              int64
	Proxy           string // host:port of the Teleport proxy
	AuthConnector   string // local, oidc, saml, github, or empty
	Username        string
	TeleportCluster string
	TeleportHome    string // optional TELEPORT_HOME
}

// Allowlists. Each pattern starts with an alphanumeric character, so a value can never begin with "-" and be
// read as a flag, and none of them admits whitespace, quotes or shell metacharacters.
var (
	proxyRe     = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9.-]{0,251}[A-Za-z0-9])?(:[0-9]{1,5})?$`)
	connectorRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9_-]{0,62})$`)
	usernameRe  = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9._@+-]{0,127})$`)
	clusterRe   = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9.-]{0,252})$`)
	kubeNameRe  = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9._-]{0,252})$`)
	contextRe   = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9._/:@-]{0,254})$`)
)

// checkValue accepts an empty value when allowEmpty is set; otherwise value must match re.
func checkValue(field, value string, re *regexp.Regexp, allowEmpty bool) error {
	if value == "" {
		if allowEmpty {
			return nil
		}
		return fmt.Errorf("%w: %s is required", ErrInvalidInput, field)
	}
	if strings.HasPrefix(value, "-") || !re.MatchString(value) {
		return fmt.Errorf("%w: %s has characters or a shape that is not allowed", ErrInvalidInput, field)
	}
	return nil
}

// ValidateProfile checks every field that reaches a tsh argv or a file path.
func ValidateProfile(p Profile) error {
	if err := checkValue("proxy", p.Proxy, proxyRe, false); err != nil {
		return err
	}
	if err := checkValue("auth_connector", p.AuthConnector, connectorRe, true); err != nil {
		return err
	}
	if err := checkValue("username", p.Username, usernameRe, true); err != nil {
		return err
	}
	if err := checkValue("teleport_cluster", p.TeleportCluster, clusterRe, true); err != nil {
		return err
	}
	return ValidateHome(p.TeleportHome)
}

// ValidateKubeName checks a cluster name as tsh lists it, before it is passed to `tsh kube login`.
func ValidateKubeName(name string) error {
	return checkValue("teleport_kube_name", name, kubeNameRe, false)
}

// ValidateContextName checks a kube context name read back from the isolated kubeconfig.
func ValidateContextName(name string) error {
	return checkValue("kube_context_name", name, contextRe, false)
}

// ValidateHome accepts an empty value, an absolute path, or a path under "~/". It rejects control characters
// and any ".." element, so the value cannot climb out of the directory it names.
func ValidateHome(home string) error {
	if home == "" {
		return nil
	}
	if strings.ContainsAny(home, "\x00\r\n") {
		return fmt.Errorf("%w: teleport_home contains control characters", ErrInvalidInput)
	}
	if !filepath.IsAbs(home) && !strings.HasPrefix(home, "~/") {
		return fmt.Errorf("%w: teleport_home must be an absolute path or start with ~/", ErrInvalidInput)
	}
	for _, part := range strings.FieldsFunc(home, func(r rune) bool { return r == '/' || r == filepath.Separator }) {
		if part == ".." {
			return fmt.Errorf("%w: teleport_home must not contain ..", ErrInvalidInput)
		}
	}
	return nil
}

// ResolveHome expands a leading "~/" with userHome and returns a clean absolute path. It assumes ValidateHome passed.
func ResolveHome(home, userHome string) string {
	if strings.HasPrefix(home, "~/") {
		home = filepath.Join(userHome, home[2:])
	}
	return filepath.Clean(home)
}
