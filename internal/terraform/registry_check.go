package terraform

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

// Registry check statuses.
const (
	RegUpToDate = "up_to_date"
	RegOutdated = "outdated"
	RegNewerOut = "newer_outside_constraint"
	RegUnpinned = "unpinned"
	RegUnknown  = "unknown"

	registryCheckMax  = 40
	registryCheckConc = 4
)

// RegistryCheckItem is one scanned requirement with its lookup outcome.
type RegistryCheckItem struct {
	RegistryRef
	Latest    string `json:"latest"`
	Status    string `json:"status"`
	Suggested string `json:"suggested"`
}

// RegistryLatest is the part of RegistryClient the check needs.
type RegistryLatest interface {
	LatestModuleVersion(ctx context.Context, ns, name, provider string) (string, error)
	LatestProviderVersion(ctx context.Context, ns, typ string) (string, error)
}

// exactPin returns the version when the constraint is a single `=`/bare pin.
func exactPin(constraint string) (string, bool) {
	c := strings.TrimSpace(constraint)
	if strings.Contains(c, ",") {
		return "", false
	}
	c = strings.TrimSpace(strings.TrimPrefix(c, "="))
	if c == "" || strings.ContainsAny(c[:1], "!<>~") {
		return "", false
	}
	return c, true
}

// RegistryStatus classifies a constraint against the latest published version.
func RegistryStatus(constraint, latest string) string {
	if latest == "" {
		return RegUnknown
	}
	if strings.TrimSpace(constraint) == "" {
		return RegUnpinned
	}
	if pin, ok := exactPin(constraint); ok {
		if _, err := parseSemver(pin); err != nil {
			return RegUnknown
		}
		if CompareVersions(pin, latest) < 0 {
			return RegOutdated
		}
		return RegUpToDate
	}
	ok, err := ConstraintSatisfied(constraint, latest)
	switch {
	case err != nil:
		return RegUnknown
	case ok:
		return RegUpToDate
	}
	return RegNewerOut
}

// CheckRegistryRefs looks up the latest version of each ref (at most 40, 4 at a
// time) and returns items in input order plus per-item error messages.
func CheckRegistryRefs(ctx context.Context, c RegistryLatest, refs []RegistryRef) ([]RegistryCheckItem, []string) {
	errs := []string{}
	if len(refs) > registryCheckMax {
		errs = append(errs, fmt.Sprintf("only the first %d of %d requirements were checked", registryCheckMax, len(refs)))
		refs = refs[:registryCheckMax]
	}
	items := make([]RegistryCheckItem, len(refs))
	msgs := make([]string, len(refs))
	var wg sync.WaitGroup
	sem := make(chan struct{}, registryCheckConc)
	for i, r := range refs {
		items[i] = RegistryCheckItem{RegistryRef: r, Status: RegUnknown}
		if r.Coord == "" {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer func() { <-sem; wg.Done() }()
			p := strings.Split(r.Coord, "/")
			var latest string
			var err error
			if r.Kind == "module" {
				latest, err = c.LatestModuleVersion(ctx, p[0], p[1], p[2])
			} else {
				latest, err = c.LatestProviderVersion(ctx, p[0], p[1])
			}
			if err != nil {
				msgs[i] = fmt.Sprintf("%s %s: %v", r.Kind, r.Name, err)
				return
			}
			items[i].Latest = latest
			items[i].Status = RegistryStatus(r.Constraint, latest)
			items[i].Suggested = SuggestConstraint(latest)
		}()
	}
	wg.Wait()
	for _, m := range msgs {
		if m != "" {
			errs = append(errs, m)
		}
	}
	return items, errs
}
