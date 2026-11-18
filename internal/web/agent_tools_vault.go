package web

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Vault tools let the agent list secret metadata (never values) and trigger a
// sync through the real /api/vault/targets/{id}/sync endpoint, with an approval
// gate on the change.

func init() { agentTools = append(agentTools, vaultTools...) }

var vaultTools = []agentTool{
	{Name: "list_vault_keys", Description: "List the keys stored in Pantau's secrets vault (metadata only: key, scope, version, expiry, rotation). Values are NEVER returned. Narrow it with scope (global, group, host, cluster).",
		InputSchema: schema(nil, map[string]interface{}{
			"scope": map[string]interface{}{"type": "string", "enum": []string{"global", "group", "host", "cluster"}, "description": "only show secrets of this scope"},
		}), run: toolListVaultKeys},
	{Name: "trigger_env_sync", Description: "Sync a configured vault target: render its template and push the secrets to the host or Kubernetes cluster it is bound to. May wait for operator approval. Returns only the sync status, SHA-256 and drift; never values.", gate: 1,
		InputSchema: schema([]string{"target_id"}, map[string]interface{}{
			"target_id": map[string]interface{}{"type": "integer", "description": "vault sync target id"},
		}), run: toolTriggerEnvSync},
}

// toolListVaultKeys returns TSV metadata rows. It never reads or emits a secret
// value: ListVaultSecrets returns only SecretMeta.
func toolListVaultKeys(s *Server, _ *http.Request, args map[string]interface{}) (string, error) {
	scope := argStr(args, "scope")
	list, err := s.db.ListVaultSecrets()
	if err != nil {
		return "", err
	}
	var b strings.Builder
	tsvLine(&b, "key", "scope", "scope_ref", "version", "expires_at", "needs_rotation")
	for _, m := range list {
		if scope != "" && string(m.Scope) != scope {
			continue
		}
		expires := ""
		if m.ExpiresAt != nil {
			expires = m.ExpiresAt.UTC().Format(time.RFC3339)
		}
		tsvLine(&b, m.Key, string(m.Scope), m.ScopeRef, m.Version, expires, m.NeedsRotation)
	}
	return b.String(), nil
}

// toolTriggerEnvSync gates the change, then drives the real sync endpoint via
// loopback so every handler rule (auth, drift, redaction) applies. It returns
// only status/sha/drift to the model.
func toolTriggerEnvSync(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	targetID, ok := args["target_id"].(float64)
	if !ok || targetID < 1 || targetID != float64(int64(targetID)) {
		return "", fmt.Errorf("target_id (integer) is required")
	}
	id := int64(targetID)
	t, err := s.db.GetVaultTarget(id)
	if err != nil || t == nil {
		return "", fmt.Errorf("vault target %d not found", id)
	}
	summary := fmt.Sprintf("sync vault target %s (%s) to %s", t.Name, t.Kind, t.TargetRef)
	if err := s.agentGateAs(r, 0, "vault", "trigger_env_sync", summary, 1); err != nil {
		return "", err
	}
	resp, err := s.loopJSON(r.Context(), http.MethodPost, "/api/vault/targets/"+strconv.FormatInt(id, 10)+"/sync", nil)
	if err != nil {
		return "", err
	}
	out := map[string]interface{}{
		"target_id": id,
		"sha256":    resp["sha256"],
		"drift":     resp["drift"],
	}
	return jsonText(out)
}
