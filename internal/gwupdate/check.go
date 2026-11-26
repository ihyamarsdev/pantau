package gwupdate

import (
	"context"
	"fmt"
	"time"
)

// The CLIs behind the Pantau gateways that can be checked for a newer upstream release.
const (
	// TargetAnsible is the ansible-playbook CLI behind the Ansible Gate.
	TargetAnsible = "ansible"
	// TargetTerraform is the terraform CLI behind the Terraform Gate.
	TargetTerraform = "terraform"
	// TargetOpenTofu is the tofu (OpenTofu) CLI behind the Terraform Gate.
	TargetOpenTofu = "tofu"
	// TargetKubectl is the kubectl CLI that External Cluster operations run with (ADR 0082 scope).
	TargetKubectl = "kubectl"
)

// UserAgent identifies Pantau to the upstream release APIs.
const UserAgent = "Pantau"

// The outcome of one comparison between the installed version and the latest upstream release.
const (
	// StatusUpdate means the installed version is older than the latest release.
	StatusUpdate = "update"
	// StatusCurrent means the installed version is the latest release.
	StatusCurrent = "current"
	// StatusUnknown means the versions cannot be compared (no version reported, or unparsable).
	StatusUnknown = "unknown"
	// StatusError means the upstream release could not be reached.
	StatusError = "error"
)

// Result is the outcome of one update check.
type Result struct {
	Target     string    `json:"target"`                // ansible, terraform or tofu
	Installed  string    `json:"installed"`             // the version in use on the Pantau server
	Latest     string    `json:"latest"`                // the newest release published upstream
	Status     string    `json:"status"`                // update, current, unknown or error
	Error      string    `json:"error,omitempty"`       // why the check failed
	ReleaseURL string    `json:"release_url,omitempty"` // where the latest release is published
	CheckedAt  time.Time `json:"checked_at"`
}

// Latest reads the newest upstream release for one target.
func Latest(ctx context.Context, target string) (Release, error) {
	switch target {
	case TargetAnsible:
		return LatestAnsible(ctx)
	case TargetTerraform:
		return LatestTerraform(ctx)
	case TargetOpenTofu:
		return LatestOpenTofu(ctx)
	case TargetKubectl:
		return LatestKubectl(ctx)
	default:
		return Release{}, fmt.Errorf("unknown target %q", target)
	}
}

// Check compares the installed version of one target with the latest upstream release.
//
// An installed version that cannot be parsed is reported as unknown, never as an update: a CLI with an
// unusual version string must not be presented as outdated. An upstream that cannot be reached is
// reported as an error with the reason, never as "up to date".
func Check(ctx context.Context, target, installed string) Result {
	res := Result{Target: target, Installed: installed, Status: StatusUnknown, CheckedAt: time.Now()}

	if installed == "" {
		res.Error = "no version reported for the installed binary"
		return res
	}
	if _, ok := ParseVersion(installed); !ok {
		res.Error = fmt.Sprintf("installed version %q cannot be compared", installed)
		return res
	}

	rel, err := Latest(ctx, target)
	if err != nil {
		res.Status = StatusError
		res.Error = err.Error()
		return res
	}
	res.Latest = rel.Version
	res.ReleaseURL = rel.URL

	switch Compare(rel.Version, installed) {
	case 1:
		res.Status = StatusUpdate
	default:
		res.Status = StatusCurrent
	}
	return res
}
