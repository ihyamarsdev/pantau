package terraform

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type Policy struct {
	ID          int64           `json:"id"`
	Name        string          `json:"name"`
	Kind        string          `json:"kind"`
	Enforcement string          `json:"enforcement"` // advisory | soft | hard
	Params      json.RawMessage `json:"params"`
}

type Violation struct {
	Address string `json:"address"`
	Message string `json:"message"`
}

type PolicyResult struct {
	PolicyID    int64       `json:"policy_id"`
	Name        string      `json:"name"`
	Kind        string      `json:"kind"`
	Enforcement string      `json:"enforcement"`
	Passed      bool        `json:"passed"`
	Violations  []Violation `json:"violations"`
}

type PolicyReport struct {
	Status  string         `json:"status"` // passed | warning | failed
	Results []PolicyResult `json:"results"`
}

var validPolicyKinds = map[string]bool{
	"deny_destroy":        true,
	"max_deletes":         true,
	"max_changes":         true,
	"deny_resource_types": true,
	"require_tags":        true,
	"deny_open_cidr":      true,
}

func ValidatePolicy(p Policy) error {
	if strings.TrimSpace(p.Name) == "" {
		return errors.New("policy name is required")
	}
	if !validPolicyKinds[p.Kind] {
		return fmt.Errorf("invalid policy kind %q", p.Kind)
	}
	if p.Enforcement != "advisory" && p.Enforcement != "soft" && p.Enforcement != "hard" {
		return fmt.Errorf("invalid enforcement %q, must be advisory, soft, or hard", p.Enforcement)
	}

	// Validate params per kind
	switch p.Kind {
	case "max_deletes", "max_changes":
		var params struct {
			Max int `json:"max"`
		}
		if len(p.Params) == 0 {
			return fmt.Errorf("policy kind %s requires params with 'max'", p.Kind)
		}
		if err := json.Unmarshal(p.Params, &params); err != nil {
			return fmt.Errorf("invalid params for %s: %w", p.Kind, err)
		}
		if params.Max < 0 {
			return errors.New("max must be >= 0")
		}
	case "deny_resource_types":
		var params struct {
			Types []string `json:"types"`
		}
		if len(p.Params) == 0 {
			return errors.New("deny_resource_types requires params with 'types'")
		}
		if err := json.Unmarshal(p.Params, &params); err != nil {
			return fmt.Errorf("invalid params for deny_resource_types: %w", err)
		}
		if len(params.Types) == 0 {
			return errors.New("types cannot be empty")
		}
	case "require_tags":
		var params struct {
			Tags  []string `json:"tags"`
			Types []string `json:"types"`
		}
		if len(p.Params) == 0 {
			return errors.New("require_tags requires params with 'tags'")
		}
		if err := json.Unmarshal(p.Params, &params); err != nil {
			return fmt.Errorf("invalid params for require_tags: %w", err)
		}
		if len(params.Tags) == 0 {
			return errors.New("tags cannot be empty")
		}
	}
	return nil
}

// EvaluatePolicies evaluates planJSON against the given policies.
func EvaluatePolicies(planJSON []byte, ps []Policy) (PolicyReport, error) {
	report := PolicyReport{
		Status:  "passed",
		Results: make([]PolicyResult, 0, len(ps)),
	}

	if len(planJSON) == 0 {
		return report, nil
	}

	var raw struct {
		ResourceChanges []struct {
			Address string `json:"address"`
			Mode    string `json:"mode"`
			Type    string `json:"type"`
			Name    string `json:"name"`
			Change  struct {
				Actions []string       `json:"actions"`
				Before  map[string]any `json:"before"`
				After   map[string]any `json:"after"`
			} `json:"change"`
		} `json:"resource_changes"`
	}

	if err := json.Unmarshal(planJSON, &raw); err != nil {
		return report, fmt.Errorf("failed to unmarshal plan json: %w", err)
	}

	hasWarning := false
	hasFailed := false

	for _, p := range ps {
		res := PolicyResult{
			PolicyID:    p.ID,
			Name:        p.Name,
			Kind:        p.Kind,
			Enforcement: p.Enforcement,
			Passed:      true,
			Violations:  make([]Violation, 0),
		}

		switch p.Kind {
		case "deny_destroy":
			for _, rc := range raw.ResourceChanges {
				action := determineAction(rc.Change.Actions)
				if action == "delete" || action == "replace" {
					res.Passed = false
					res.Violations = append(res.Violations, Violation{
						Address: rc.Address,
						Message: fmt.Sprintf("resource deletion is denied (%s)", action),
					})
				}
			}

		case "max_deletes":
			var params struct {
				Max int `json:"max"`
			}
			_ = json.Unmarshal(p.Params, &params)
			deletes := 0
			for _, rc := range raw.ResourceChanges {
				action := determineAction(rc.Change.Actions)
				if action == "delete" || action == "replace" {
					deletes++
				}
			}
			if deletes > params.Max {
				res.Passed = false
				res.Violations = append(res.Violations, Violation{
					Address: "(global)",
					Message: fmt.Sprintf("total deletes %d exceeds maximum allowed %d", deletes, params.Max),
				})
			}

		case "max_changes":
			var params struct {
				Max int `json:"max"`
			}
			_ = json.Unmarshal(p.Params, &params)
			changes := 0
			for _, rc := range raw.ResourceChanges {
				action := determineAction(rc.Change.Actions)
				if action != "no-op" && action != "read" {
					changes++
				}
			}
			if changes > params.Max {
				res.Passed = false
				res.Violations = append(res.Violations, Violation{
					Address: "(global)",
					Message: fmt.Sprintf("total changes %d exceeds maximum allowed %d", changes, params.Max),
				})
			}

		case "deny_resource_types":
			var params struct {
				Types []string `json:"types"`
			}
			_ = json.Unmarshal(p.Params, &params)
			deniedTypes := make(map[string]bool)
			for _, t := range params.Types {
				deniedTypes[t] = true
			}
			for _, rc := range raw.ResourceChanges {
				action := determineAction(rc.Change.Actions)
				if action != "no-op" && deniedTypes[rc.Type] {
					res.Passed = false
					res.Violations = append(res.Violations, Violation{
						Address: rc.Address,
						Message: fmt.Sprintf("resource type %q is denied", rc.Type),
					})
				}
			}

		case "require_tags":
			var params struct {
				Tags  []string `json:"tags"`
				Types []string `json:"types"`
			}
			_ = json.Unmarshal(p.Params, &params)
			typeFilter := make(map[string]bool)
			for _, t := range params.Types {
				typeFilter[t] = true
			}

			for _, rc := range raw.ResourceChanges {
				action := determineAction(rc.Change.Actions)
				if action == "delete" || action == "no-op" || rc.Mode != "managed" {
					continue
				}
				if len(typeFilter) > 0 && !typeFilter[rc.Type] {
					continue
				}

				// Check tags or tags_all in After
				tagsMap := extractTags(rc.Change.After)
				for _, reqTag := range params.Tags {
					if _, has := tagsMap[reqTag]; !has {
						res.Passed = false
						res.Violations = append(res.Violations, Violation{
							Address: rc.Address,
							Message: fmt.Sprintf("missing required tag %q", reqTag),
						})
					}
				}
			}

		case "deny_open_cidr":
			for _, rc := range raw.ResourceChanges {
				action := determineAction(rc.Change.Actions)
				if action == "delete" || action == "no-op" {
					continue
				}
				if hasOpenCIDR(rc.Change.After) {
					res.Passed = false
					res.Violations = append(res.Violations, Violation{
						Address: rc.Address,
						Message: "open CIDR (0.0.0.0/0 or ::/0) is not permitted",
					})
				}
			}
		}

		if !res.Passed {
			if p.Enforcement == "hard" {
				hasFailed = true
			} else {
				hasWarning = true
			}
		}

		report.Results = append(report.Results, res)
	}

	if hasFailed {
		report.Status = "failed"
	} else if hasWarning {
		report.Status = "warning"
	}

	return report, nil
}

func extractTags(after map[string]any) map[string]any {
	result := make(map[string]any)
	if after == nil {
		return result
	}
	if tm, ok := after["tags"].(map[string]any); ok {
		for k, v := range tm {
			result[k] = v
		}
	}
	if tm, ok := after["tags_all"].(map[string]any); ok {
		for k, v := range tm {
			result[k] = v
		}
	}
	return result
}

func hasOpenCIDR(v any) bool {
	if v == nil {
		return false
	}
	switch val := v.(type) {
	case string:
		trimmed := strings.TrimSpace(val)
		if trimmed == "0.0.0.0/0" || trimmed == "::/0" {
			return true
		}
	case []any:
		for _, elem := range val {
			if hasOpenCIDR(elem) {
				return true
			}
		}
	case map[string]any:
		for _, elem := range val {
			if hasOpenCIDR(elem) {
				return true
			}
		}
	}
	return false
}
