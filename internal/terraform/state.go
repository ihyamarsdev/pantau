package terraform

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
)

type StateResource struct {
	Address    string         `json:"address"`
	Module     string         `json:"module,omitempty"`
	Mode       string         `json:"mode"`
	Type       string         `json:"type"`
	Name       string         `json:"name"`
	Provider   string         `json:"provider"`
	Index      any            `json:"index,omitempty"`
	Attributes map[string]any `json:"attributes"`
}

type StateInfo struct {
	Serial    int64           `json:"serial"`
	Lineage   string          `json:"lineage"`
	TFVersion string          `json:"tf_version"`
	Resources []StateResource `json:"resources"`
	Outputs   []Output        `json:"outputs"`
}

type AttrChange struct {
	Path   string `json:"path"`
	Before any    `json:"before"`
	After  any    `json:"after"`
}

type StateDiff struct {
	Added   []string `json:"added"`
	Removed []string `json:"removed"`
	Changed []struct {
		Address string       `json:"address"`
		Changes []AttrChange `json:"changes"`
	} `json:"changed"`
}

// rawState is the structure of tfstate v4
type rawState struct {
	Version          int               `json:"version"`
	TerraformVersion string            `json:"terraform_version"`
	Serial           int64             `json:"serial"`
	Lineage          string            `json:"lineage"`
	Outputs          map[string]rawOut `json:"outputs"`
	Resources        []rawResItem      `json:"resources"`
}

type rawOut struct {
	Value     json.RawMessage `json:"value"`
	Type      json.RawMessage `json:"type"`
	Sensitive bool            `json:"sensitive"`
}

type rawResItem struct {
	Module    string       `json:"module"`
	Mode      string       `json:"mode"`
	Type      string       `json:"type"`
	Name      string       `json:"name"`
	Provider  string       `json:"provider"`
	Instances []rawResInst `json:"instances"`
}

type rawResInst struct {
	IndexKey            any            `json:"index_key"`
	SchemaVersion       int            `json:"schema_version"`
	Attributes          map[string]any `json:"attributes"`
	SensitiveAttributes []any          `json:"sensitive_attributes"`
}

// ParseState validates version==4, <=16MiB, masks sensitive attributes with "(sensitive)" and returns StateInfo.
func ParseState(b []byte) (*StateInfo, error) {
	if len(b) > 16*1024*1024 {
		return nil, errors.New("state file exceeds 16 MiB")
	}

	var raw rawState
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, fmt.Errorf("failed to unmarshal state: %w", err)
	}

	if raw.Version != 4 {
		return nil, fmt.Errorf("unsupported state version %d, expected 4", raw.Version)
	}

	info := &StateInfo{
		Serial:    raw.Serial,
		Lineage:   raw.Lineage,
		TFVersion: raw.TerraformVersion,
	}

	// Outputs
	var outNames []string
	for k := range raw.Outputs {
		outNames = append(outNames, k)
	}
	sort.Strings(outNames)
	for _, k := range outNames {
		o := raw.Outputs[k]
		info.Outputs = append(info.Outputs, Output{
			Name:      k,
			Value:     o.Value,
			Type:      o.Type,
			Sensitive: o.Sensitive,
		})
	}

	// Resources
	for _, res := range raw.Resources {
		for _, inst := range res.Instances {
			addr := buildAddress(res.Module, res.Mode, res.Type, res.Name, inst.IndexKey)
			attrs := maskSensitiveAttributes(inst.Attributes, inst.SensitiveAttributes)
			info.Resources = append(info.Resources, StateResource{
				Address:    addr,
				Module:     res.Module,
				Mode:       res.Mode,
				Type:       res.Type,
				Name:       res.Name,
				Provider:   res.Provider,
				Index:      inst.IndexKey,
				Attributes: attrs,
			})
		}
	}

	return info, nil
}

func buildAddress(mod, mode, typ, name string, indexKey any) string {
	var prefix string
	if mod != "" {
		prefix = mod + "."
	}
	if mode == "data" {
		prefix += "data."
	}
	addr := prefix + typ + "." + name
	if indexKey != nil {
		switch v := indexKey.(type) {
		case string:
			addr += fmt.Sprintf("[%q]", v)
		default:
			addr += fmt.Sprintf("[%v]", v)
		}
	}
	return addr
}

func maskSensitiveAttributes(attrs map[string]any, sensitiveList []any) map[string]any {
	if attrs == nil {
		return nil
	}
	res := make(map[string]any, len(attrs))
	for k, v := range attrs {
		res[k] = v
	}

	for _, item := range sensitiveList {
		switch s := item.(type) {
		case string:
			if _, exists := res[s]; exists {
				res[s] = "(sensitive)"
			}
		case []any:
			// Sensitive path list: e.g. [{"value": "pass", "type": "get_attr"}]
			if len(s) > 0 {
				if m, ok := s[0].(map[string]any); ok {
					if val, ok := m["value"].(string); ok {
						if _, exists := res[val]; exists {
							res[val] = "(sensitive)"
						}
					}
				}
			}
		}
	}
	return res
}

// DiffStates compares two state files and returns added, removed, and changed resources.
func DiffStates(a, b []byte) (*StateDiff, error) {
	var infoA, infoB *StateInfo
	var err error

	if len(a) > 0 {
		infoA, err = ParseState(a)
		if err != nil {
			return nil, fmt.Errorf("state A: %w", err)
		}
	} else {
		infoA = &StateInfo{}
	}

	if len(b) > 0 {
		infoB, err = ParseState(b)
		if err != nil {
			return nil, fmt.Errorf("state B: %w", err)
		}
	} else {
		infoB = &StateInfo{}
	}

	mapA := make(map[string]StateResource)
	for _, r := range infoA.Resources {
		mapA[r.Address] = r
	}
	mapB := make(map[string]StateResource)
	for _, r := range infoB.Resources {
		mapB[r.Address] = r
	}

	diff := &StateDiff{
		Added:   make([]string, 0),
		Removed: make([]string, 0),
	}

	for addr := range mapB {
		if _, exists := mapA[addr]; !exists {
			diff.Added = append(diff.Added, addr)
		}
	}
	sort.Strings(diff.Added)

	for addr := range mapA {
		if _, exists := mapB[addr]; !exists {
			diff.Removed = append(diff.Removed, addr)
		}
	}
	sort.Strings(diff.Removed)

	for addr, rA := range mapA {
		if rB, exists := mapB[addr]; exists {
			changes := diffAttrs(rA.Attributes, rB.Attributes)
			if len(changes) > 0 {
				diff.Changed = append(diff.Changed, struct {
					Address string       `json:"address"`
					Changes []AttrChange `json:"changes"`
				}{
					Address: addr,
					Changes: changes,
				})
			}
		}
	}
	sort.Slice(diff.Changed, func(i, j int) bool {
		return diff.Changed[i].Address < diff.Changed[j].Address
	})

	return diff, nil
}

func diffAttrs(a, b map[string]any) []AttrChange {
	keySet := make(map[string]bool)
	for k := range a {
		keySet[k] = true
	}
	for k := range b {
		keySet[k] = true
	}

	keys := make([]string, 0, len(keySet))
	for k := range keySet {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var changes []AttrChange
	for _, k := range keys {
		valA := a[k]
		valB := b[k]
		if !jsonEqual(valA, valB) {
			changes = append(changes, AttrChange{
				Path:   k,
				Before: valA,
				After:  valB,
			})
		}
	}
	return changes
}

// RollbackState returns `old` with serial = current.serial + 1, lineage = current.lineage.
func RollbackState(old, current []byte) ([]byte, error) {
	if len(old) == 0 {
		return nil, errors.New("old state cannot be empty")
	}
	if len(current) == 0 {
		return nil, errors.New("current state cannot be empty")
	}

	var curRaw rawState
	if err := json.Unmarshal(current, &curRaw); err != nil {
		return nil, fmt.Errorf("invalid current state: %w", err)
	}

	var oldMap map[string]any
	if err := json.Unmarshal(old, &oldMap); err != nil {
		return nil, fmt.Errorf("invalid old state: %w", err)
	}

	oldMap["serial"] = curRaw.Serial + 1
	oldMap["lineage"] = curRaw.Lineage

	return json.MarshalIndent(oldMap, "", "  ")
}
