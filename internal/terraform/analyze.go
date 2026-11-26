package terraform

import (
	"encoding/json"
	"fmt"
	"sort"
)

// PlanResourceItem is a compact representation of one resource change in a Terraform/OpenTofu plan.
type PlanResourceItem struct {
	Symbol   string     `json:"symbol"` // "+create", "~update", "-destroy", "-/+replace", "read"
	Action   string     `json:"action"`
	Address  string     `json:"address"`
	Type     string     `json:"type"`
	Provider string     `json:"provider"`
	Reason   string     `json:"reason,omitempty"`
	Diffs    []AttrDiff `json:"diffs,omitempty"`
}

// PlanAnalysis is the semantic pre-parsed summary of `terraform show -json` (or `tofu show -json`).
type PlanAnalysis struct {
	TFVersion     string             `json:"tf_version,omitempty"`
	Counts        map[string]int     `json:"counts"`
	HasChanges    bool               `json:"has_changes"`
	Resources     []PlanResourceItem `json:"resources"`
	OutputChanges []string           `json:"output_changes,omitempty"`
}

func actionSymbol(action string) string {
	switch action {
	case "create":
		return "+create"
	case "update":
		return "~update"
	case "delete":
		return "-destroy"
	case "replace":
		return "-/+replace"
	default:
		return action
	}
}

// maskNestedSensitive recursively replaces any value marked true in sensitiveMask with "(sensitive)".
func maskNestedSensitive(val, sensitiveMask any) any {
	if sensitiveMask == nil || val == nil {
		return val
	}
	if b, ok := sensitiveMask.(bool); ok {
		if b {
			return "(sensitive)"
		}
		return val
	}
	if smap, ok := sensitiveMask.(map[string]any); ok {
		vmap, isMap := val.(map[string]any)
		if !isMap {
			return val
		}
		out := make(map[string]any, len(vmap))
		for k, v := range vmap {
			out[k] = maskNestedSensitive(v, smap[k])
		}
		return out
	}
	if sarr, ok := sensitiveMask.([]any); ok {
		varr, isArr := val.([]any)
		if !isArr {
			return val
		}
		out := make([]any, len(varr))
		for i, v := range varr {
			var mask any
			if i < len(sarr) {
				mask = sarr[i]
			}
			out[i] = maskNestedSensitive(v, mask)
		}
		return out
	}
	return val
}

func hasAnySensitive(sensitiveObj any, key string) bool {
	if isSensitiveKey(sensitiveObj, key) {
		return true
	}
	if m, ok := sensitiveObj.(map[string]any); ok {
		return containsTrue(m[key])
	}
	return false
}

func containsTrue(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case map[string]any:
		for _, sub := range t {
			if containsTrue(sub) {
				return true
			}
		}
	case []any:
		for _, sub := range t {
			if containsTrue(sub) {
				return true
			}
		}
	}
	return false
}

// AnalyzePlanJSON parses a `terraform show -json` plan payload into a compact summary with
// `+create / ~update / -destroy / -/+replace` symbols and nested sensitive attribute masking.
func AnalyzePlanJSON(planJSON []byte) (*PlanAnalysis, error) {
	var raw struct {
		TerraformVersion string         `json:"terraform_version"`
		ResourceChanges  []rawResChange `json:"resource_changes"`
		OutputChanges    map[string]any `json:"output_changes"`
	}
	if err := json.Unmarshal(planJSON, &raw); err != nil {
		return nil, fmt.Errorf("parse terraform show -json: %w", err)
	}

	counts := map[string]int{
		"create":  0,
		"update":  0,
		"destroy": 0,
		"replace": 0,
	}
	resources := make([]PlanResourceItem, 0, len(raw.ResourceChanges))
	for _, rc := range raw.ResourceChanges {
		action := determineAction(rc.Change.Actions)
		if action == "no-op" {
			continue
		}
		switch action {
		case "create":
			counts["create"]++
		case "update":
			counts["update"]++
		case "delete":
			counts["destroy"]++
		case "replace":
			counts["replace"]++
		}

		diffs := extractAttrDiffs(rc.Change)
		beforeMap, _ := rc.Change.Before.(map[string]any)
		afterMap, _ := rc.Change.After.(map[string]any)
		for i := range diffs {
			k := diffs[i].Path
			if hasAnySensitive(rc.Change.BeforeSensitive, k) || hasAnySensitive(rc.Change.AfterSensitive, k) {
				diffs[i].Sensitive = true
				if isSensitiveKey(rc.Change.BeforeSensitive, k) || isSensitiveKey(rc.Change.AfterSensitive, k) {
					diffs[i].Before = "(sensitive)"
					diffs[i].After = "(sensitive)"
				} else {
					if bSens, ok := rc.Change.BeforeSensitive.(map[string]any); ok {
						diffs[i].Before = maskNestedSensitive(beforeMap[k], bSens[k])
					}
					if aSens, ok := rc.Change.AfterSensitive.(map[string]any); ok {
						diffs[i].After = maskNestedSensitive(afterMap[k], aSens[k])
					}
				}
			}
		}

		resources = append(resources, PlanResourceItem{
			Symbol:   actionSymbol(action),
			Action:   action,
			Address:  rc.Address,
			Type:     rc.Type,
			Provider: rc.ProviderName,
			Reason:   rc.Change.ActionReason,
			Diffs:    diffs,
		})
	}

	outputs := make([]string, 0, len(raw.OutputChanges))
	for k := range raw.OutputChanges {
		outputs = append(outputs, k)
	}
	sort.Strings(outputs)

	hasChanges := counts["create"]+counts["update"]+counts["destroy"]+counts["replace"] > 0 || len(outputs) > 0
	return &PlanAnalysis{
		TFVersion:     raw.TerraformVersion,
		Counts:        counts,
		HasChanges:    hasChanges,
		Resources:     resources,
		OutputChanges: outputs,
	}, nil
}
