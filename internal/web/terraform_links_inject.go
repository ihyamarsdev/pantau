package web

import (
	"bytes"
	"encoding/json"
	"fmt"

	"pantau/internal/store"
	"pantau/internal/terraform"
)

// latestStateOutputs returns the outputs of the latest state of a workspace by name (empty without a state).
func latestStateOutputs(db *store.DB, workspaceID int64) (map[string]terraform.Output, error) {
	out := map[string]terraform.Output{}
	sv, err := db.LatestTFStateVersion(workspaceID)
	if err != nil {
		return nil, err
	}
	if sv == nil || len(sv.State) == 0 {
		return out, nil
	}
	info, err := terraform.ParseState(sv.State)
	if err != nil {
		return out, nil // an unparsable state has no usable outputs
	}
	for _, o := range info.Outputs {
		out[o.Name] = o
	}
	return out, nil
}

// injectOutputLinks adds every output link of ws as an HCL variable; explicit variables win.
func (m *TerraformRunManager) injectOutputLinks(ws *store.TFWorkspace, cfg *terraform.Config) error {
	links, err := m.db.ListTFOutputLinks(ws.ID)
	if err != nil {
		return fmt.Errorf("failed to load output links: %w", err)
	}
	explicit := make(map[string]bool, len(cfg.Vars))
	for _, v := range cfg.Vars {
		explicit[v.Key] = true
	}
	cache := map[int64]map[string]terraform.Output{}
	for _, l := range links {
		if explicit[l.VarName] {
			continue
		}
		outs, ok := cache[l.SourceWorkspaceID]
		if !ok {
			if outs, err = latestStateOutputs(m.db, l.SourceWorkspaceID); err != nil {
				return fmt.Errorf("failed to load state of linked workspace: %w", err)
			}
			cache[l.SourceWorkspaceID] = outs
		}
		o, found := outs[l.OutputName]
		if !found {
			name := fmt.Sprintf("#%d", l.SourceWorkspaceID)
			if src, err := m.db.GetTFWorkspace(l.SourceWorkspaceID); err == nil {
				name = src.Name
			}
			return fmt.Errorf("linked output %q of workspace %q is not available (apply that workspace first)", l.OutputName, name)
		}
		val := "null"
		if len(o.Value) > 0 {
			var b bytes.Buffer
			if json.Compact(&b, o.Value) == nil {
				val = b.String()
			}
		}
		cfg.Vars = append(cfg.Vars, terraform.Var{Key: l.VarName, Value: val, HCL: true, Sensitive: o.Sensitive})
	}
	return nil
}
