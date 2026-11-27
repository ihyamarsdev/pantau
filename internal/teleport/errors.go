// Package teleport runs the Teleport tsh CLI and kubectl for Pantau's Teleport profiles. Every binary runs
// with an argv slice (never through a shell), and each profile keeps its kubeconfig in an isolated file so the
// user's ~/.kube/config is never touched.
package teleport

import (
	"errors"
	"strings"
)

var (
	// ErrTSHNotInstalled means no usable tsh binary was found. Pantau never downloads tsh.
	ErrTSHNotInstalled = errors.New("tsh is not installed on the Pantau server")
	// ErrTeleportSessionExpired means the Teleport certificate is missing or expired. The UI shows a Re-Login action.
	ErrTeleportSessionExpired = errors.New("teleport session expired or not logged in")
	// ErrContextNotProvisioned means the cluster has no kube context yet. Run EnsureKubeContext first.
	ErrContextNotProvisioned = errors.New("teleport cluster has no kube context yet")
	// ErrInvalidInput marks a profile or cluster value rejected before anything runs.
	ErrInvalidInput = errors.New("invalid teleport input")
)

// CodeSessionExpired is the structured API code for ErrTeleportSessionExpired.
const CodeSessionExpired = "TELEPORT_SESSION_EXPIRED"

// expiredMarkers are fragments of tsh and kubectl output that mean the Teleport certificate is no longer valid.
// "unauthorized" is the kube API answer for a rejected Teleport certificate.
var expiredMarkers = []string{
	"certificate has expired",
	"certificate is expired",
	"credentials have expired",
	"not logged in",
	"unauthorized",
	"tsh login",
}

// IsSessionExpiredOutput reports whether tsh or kubectl output says the Teleport session is gone.
func IsSessionExpiredOutput(out string) bool {
	s := strings.ToLower(out)
	for _, m := range expiredMarkers {
		if strings.Contains(s, m) {
			return true
		}
	}
	return false
}
