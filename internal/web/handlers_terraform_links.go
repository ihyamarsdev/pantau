package web

import (
	"fmt"
	"net/http"
	"regexp"
	"sort"

	"pantau/internal/logger"
	"pantau/internal/store"
)

var tfLinkVarRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

type tfLinkReq struct {
	VarName           string `json:"var_name"`
	SourceWorkspaceID int64  `json:"source_workspace_id"`
	OutputName        string `json:"output_name"`
}

// tfLinkCreatesCycle reports whether making ws depend on the given sources closes a loop.
func tfLinkCreatesCycle(all []store.TFOutputLink, wsID int64, sources []int64) bool {
	deps := map[int64][]int64{}
	for _, l := range all {
		if l.WorkspaceID != wsID { // ws's own links are being replaced
			deps[l.WorkspaceID] = append(deps[l.WorkspaceID], l.SourceWorkspaceID)
		}
	}
	seen := map[int64]bool{}
	stack := append([]int64(nil), sources...)
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if n == wsID {
			return true
		}
		if seen[n] {
			continue
		}
		seen[n] = true
		stack = append(stack, deps[n]...)
	}
	return false
}

// handleTerraformOutputLinks: GET/PUT /api/terraform/workspaces/{id}/output-links
func (s *Server) handleTerraformOutputLinks(w http.ResponseWriter, r *http.Request, ws *store.TFWorkspace, rest []string) {
	if len(rest) > 0 {
		http.NotFound(w, r)
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.writeTerraformOutputLinks(w, ws)
	case http.MethodPut:
		var req struct {
			Links []tfLinkReq `json:"links"`
		}
		if !decodeTFBody(w, r, 1<<20, &req) {
			return
		}
		links, msg := s.validateTerraformOutputLinks(ws, req.Links)
		if msg != "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": msg})
			return
		}
		if err := s.db.ReplaceTFOutputLinks(ws.ID, links); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		logger.Info("terraform", ws.Name, fmt.Sprintf("Updated output links of workspace %s (%d links)", ws.Name, len(links)))
		s.writeTerraformOutputLinks(w, ws)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

// validateTerraformOutputLinks returns the links to store, or a 400 message.
func (s *Server) validateTerraformOutputLinks(ws *store.TFWorkspace, in []tfLinkReq) ([]store.TFOutputLink, string) {
	names := map[string]bool{}
	links := make([]store.TFOutputLink, 0, len(in))
	sources := make([]int64, 0, len(in))
	for _, l := range in {
		if !tfLinkVarRe.MatchString(l.VarName) {
			return nil, fmt.Sprintf("invalid variable name %q", l.VarName)
		}
		if names[l.VarName] {
			return nil, fmt.Sprintf("duplicate variable name %q", l.VarName)
		}
		names[l.VarName] = true
		if l.OutputName == "" {
			return nil, "output_name is required"
		}
		if l.SourceWorkspaceID == ws.ID {
			return nil, "a workspace cannot link to its own outputs"
		}
		if _, err := s.db.GetTFWorkspace(l.SourceWorkspaceID); err != nil {
			return nil, fmt.Sprintf("source workspace %d not found", l.SourceWorkspaceID)
		}
		links = append(links, store.TFOutputLink{VarName: l.VarName, SourceWorkspaceID: l.SourceWorkspaceID, OutputName: l.OutputName})
		sources = append(sources, l.SourceWorkspaceID)
	}
	all, err := s.db.ListAllTFOutputLinks()
	if err != nil {
		return nil, err.Error()
	}
	if tfLinkCreatesCycle(all, ws.ID, sources) {
		return nil, "output links would create a dependency cycle"
	}
	return links, ""
}

func (s *Server) writeTerraformOutputLinks(w http.ResponseWriter, ws *store.TFWorkspace) {
	fail := func(err error) { writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()}) }
	stored, err := s.db.ListTFOutputLinks(ws.ID)
	if err != nil {
		fail(err)
		return
	}
	all, err := s.db.ListTFWorkspaces("", "", "")
	if err != nil {
		fail(err)
		return
	}
	type outInfo struct {
		Name      string `json:"name"`
		Sensitive bool   `json:"sensitive"`
	}
	type source struct {
		ID      int64     `json:"id"`
		Name    string    `json:"name"`
		Outputs []outInfo `json:"outputs"`
	}
	sources := []source{}
	outsByWs := map[int64]map[string]bool{} // workspace -> output names
	names := map[int64]string{}
	for _, o := range all {
		names[o.ID] = o.Name
		sv, err := s.db.LatestTFStateVersion(o.ID)
		if err != nil {
			fail(err)
			return
		}
		if sv == nil {
			continue
		}
		outs, err := latestStateOutputs(s.db, o.ID)
		if err != nil {
			fail(err)
			return
		}
		src := source{ID: o.ID, Name: o.Name, Outputs: []outInfo{}}
		outsByWs[o.ID] = map[string]bool{}
		for n, out := range outs {
			outsByWs[o.ID][n] = true
			src.Outputs = append(src.Outputs, outInfo{Name: n, Sensitive: out.Sensitive})
		}
		sort.Slice(src.Outputs, func(i, j int) bool { return src.Outputs[i].Name < src.Outputs[j].Name })
		if o.ID != ws.ID {
			sources = append(sources, src)
		}
	}
	links := make([]map[string]any, 0, len(stored))
	for _, l := range stored {
		links = append(links, map[string]any{
			"id": l.ID, "var_name": l.VarName, "source_workspace_id": l.SourceWorkspaceID,
			"source_workspace_name": names[l.SourceWorkspaceID], "output_name": l.OutputName,
			"available": outsByWs[l.SourceWorkspaceID][l.OutputName],
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"links": links, "sources": sources})
}
