package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"pantau/internal/terraform"
)

// Agent tools for the orchestration features: Ansible playbooks and schedules, Terraform workspaces and their
// catalog, Kubernetes cluster registration, the cloned repositories and the read-only Docker Hub / self-update
// information.
//
// They call the same HTTP handlers the UI uses, in-process (loopback), so every rule of the UI applies unchanged:
// the Ansible and Terraform gates, the workspace lock, the production-environment refusal of auto_apply, the
// Kubernetes access mode, the repository credential rules and the audit log.
//
// Gates: 1 routine and reversible (a draft, a lock, a plan), 2 destructive on one resource (a run cancelled, an
// asset deleted), 3 infrastructure change (a playbook or workspace created or deleted, a cluster registered or
// removed, a repository cloned or deleted, a schedule that runs playbooks on hosts): gate 3 is never auto-approved
// and never covered by a session grant.

func init() { agentTools = append(agentTools, iacTools...) }

var (
	pSched     = map[string]interface{}{"type": "integer", "description": "Ansible schedule id from ansible_schedules"}
	pClusterID = map[string]interface{}{"type": "integer", "description": "Kubernetes cluster id from kube_targets (negative)"}
	pKeyID     = map[string]interface{}{"type": "integer", "description": "SSH Key Library entry id (GET /api/ssh-keys)"}
	pAction    = map[string]interface{}{"type": "string", "description": "the action to perform"}
)

var iacTools = []agentTool{
	// ---- Ansible: playbooks
	{Name: "ansible_playbook_action", Description: "Manage an Ansible playbook: action = create (name and content, or a git_url with git_key_id), update (same fields, replaces the stored playbook), delete (removes the playbook and its schedules), format (checks and reformats YAML, nothing is stored), analyze (variables the YAML references), outline (splits a playbook into play, tasks and roles), compose (the inverse of outline: the parts become YAML), import (adds playbooks from a list of name/content objects). create, update, import and delete are always approved by the operator, every time.", gate: gateInfra,
		InputSchema: schema([]string{"action"}, map[string]interface{}{"action": map[string]interface{}{"type": "string", "enum": []string{"create", "update", "delete", "format", "analyze", "outline", "compose", "import"}}, "playbook_id": pPB, "name": str, "description": str, "content": str, "extra_vars": map[string]interface{}{"type": "object"}, "group_name": str, "git_url": str, "git_branch": str, "git_path": str, "git_key_id": pKeyID, "playbooks": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "object"}, "description": "for import: the playbooks to add (name, content, description, extra_vars)"}, "outline": map[string]interface{}{"type": "object", "description": "for compose: the parts of a playbook (play, pre_tasks, tasks, roles)"}}), run: toolAnsiblePlaybookAction},

	// ---- Ansible: schedules
	{Name: "ansible_schedule_action", Description: "The Ansible schedules (cron entries that run a playbook): action = list (all schedules, or one with schedule_id), create (playbook_id, cron and the targets), update (same fields, replaces the schedule), enable, disable (only the enabled flag changes), delete (removes the schedule; the playbook is kept), run_now (runs the playbook once, right now, with the schedule's targets and mode). Every action but list waits for explicit operator approval, every time.",
		InputSchema: schema([]string{"action"}, map[string]interface{}{"action": map[string]interface{}{"type": "string", "enum": []string{"list", "create", "update", "enable", "disable", "delete", "run_now"}}, "schedule_id": pSched, "playbook_id": pPB, "name": str, "cron": str, "mode": map[string]interface{}{"type": "string", "enum": []string{"run", "check", "diff", "syntax"}}, "host_ids": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "integer"}}, "group": str, "tags": str, "skip_tags": str, "limit": str, "verbosity": map[string]interface{}{"type": "integer"}, "become": map[string]interface{}{"type": "boolean"}, "extra_vars": map[string]interface{}{"type": "object"}}), run: toolAnsibleScheduleAction},
	// ---- Ansible: resources (assets, library, roles, config, status)
	{Name: "ansible_resources", Description: "The Ansible resources of the Pantau server: action = status (is Ansible installed and enabled), assets (the stored templates and files), library (the installed collections and roles), roles (the roles a playbook can name), config (the global ansible.cfg additions), explorer (the hosts grouped for Ansible, with the alias Ansible sees), facts (the stored facts of one host), gather_facts (runs setup on a host now), adhoc (runs one ad-hoc Ansible command, or reads the ad-hoc run history), write_asset (stores a template or a file, replaces the one with the same name), delete_asset (removes it), install_requirements (installs a requirements.yml into the shared library), install_collection or install_role (installs it from Ansible Galaxy into the shared library), remove_collection or remove_role (removes it from the library), set_config (writes the global ansible.cfg additions; only the sections defaults, privilege_escalation and ssh_connection are allowed). Every action but the reads waits for explicit operator approval, every time.",
		InputSchema: schema([]string{"action"}, map[string]interface{}{"action": map[string]interface{}{"type": "string", "enum": []string{"status", "assets", "library", "roles", "config", "explorer", "facts", "gather_facts", "adhoc", "install_requirements", "write_asset", "delete_asset", "install_collection", "install_role", "remove_collection", "remove_role", "set_config"}}, "kind": map[string]interface{}{"type": "string", "enum": []string{"template", "file", "collection", "role"}, "description": "for assets: template or file; for the library: collection or role"}, "host_id": pHost, "asset_id": map[string]interface{}{"type": "integer", "description": "for assets: the id of one template to read"}, "name": str, "content": str, "version": str, "module": map[string]interface{}{"type": "string", "description": "for adhoc: the Ansible module (ping, apt, copy, shell)"}, "args": map[string]interface{}{"type": "string", "description": "for adhoc: the module arguments"}, "pattern": str, "group": str, "host_ids": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "integer"}}, "become": map[string]interface{}{"type": "boolean"}, "verbosity": map[string]interface{}{"type": "integer"}}), run: toolAnsibleResources},

	// ---- Terraform: workspaces
	{Name: "tf_workspace_action", Description: "Manage a Terraform workspace: action = create (name and, for an inline workspace, the files), update (name, description, environment, group, tags, files, drift and plan interval, auto_apply), delete (removes the workspace and its data directory; the deployed infrastructure is NOT touched), lock (blocks every run until it is unlocked), unlock (releases a manual lock), force_unlock (releases the lock of an active run), drift_check (starts a drift detection run), vcs_test (tests the git connection of the workspace). Every action waits for explicit operator approval, every time; auto_apply is refused for a production workspace, exactly as in Pantau.", gate: gateInfra,
		InputSchema: schema([]string{"action"}, map[string]interface{}{"action": map[string]interface{}{"type": "string", "enum": []string{"create", "update", "delete", "lock", "unlock", "force_unlock", "drift_check", "vcs_test"}}, "workspace_id": pWS, "name": str, "description": str, "environment": str, "group_name": str, "tags": map[string]interface{}{"type": "array", "items": str}, "files": map[string]interface{}{"type": "object", "description": "inline workspace files, name -> content"}, "source": map[string]interface{}{"type": "string", "enum": []string{"inline", "git"}}, "git_url": str, "git_branch": str, "git_path": str, "git_key_id": pKeyID, "drift_interval_minutes": map[string]interface{}{"type": "integer"}, "plan_interval_minutes": map[string]interface{}{"type": "integer"}, "auto_apply": map[string]interface{}{"type": "boolean"}, "reason": str}), run: toolTFWorkspaceAction},
	{Name: "tf_run_action", Description: "Act on a Terraform run: action = plan (starts a plan; nothing is changed), refresh (refreshes the state), destroy (plans a destroy; nothing is changed yet), cancel (stops a running run), discard (drops a finished plan, so it can never be applied), apply (applies a finished plan in status awaiting_approval; comment says why). destroy and apply change real infrastructure and always wait for explicit operator approval, every time: plan first and read the result with tf_runs.", gate: gateInfra,
		InputSchema: schema([]string{"action"}, map[string]interface{}{"action": map[string]interface{}{"type": "string", "enum": []string{"plan", "refresh", "destroy", "cancel", "discard", "apply"}}, "run_id": pRunID, "workspace_id": pWS, "comment": map[string]interface{}{"type": "string", "description": "why, at least 3 characters (apply, discard)"}, "message": str, "targets": map[string]interface{}{"type": "array", "items": str}, "replace": map[string]interface{}{"type": "array", "items": str}}), run: toolTFRunAction},
	{Name: "tf_workspace_scaffold", Description: "Scaffold an inline Terraform workspace in ONE call and ONE approval: it creates the workspace (name) or rewrites an existing one (workspace_id) with all its files at once, sets its variables, and with validate: true runs terraform init -backend=false and terraform validate. Use this for new configurations instead of separate tf_workspace_action and tf_workspace_data calls. File names are plain names (no '..', no '/'). A sensitive variable must be a {{ vault.KEY }} reference, never a plaintext secret. The operator approves once for the whole batch; nothing is applied.",
		InputSchema: schema([]string{"files"}, map[string]interface{}{"workspace_id": pWS, "name": str, "description": str, "environment": str, "group_name": str, "tags": map[string]interface{}{"type": "array", "items": str}, "files": map[string]interface{}{"type": "object", "description": "all files of the workspace, name -> content (e.g. versions.tf, variables.tf, main.tf, outputs.tf)"}, "variables": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "object"}, "description": "each {key, value, category: terraform|env, sensitive}; a sensitive value must be {{ vault.KEY }}"}, "validate": map[string]interface{}{"type": "boolean"}}), run: toolTFWorkspaceScaffold},
	{Name: "tf_workspace_data", Description: "The stored data of a Terraform workspace: action = state (the resources in the state), outputs (the outputs; sensitive ones stay hidden), variables (the variables; sensitive values are never returned), versions (the saved state versions), drift (the last drift check), diff (the difference between two state versions), audit (the Terraform log), write_file or delete_file (the files of an inline workspace), set_variable or delete_variable (a variable), state_op (state rm, state mv or import: rewrites the state under the workspace lock), state_import (stores a whole state file as a new version), rollback (restores an older state version), fmt or validate (checks the files; fmt returns the formatted files, validate runs terraform validate). Every action but the reads waits for explicit operator approval, every time; a sensitive variable value must not be sent to the model.",
		InputSchema: schema([]string{"workspace_id", "action"}, map[string]interface{}{"workspace_id": pWS, "action": map[string]interface{}{"type": "string", "enum": []string{"state", "outputs", "variables", "versions", "drift", "diff", "audit", "write_file", "delete_file", "set_variable", "delete_variable", "state_op", "state_import", "rollback", "fmt", "validate"}}, "path": str, "content": str, "files": map[string]interface{}{"type": "object", "description": "for fmt and validate: the files of the workspace"}, "key": str, "value": str, "category": map[string]interface{}{"type": "string", "enum": []string{"terraform", "env"}}, "sensitive": map[string]interface{}{"type": "boolean"}, "description": str, "op": map[string]interface{}{"type": "string", "enum": []string{"rm", "mv", "import"}}, "address": str, "to": str, "import_id": str, "version_id": map[string]interface{}{"type": "integer"}, "from_version_id": map[string]interface{}{"type": "integer", "description": "for diff: the older state version id"}, "to_version_id": map[string]interface{}{"type": "integer", "description": "for diff: the newer state version id"}, "limit": map[string]interface{}{"type": "integer"}}), run: toolTFWorkspaceData},
	{Name: "tf_catalog", Description: "The Terraform catalog of the Pantau server: action = policies (the plan policies), modules (the private modules), varsets (the variable sets), templates (the index of the built-in workspace templates in ONE call: id, name, provider, summary; optional query filters it; template_id returns the files of that one template; call it once, never once per template), registry_modules or registry_providers (the public registry), status (the installed binaries), create_policy, update_policy, delete_policy (a plan policy), create_module, delete_module (a private module), create_varset, delete_varset (a variable set), varset_variables (the variables of a variable set), set_varset_variable, delete_varset_variable (one variable of a variable set), set_varset_workspaces (the workspaces a variable set applies to), cloud_accounts (the cloud accounts of the catalog; their secrets are never shown), importable_accounts (the Pantau accounts a cloud account can copy its credentials from), create_cloud_account (imports the credentials from an existing Pantau account: cloudflare or kubernetes, by id), update_cloud_account (the name, description and non-secret fields), delete_cloud_account, test_cloud_account (tests the stored credentials). Every action but the reads waits for explicit operator approval, every time.",
		InputSchema: schema([]string{"action"}, map[string]interface{}{"action": map[string]interface{}{"type": "string", "enum": []string{"policies", "modules", "varsets", "templates", "registry_modules", "registry_providers", "status", "create_policy", "update_policy", "delete_policy", "create_module", "delete_module", "create_varset", "delete_varset", "varset_variables", "set_varset_variable", "delete_varset_variable", "set_varset_workspaces", "cloud_accounts", "importable_accounts", "create_cloud_account", "update_cloud_account", "delete_cloud_account", "test_cloud_account"}}, "policy_id": map[string]interface{}{"type": "integer"}, "module_id": map[string]interface{}{"type": "integer"}, "varset_id": map[string]interface{}{"type": "integer"}, "name": str, "description": str, "kind": str, "params": map[string]interface{}{"type": "object"}, "enforcement": str, "enabled": map[string]interface{}{"type": "boolean"}, "workspace_id": pWS, "provider": str, "global": map[string]interface{}{"type": "boolean"}, "query": str, "template_id": map[string]interface{}{"type": "string", "description": "for templates: the id of one template, to get its files"}, "limit": map[string]interface{}{"type": "integer"}, "variable_id": map[string]interface{}{"type": "integer"}, "workspace_ids": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "integer"}}, "account_id": map[string]interface{}{"type": "integer"}, "import_type": map[string]interface{}{"type": "string", "enum": []string{"cloudflare", "kubernetes"}}, "import_id": map[string]interface{}{"type": "integer", "description": "the Pantau account the credentials are copied from"}}), run: toolTFCatalog},

	// ---- Kubernetes: clusters
	{Name: "kube_cluster_action", Description: "Manage a Kubernetes cluster in Pantau: action = rename (a new display name), set_mode (full, no-exec or read-only), delete (removes the cluster and its stored credentials; the cluster itself keeps running). A cluster can only be registered from a kubeconfig the operator already stored, so the agent never carries one: it can only rename, change the access mode or remove a cluster that is already registered. Every action waits for explicit operator approval, every time.", gate: gateInfra,
		InputSchema: schema([]string{"action"}, map[string]interface{}{"action": map[string]interface{}{"type": "string", "enum": []string{"rename", "set_mode", "delete"}}, "cluster_id": pClusterID, "name": str, "kube_mode": map[string]interface{}{"type": "string", "enum": []string{"full", "no-exec", "read-only"}}}), run: toolKubeClusterAction},

	// ---- Repositories
	{Name: "repo_action", Description: "Manage the cloned Git repositories: action = clone (clones a repository into the Pantau server; auth none, or ssh with the SSH Key Library key_id), delete (removes the repository and its clone folder), move (moves it to another folder; group empty = no folder), set_auth (changes the credentials: none or ssh with key_id; a token is never accepted here), folder (where new clones are created), set_folder (moves that folder; empty = the default). Every action waits for explicit operator approval, every time.", gate: gateInfra,
		InputSchema: schema([]string{"action"}, map[string]interface{}{"action": map[string]interface{}{"type": "string", "enum": []string{"clone", "delete", "move", "set_auth", "folder", "set_folder"}}, "repo_id": pRepo, "url": str, "name": str, "branch": str, "group": str, "auth": map[string]interface{}{"type": "string", "enum": []string{"none", "ssh"}}, "key_id": pKeyID}), run: toolRepoAction},

	// ---- Docker Hub and self-update (read only)
	{Name: "dockerhub_search", Description: "The public Docker Hub and the Pantau release check: action = search (repositories matching a query), repo (one repository: description, stars, readme), tags (the tags of a repository), update (whether a newer Pantau release is available; nothing is installed and Pantau is not restarted).",
		InputSchema: schema([]string{"action"}, map[string]interface{}{"action": map[string]interface{}{"type": "string", "enum": []string{"search", "repo", "tags", "update"}}, "query": str, "namespace": str, "name": str, "page": map[string]interface{}{"type": "integer"}}), run: toolDockerHubSearch},
}

// ---------------------------------------------------------------- Ansible

// ansiblePlaybookBody builds the request body of the playbook handlers from the tool arguments.
func ansiblePlaybookBody(args map[string]interface{}) map[string]interface{} {
	body := map[string]interface{}{}
	for _, k := range []string{"name", "description", "content", "group_name", "git_url", "git_branch", "git_path"} {
		if v := argStr(args, k); v != "" {
			body[k] = v
		}
	}
	if ev, ok := args["extra_vars"].(map[string]interface{}); ok && len(ev) > 0 {
		b, _ := json.Marshal(ev)
		body["extra_vars"] = string(b)
	}
	if id := argInt(args, "git_key_id"); id > 0 {
		body["git_key_id"] = id
		body["git_auth"] = "ssh_key"
	}
	if _, ok := body["git_auth"]; !ok && argStr(args, "git_url") != "" {
		body["git_auth"] = "none"
	}
	return body
}

func toolAnsiblePlaybookAction(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	action := argStr(args, "action")
	id := argInt(args, "playbook_id")

	switch action {
	case "format", "analyze", "outline":
		content, _ := args["content"].(string)
		if strings.TrimSpace(content) == "" {
			return "", fmt.Errorf("content is required")
		}
		body := map[string]interface{}{"content": content}
		if ev, ok := args["extra_vars"].(map[string]interface{}); ok && len(ev) > 0 {
			b, _ := json.Marshal(ev)
			body["extra_vars"] = string(b)
		}
		preview := true // a POST, but a server-side dry run: nothing is stored, so it needs no gate
		m, err := s.loopJSON(context.WithValue(r.Context(), gateKey{}, &preview), http.MethodPost, "/api/ansible/playbooks/"+action, body)
		if err != nil {
			return "", err
		}
		if action == "format" {
			out, _ := m["content"].(string)
			return redactSecrets(clipTo(out, 16<<10)), nil
		}
		return jsonText(m)

	case "compose":
		// compose is the inverse of outline: it turns the parts of a playbook into YAML. Nothing is stored.
		out, _ := args["outline"].(map[string]interface{})
		if len(out) == 0 {
			return "", fmt.Errorf("outline (the play, tasks and roles of the playbook) is required")
		}
		preview := true
		m, err := s.loopJSON(context.WithValue(r.Context(), gateKey{}, &preview), http.MethodPost, "/api/ansible/playbooks/compose", out)
		if err != nil {
			return "", err
		}
		c, _ := m["content"].(string)
		return redactSecrets(clipTo(c, 16<<10)), nil

	case "import":
		list, _ := args["playbooks"].([]interface{})
		if len(list) == 0 {
			return "", fmt.Errorf("playbooks is required")
		}
		items := make([]interface{}, 0, len(list))
		for _, it := range list {
			obj, ok := it.(map[string]interface{})
			if !ok {
				return "", fmt.Errorf("playbooks must be objects")
			}
			name, _ := obj["name"].(string)
			content, _ := obj["content"].(string)
			if strings.TrimSpace(name) == "" || content == "" {
				return "", fmt.Errorf("every imported playbook needs a name and content")
			}
			item := map[string]interface{}{"name": name, "content": content}
			if d, ok := obj["description"].(string); ok && d != "" {
				item["description"] = d
			}
			if ev, ok := obj["extra_vars"].(map[string]interface{}); ok && len(ev) > 0 {
				b, _ := json.Marshal(ev)
				item["extra_vars"] = string(b)
			}
			items = append(items, item)
		}
		summary := fmt.Sprintf("import %d Ansible playbook(s) into Pantau:\n%s", len(items), playbookNames(items))
		if err := s.agentGateAs(r, 0, "Ansible", "ansible_playbook_action", summary, gateInfra); err != nil {
			return "", err
		}
		m, err := s.loopJSON(r.Context(), http.MethodPost, "/api/ansible/playbooks/import", map[string]interface{}{"playbooks": items})
		if err != nil {
			return "", err
		}
		return jsonText(pickAll(m["playbooks"], "id", "name", "description"))

	case "create", "update":
		body := ansiblePlaybookBody(args)
		if argStr(args, "name") == "" {
			return "", fmt.Errorf("name is required")
		}
		if argStr(args, "content") == "" && argStr(args, "git_url") == "" {
			return "", fmt.Errorf("content or git_url is required")
		}
		method, path := http.MethodPost, "/api/ansible/playbooks"
		if action == "update" {
			if id < 1 {
				return "", fmt.Errorf("playbook_id is required")
			}
			method, path = http.MethodPut, fmt.Sprintf("/api/ansible/playbooks/%d", id)
		}
		summary := fmt.Sprintf("%s the Ansible playbook %q\n%s", action, argStr(args, "name"), clipTo(redactSecrets(iacJSON(body)), 2000))
		if err := s.agentGateAs(r, 0, "Ansible", "ansible_playbook_action", summary, gateInfra); err != nil {
			return "", err
		}
		m, err := s.loopJSON(r.Context(), method, path, body)
		if err != nil {
			return "", err
		}
		pb, _ := m["playbook"].(map[string]interface{})
		return jsonText(pick(pb, "id", "name", "description", "source", "group_name", "updated_at"))

	case "delete":
		if id < 1 {
			return "", fmt.Errorf("playbook_id is required")
		}
		name, _ := s.ansiblePlaybookName(r.Context(), id)
		summary := fmt.Sprintf("delete the Ansible playbook %q (id %d) and its schedules", name, id)
		if err := s.agentGateAs(r, 0, "Ansible", "ansible_playbook_action", summary, gateInfra); err != nil {
			return "", err
		}
		if _, err := s.loopJSON(r.Context(), http.MethodDelete, fmt.Sprintf("/api/ansible/playbooks/%d", id), nil); err != nil {
			return "", err
		}
		return "deleted playbook " + name, nil
	}
	return "", fmt.Errorf("unknown action %q", action)
}

func playbookNames(items []interface{}) string {
	names := make([]string, 0, len(items))
	for _, it := range items {
		obj, _ := it.(map[string]interface{})
		name, _ := obj["name"].(string)
		names = append(names, name)
	}
	return strings.Join(names, ", ")
}

func iacJSON(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// ansiblePlaybookName reads the name of a playbook for an approval summary.
func (s *Server) ansiblePlaybookName(_ context.Context, id int64) (string, error) {
	pb, err := s.db.GetAnsiblePlaybook(id)
	if err != nil || pb == nil {
		return fmt.Sprintf("#%d", id), nil
	}
	return pb.Name, nil
}

func toolAnsibleSchedules(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	if id := argInt(args, "schedule_id"); id > 0 {
		m, err := s.loopJSON(r.Context(), http.MethodGet, fmt.Sprintf("/api/ansible/schedules/%d", id), nil)
		if err != nil {
			return "", err
		}
		sc, _ := m["schedule"].(map[string]interface{})
		return jsonText(pick(sc, "id", "playbook_id", "playbook_name", "name", "cron", "enabled", "mode", "host_ids", "group", "tags", "skip_tags", "limit", "verbosity", "become", "next_run_at", "last_run_at", "last_run_id", "last_error"))
	}
	q := url.Values{}
	if pid := argInt(args, "playbook_id"); pid > 0 {
		q.Set("playbook_id", strconv.FormatInt(pid, 10))
	}
	m, err := s.loopJSON(r.Context(), http.MethodGet, "/api/ansible/schedules?"+q.Encode(), nil)
	if err != nil {
		return "", err
	}
	return jsonText(pickAll(m["schedules"], "id", "playbook_id", "playbook_name", "name", "cron", "enabled", "mode", "group", "next_run_at", "last_run_at", "last_error"))
}

// ansibleScheduleBody builds the schedule request body from the tool arguments.
func ansibleScheduleBody(args map[string]interface{}) map[string]interface{} {
	body := map[string]interface{}{}
	for _, k := range []string{"name", "cron", "mode", "group", "tags", "skip_tags", "limit"} {
		if v := argStr(args, k); v != "" {
			body[k] = v
		}
	}
	if pid := argInt(args, "playbook_id"); pid > 0 {
		body["playbook_id"] = pid
	}
	if raw, ok := args["host_ids"].([]interface{}); ok {
		ids := []int64{}
		for _, v := range raw {
			if f, ok := v.(float64); ok {
				ids = append(ids, int64(f))
			}
		}
		body["host_ids"] = ids
	}
	if v, ok := args["verbosity"].(float64); ok {
		body["verbosity"] = int(v)
	}
	if v, ok := args["become"].(bool); ok {
		body["become"] = v
	}
	if ev, ok := args["extra_vars"].(map[string]interface{}); ok && len(ev) > 0 {
		b, _ := json.Marshal(ev)
		body["extra_vars"] = string(b)
	}
	return body
}

func toolAnsibleScheduleAction(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	action := argStr(args, "action")
	id := argInt(args, "schedule_id")

	switch action {
	case "list":
		return toolAnsibleSchedules(s, r, args)
	case "create", "update":
		body := ansibleScheduleBody(args)
		if pid, _ := body["playbook_id"].(int64); pid < 1 {
			return "", fmt.Errorf("playbook_id is required")
		}
		if argStr(args, "cron") == "" {
			return "", fmt.Errorf("cron is required")
		}
		body["enabled"] = true
		method, path := http.MethodPost, "/api/ansible/schedules"
		if action == "update" {
			if id < 1 {
				return "", fmt.Errorf("schedule_id is required")
			}
			method, path = http.MethodPut, fmt.Sprintf("/api/ansible/schedules/%d", id)
		}
		summary := fmt.Sprintf("%s the Ansible schedule %q\n%s", action, argStr(args, "name"), clipTo(iacJSON(body), 1500))
		if err := s.agentGateAs(r, 0, "Ansible", "ansible_schedule_action", summary, gateInfra); err != nil {
			return "", err
		}
		m, err := s.loopJSON(r.Context(), method, path, body)
		if err != nil {
			return "", err
		}
		sc, _ := m["schedule"].(map[string]interface{})
		return jsonText(pick(sc, "id", "playbook_id", "name", "cron", "enabled", "mode", "next_run_at"))

	case "enable", "disable":
		if id < 1 {
			return "", fmt.Errorf("schedule_id is required")
		}
		m, err := s.loopJSON(r.Context(), http.MethodGet, fmt.Sprintf("/api/ansible/schedules/%d", id), nil)
		if err != nil {
			return "", err
		}
		sc, _ := m["schedule"].(map[string]interface{})
		body := map[string]interface{}{"enabled": action == "enable"}
		for _, k := range []string{"playbook_id", "name", "cron", "mode", "group", "tags", "skip_tags", "limit", "host_ids", "verbosity", "become", "extra_vars"} {
			if v, ok := sc[k]; ok {
				body[k] = v
			}
		}
		summary := fmt.Sprintf("%s the Ansible schedule %q (id %d)", action, scheduleLabel(sc), id)
		if err := s.agentGateAs(r, 0, "Ansible", "ansible_schedule_action", summary, gateInfra); err != nil {
			return "", err
		}
		out, err := s.loopJSON(r.Context(), http.MethodPut, fmt.Sprintf("/api/ansible/schedules/%d", id), body)
		if err != nil {
			return "", err
		}
		updated, _ := out["schedule"].(map[string]interface{})
		return jsonText(pick(updated, "id", "name", "cron", "enabled", "mode", "next_run_at"))

	case "delete":
		if id < 1 {
			return "", fmt.Errorf("schedule_id is required")
		}
		m, err := s.loopJSON(r.Context(), http.MethodGet, fmt.Sprintf("/api/ansible/schedules/%d", id), nil)
		if err != nil {
			return "", err
		}
		sc, _ := m["schedule"].(map[string]interface{})
		summary := fmt.Sprintf("delete the Ansible schedule %q (id %d); the playbook is kept", scheduleLabel(sc), id)
		if err := s.agentGateAs(r, 0, "Ansible", "ansible_schedule_action", summary, gateInfra); err != nil {
			return "", err
		}
		if _, err := s.loopJSON(r.Context(), http.MethodDelete, fmt.Sprintf("/api/ansible/schedules/%d", id), nil); err != nil {
			return "", err
		}
		return "deleted schedule " + scheduleLabel(sc), nil

	case "run_now":
		return s.ansibleScheduleRunNow(r, args, id)
	}
	return "", fmt.Errorf("unknown action %q", action)
}

func scheduleLabel(sc map[string]interface{}) string {
	if n, _ := sc["name"].(string); n != "" {
		return n
	}
	if pb, _ := sc["playbook_name"].(string); pb != "" {
		return pb
	}
	return "schedule"
}

// ansibleScheduleRunNow runs the playbook of a schedule once, with the schedule's own targets and mode.
func (s *Server) ansibleScheduleRunNow(r *http.Request, args map[string]interface{}, id int64) (string, error) {
	var sc map[string]interface{}
	if id > 0 {
		m, err := s.loopJSON(r.Context(), http.MethodGet, fmt.Sprintf("/api/ansible/schedules/%d", id), nil)
		if err != nil {
			return "", err
		}
		sc, _ = m["schedule"].(map[string]interface{})
	} else {
		pid := argInt(args, "playbook_id")
		if pid < 1 {
			return "", fmt.Errorf("schedule_id or playbook_id is required")
		}
		m, err := s.loopJSON(r.Context(), http.MethodGet, fmt.Sprintf("/api/ansible/playbooks/%d", pid), nil)
		if err != nil {
			return "", err
		}
		pb, _ := m["playbook"].(map[string]interface{})
		sc = map[string]interface{}{"playbook_id": pid, "playbook_name": pb["name"], "mode": "run"}
	}
	pid := int64(0)
	switch v := sc["playbook_id"].(type) {
	case float64:
		pid = int64(v)
	case int64:
		pid = v
	}
	if pid < 1 {
		return "", fmt.Errorf("the schedule has no playbook")
	}
	mode, _ := sc["mode"].(string)
	if mode == "" {
		mode = "run"
	}
	body := map[string]interface{}{"mode": mode}
	for _, k := range []string{"group", "limit", "tags", "skip_tags"} {
		if v, ok := sc[k]; ok && v != nil && v != "" {
			body[k] = v
		}
	}
	if v, ok := sc["host_ids"].([]interface{}); ok {
		body["host_ids"] = v
	}
	if v, ok := sc["become"].(bool); ok {
		body["become"] = v
	}
	if v, ok := sc["extra_vars"].(string); ok && v != "" {
		body["extra_vars"] = v
	}
	name, _ := sc["playbook_name"].(string)
	summary := fmt.Sprintf("run the Ansible playbook %q (id %d) now, mode %s\ntargets: %s", name, pid, mode, clipTo(iacJSON(body), 800))
	if err := s.agentGateAs(r, 0, "Ansible", "ansible_schedule_action", summary, gateInfra); err != nil {
		return "", err
	}
	res, err := s.loopJSON(r.Context(), http.MethodPost, fmt.Sprintf("/api/ansible/playbooks/%d/run", pid), body)
	if err != nil {
		return "", err
	}
	run, _ := res["run"].(map[string]interface{})
	return jsonText(pick(run, "id", "status", "mode", "targets", "playbook_name"))
}

func toolAnsibleResources(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	action := argStr(args, "action")
	switch action {
	case "write_asset", "delete_asset", "install_collection", "install_role", "remove_collection", "remove_role", "set_config":
		return toolAnsibleResourceAction(s, r, args)
	}
	what := action
	switch what {
	case "status":
		m, err := s.loopJSON(r.Context(), http.MethodGet, "/api/ansible/status", nil)
		if err != nil {
			return "", err
		}
		return jsonText(pick(m, "available", "version", "error", "enabled", "max_concurrent", "git_available"))
	case "assets":
		if aid := argInt(args, "asset_id"); aid > 0 {
			m, err := s.loopJSON(r.Context(), http.MethodGet, fmt.Sprintf("/api/ansible/assets/%d", aid), nil)
			if err != nil {
				return "", err
			}
			out := pick(m, "id", "name")
			if c, ok := m["content"].(string); ok {
				out["content"] = redactSecrets(clipTo(c, 16<<10))
			}
			return jsonText(out)
		}
		q := url.Values{}
		if k := argStr(args, "kind"); k != "" {
			q.Set("kind", k)
		}
		m, err := s.loopJSON(r.Context(), http.MethodGet, "/api/ansible/assets?"+q.Encode(), nil)
		if err != nil {
			return "", err
		}
		return jsonText(pickAll(m["assets"], "id", "kind", "name", "size", "updated_at"))
	case "library":
		m, err := s.loopJSON(r.Context(), http.MethodGet, "/api/ansible/library", nil)
		if err != nil {
			return "", err
		}
		return jsonText(pick(m, "available", "collections", "roles"))
	case "roles":
		m, err := s.loopJSON(r.Context(), http.MethodGet, "/api/ansible/roles", nil)
		if err != nil {
			return "", err
		}
		return jsonText(m)
	case "config":
		m, err := s.loopJSON(r.Context(), http.MethodGet, "/api/ansible/config", nil)
		if err != nil {
			return "", err
		}
		out := pick(m, "allowed_sections", "managed_keys")
		if c, ok := m["content"].(string); ok {
			out["content"] = redactSecrets(clipTo(c, 8<<10))
		}
		return jsonText(out)
	case "explorer":
		m, err := s.loopJSON(r.Context(), http.MethodGet, "/api/ansible/explorer", nil)
		if err != nil {
			return "", err
		}
		return jsonText(m)
	case "facts":
		hid, err := argHost(args)
		if err != nil {
			return "", err
		}
		m, err := s.loopJSON(r.Context(), http.MethodGet, fmt.Sprintf("/api/ansible/hosts/%d/facts", hid), nil)
		if err != nil {
			return "", err
		}
		return jsonText(pick(m, "gathered_at", "summary"))
	case "gather_facts":
		hid, err := argHost(args)
		if err != nil {
			return "", err
		}
		summary := fmt.Sprintf("gather the Ansible facts of host #%d now (it runs setup on the host)", hid)
		if err := s.agentGateAs(r, 0, "Ansible", "ansible_resources", summary, gateInfra); err != nil {
			return "", err
		}
		m, err := s.loopJSON(r.Context(), http.MethodPost, fmt.Sprintf("/api/ansible/hosts/%d/facts", hid), nil)
		if err != nil {
			return "", err
		}
		return jsonText(pick(m, "gathered_at", "summary"))
	case "adhoc":
		return s.ansibleAdhocAction(r, args)
	}
	return "", fmt.Errorf("unknown action %q", action)
}

// ansibleAdhocAction runs one ad-hoc Ansible command (a module with its args) on the selected hosts, or reads the
// ad-hoc run history. An ad-hoc command changes the hosts it touches, so it always asks.
func (s *Server) ansibleAdhocAction(r *http.Request, args map[string]interface{}) (string, error) {
	if argStr(args, "module") == "" {
		// The list of the ad-hoc runs.
		m, err := s.loopJSON(r.Context(), http.MethodGet, "/api/ansible/adhoc?limit=20", nil)
		if err != nil {
			return "", err
		}
		return jsonText(pickAll(m["runs"], "id", "module", "args", "pattern", "status", "created_at"))
	}
	body := map[string]interface{}{"module": argStr(args, "module"), "args": argStr(args, "args")}
	if v := argStr(args, "pattern"); v != "" {
		body["pattern"] = v
	}
	if v := argStr(args, "group"); v != "" {
		body["group"] = v
	}
	if raw, ok := args["host_ids"].([]interface{}); ok {
		ids := []int64{}
		for _, v := range raw {
			if f, ok := v.(float64); ok {
				ids = append(ids, int64(f))
			}
		}
		body["host_ids"] = ids
	}
	if v, ok := args["become"].(bool); ok {
		body["become"] = v
	}
	if v, ok := args["verbosity"].(float64); ok {
		body["verbosity"] = int(v)
	}
	summary := fmt.Sprintf("run the ad-hoc Ansible command %q with args %q on the selected hosts\n%s", argStr(args, "module"), argStr(args, "args"), clipTo(iacJSON(body), 800))
	if err := s.agentGateAs(r, 0, "Ansible", "ansible_resources", summary, gateInfra); err != nil {
		return "", err
	}
	m, err := s.loopJSON(r.Context(), http.MethodPost, "/api/ansible/adhoc", body)
	if err != nil {
		return "", err
	}
	run, _ := m["run"].(map[string]interface{})
	return jsonText(pick(run, "id", "status", "module", "args", "pattern", "targets"))
}

func toolAnsibleResourceAction(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	action := argStr(args, "action")
	kind := argStr(args, "kind")
	name := argStr(args, "name")

	switch action {
	case "write_asset":
		if kind != "template" && kind != "file" {
			return "", fmt.Errorf("kind must be template or file")
		}
		if name == "" {
			return "", fmt.Errorf("name is required")
		}
		content, _ := args["content"].(string)
		if content == "" {
			return "", fmt.Errorf("content is required")
		}
		summary := fmt.Sprintf("write the Ansible %s %q (%d bytes)", kind, name, len(content))
		if err := s.agentGateAs(r, 0, "Ansible", "ansible_resource_action", summary, gateInfra); err != nil {
			return "", err
		}
		code, body := s.loopback(r.Context(), http.MethodPut, "/api/ansible/assets?kind="+url.QueryEscape(kind)+"&name="+url.QueryEscape(name), content)
		if code/100 != 2 {
			return "", fmt.Errorf("the asset was not stored (HTTP %d): %s", code, clipTo(strings.TrimSpace(string(body)), 200))
		}
		return "stored the Ansible " + kind + " " + name, nil

	case "delete_asset":
		aid := argInt(args, "asset_id")
		if aid < 1 {
			return "", fmt.Errorf("asset_id is required")
		}
		summary := fmt.Sprintf("delete the Ansible asset #%d", aid)
		if err := s.agentGateAs(r, 0, "Ansible", "ansible_resource_action", summary, gateInfra); err != nil {
			return "", err
		}
		if _, err := s.loopJSON(r.Context(), http.MethodDelete, fmt.Sprintf("/api/ansible/assets/%d", aid), nil); err != nil {
			return "", err
		}
		return fmt.Sprintf("deleted the Ansible asset #%d", aid), nil

	case "install_requirements":
		content, _ := args["content"].(string)
		if strings.TrimSpace(content) == "" {
			return "", fmt.Errorf("content (the requirements.yml) is required")
		}
		summary := fmt.Sprintf("install the Ansible requirements file into the shared library:\n%s", clipTo(content, 2000))
		if err := s.agentGateAs(r, 0, "Ansible", "ansible_resources", summary, gateInfra); err != nil {
			return "", err
		}
		code, out := s.loopback(r.Context(), http.MethodPost, "/api/ansible/library/requirements", map[string]string{"content": content})
		if code/100 != 2 {
			return "", fmt.Errorf("the install failed (HTTP %d): %s", code, clipTo(strings.TrimSpace(string(out)), 300))
		}
		return "installed the Ansible requirements", nil

	case "install_collection", "install_role":
		kind = "collection"
		if action == "install_role" {
			kind = "role"
		}
		if name == "" {
			return "", fmt.Errorf("name is required")
		}
		body := map[string]interface{}{"kind": kind, "name": name}
		if v := argStr(args, "version"); v != "" {
			body["version"] = v
		}
		summary := fmt.Sprintf("install the Ansible %s %q from Ansible Galaxy into the shared library", kind, name)
		if err := s.agentGateAs(r, 0, "Ansible", "ansible_resource_action", summary, gateInfra); err != nil {
			return "", err
		}
		code, out := s.loopback(r.Context(), http.MethodPost, "/api/ansible/library/galaxy", body)
		if code/100 != 2 {
			return "", fmt.Errorf("the install failed (HTTP %d): %s", code, clipTo(strings.TrimSpace(string(out)), 300))
		}
		return "installed the Ansible " + kind + " " + name, nil

	case "remove_collection", "remove_role":
		kind = "collections"
		if action == "remove_role" {
			kind = "roles"
		}
		if name == "" {
			return "", fmt.Errorf("name is required")
		}
		summary := fmt.Sprintf("remove the Ansible %s %q from the shared library", kind, name)
		if err := s.agentGateAs(r, 0, "Ansible", "ansible_resource_action", summary, gateInfra); err != nil {
			return "", err
		}
		if _, err := s.loopJSON(r.Context(), http.MethodDelete, "/api/ansible/library/"+kind+"/"+url.PathEscape(name), nil); err != nil {
			return "", err
		}
		return "removed the Ansible " + strings.TrimSuffix(kind, "s") + " " + name, nil

	case "set_config":
		content, _ := args["content"].(string)
		if strings.TrimSpace(content) == "" {
			return "", fmt.Errorf("content is required")
		}
		summary := fmt.Sprintf("replace the global Ansible config (ansible.cfg additions) of every Ansible run:\n%s", clipTo(content, 2000))
		if err := s.agentGateAs(r, 0, "Ansible", "ansible_resource_action", summary, gateInfra); err != nil {
			return "", err
		}
		m, err := s.loopJSON(r.Context(), http.MethodPut, "/api/ansible/config", map[string]string{"content": content})
		if err != nil {
			return "", err
		}
		out := pick(m, "allowed_sections", "managed_keys")
		if c, ok := m["content"].(string); ok {
			out["content"] = clipTo(c, 2000)
		}
		return jsonText(out)
	}
	return "", fmt.Errorf("unknown action %q", action)
}

// ---------------------------------------------------------------- Terraform

func toolTFWorkspaceAction(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	action := argStr(args, "action")
	id := argInt(args, "workspace_id")

	switch action {
	case "create":
		body := tfWorkspaceBody(args)
		if argStr(args, "name") == "" {
			return "", fmt.Errorf("name is required")
		}
		summary := fmt.Sprintf("create the Terraform workspace %q\n%s", argStr(args, "name"), clipTo(iacJSON(pick(body, "name", "environment", "source", "files", "git_url", "git_branch")), 2000))
		if err := s.agentGateAs(r, 0, "Terraform", "tf_workspace_action", summary, gateInfra); err != nil {
			return "", err
		}
		m, err := s.loopJSON(r.Context(), http.MethodPost, "/api/terraform/workspaces", body)
		if err != nil {
			return "", err
		}
		ws, _ := m["workspace"].(map[string]interface{})
		// The metadata is echoed back, so the model sees that create saved it and does not send an update.
		return jsonText(pick(ws, "id", "name", "description", "environment", "group_name", "tags", "source", "status", "auto_apply", "plan_interval_minutes"))

	case "update":
		if id < 1 {
			return "", fmt.Errorf("workspace_id is required")
		}
		body := tfWorkspaceBody(args)
		name, _, err := s.tfWorkspaceName(r.Context(), id)
		if err != nil {
			return "", err
		}
		summary := fmt.Sprintf("update the Terraform workspace %q (id %d)\n%s", name, id, clipTo(iacJSON(body), 2000))
		if err := s.agentGateAs(r, 0, "Terraform", "tf_workspace_action", summary, gateInfra); err != nil {
			return "", err
		}
		m, err := s.loopJSON(r.Context(), http.MethodPut, fmt.Sprintf("/api/terraform/workspaces/%d", id), body)
		if err != nil {
			return "", err
		}
		ws, _ := m["workspace"].(map[string]interface{})
		return jsonText(pick(ws, "id", "name", "environment", "source", "status", "auto_apply", "plan_interval_minutes", "drift_interval_minutes"))

	case "delete":
		if id < 1 {
			return "", fmt.Errorf("workspace_id is required")
		}
		name, locked, err := s.tfWorkspaceName(r.Context(), id)
		if err != nil {
			return "", err
		}
		if locked {
			return "", fmt.Errorf("workspace %q is locked (another run is active or awaiting apply); check tf_runs", name)
		}
		summary := fmt.Sprintf("delete the Terraform workspace %q (id %d) and its stored state, runs and files.\nThe deployed infrastructure is NOT touched: use tf_run_action destroy and apply to remove it.", name, id)
		if err := s.agentGateAs(r, 0, "Terraform", "tf_workspace_action", summary, gateInfra); err != nil {
			return "", err
		}
		if _, err := s.loopJSON(r.Context(), http.MethodDelete, fmt.Sprintf("/api/terraform/workspaces/%d", id), nil); err != nil {
			return "", err
		}
		return "deleted the Terraform workspace " + name, nil

	case "lock", "unlock", "force_unlock":
		if id < 1 {
			return "", fmt.Errorf("workspace_id is required")
		}
		name, _, err := s.tfWorkspaceName(r.Context(), id)
		if err != nil {
			return "", err
		}
		body := map[string]interface{}{}
		if v := argStr(args, "reason"); v != "" {
			body["reason"] = v
		}
		summary := fmt.Sprintf("%s the Terraform workspace %q (id %d)", strings.ReplaceAll(action, "_", " "), name, id)
		if action == "force_unlock" {
			summary += "; the run that holds the lock is abandoned"
		}
		if err := s.agentGateAs(r, 0, "Terraform", "tf_workspace_action", summary, gateInfra); err != nil {
			return "", err
		}
		m, err := s.loopJSON(r.Context(), http.MethodPost, fmt.Sprintf("/api/terraform/workspaces/%d/%s", id, strings.ReplaceAll(action, "_", "-")), body)
		if err != nil {
			return "", err
		}
		ws, _ := m["workspace"].(map[string]interface{})
		return jsonText(pick(ws, "id", "name", "locked", "lock_reason"))

	case "drift_check":
		if id < 1 {
			return "", fmt.Errorf("workspace_id is required")
		}
		name, locked, err := s.tfWorkspaceName(r.Context(), id)
		if err != nil {
			return "", err
		}
		if locked {
			return "", fmt.Errorf("workspace %q is locked (another run is active or awaiting apply); check tf_runs", name)
		}
		summary := fmt.Sprintf("start a drift detection run on the Terraform workspace %q (id %d): it compares the state with the real infrastructure and changes nothing", name, id)
		if err := s.agentGateAs(r, 0, "Terraform", "tf_workspace_action", summary, gateInfra); err != nil {
			return "", err
		}
		m, err := s.loopJSON(r.Context(), http.MethodPost, fmt.Sprintf("/api/terraform/workspaces/%d/drift/check", id), nil)
		if err != nil {
			return "", err
		}
		run, _ := m["run"].(map[string]interface{})
		return jsonText(pick(run, "id", "type", "status"))

	case "vcs_test":
		if id < 1 {
			return "", fmt.Errorf("workspace_id is required")
		}
		name, _, err := s.tfWorkspaceName(r.Context(), id)
		if err != nil {
			return "", err
		}
		summary := fmt.Sprintf("test the git connection of the Terraform workspace %q (id %d)", name, id)
		if err := s.agentGateAs(r, 0, "Terraform", "tf_workspace_action", summary, gateInfra); err != nil {
			return "", err
		}
		m, err := s.loopJSON(r.Context(), http.MethodPost, fmt.Sprintf("/api/terraform/workspaces/%d/vcs/test", id), nil)
		if err != nil {
			return "", err
		}
		out := pick(m, "ok")
		if e, ok := m["error"].(string); ok && e != "" {
			out["error"] = clipTo(e, 300)
		}
		return jsonText(out)
	}
	return "", fmt.Errorf("unknown action %q", action)
}

// tfScaffoldVaultRef is the only form a sensitive variable may take in a scaffold: a blind reference the server
// resolves, never the secret itself.
var tfScaffoldVaultRef = regexp.MustCompile(`^\{\{\s*vault\.[A-Za-z_][A-Za-z0-9_]*\s*\}\}$`)

// toolTFWorkspaceScaffold creates or rewrites an inline workspace with all its files, sets its variables and
// optionally validates it, behind one approval. Every check that needs no approval runs before the gate, so a
// refused request never bothers the operator.
func toolTFWorkspaceScaffold(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	if s.db == nil || !s.db.GetTerraformEnabled() {
		return "", fmt.Errorf("terraform is disabled: an administrator must enable the Terraform gate first")
	}
	rawFiles, ok := args["files"].(map[string]interface{})
	if !ok || len(rawFiles) == 0 {
		return "", fmt.Errorf("files is required: a map of file name to content")
	}
	files := make(map[string]string, len(rawFiles))
	for name, v := range rawFiles {
		txt, ok := v.(string)
		if !ok {
			return "", fmt.Errorf("file %q must be a string", name)
		}
		files[name] = txt
	}
	if err := terraform.ValidateFiles(files); err != nil {
		return "", err
	}

	vars := []map[string]interface{}{}
	rawVars, _ := args["variables"].([]interface{})
	for _, rv := range rawVars {
		obj, _ := rv.(map[string]interface{})
		key := argStr(obj, "key")
		if key == "" {
			return "", fmt.Errorf("every variable needs a key")
		}
		value, _ := obj["value"].(string)
		sensitive, _ := obj["sensitive"].(bool)
		if sensitive && !tfScaffoldVaultRef.MatchString(strings.TrimSpace(value)) {
			return "", fmt.Errorf("sensitive variable %q must be a {{ vault.KEY }} reference, never a plaintext secret", key)
		}
		category := argStr(obj, "category")
		if category == "" {
			category = "terraform"
		}
		if category != "terraform" && category != "env" {
			return "", fmt.Errorf("variable %q: category must be terraform or env", key)
		}
		vars = append(vars, map[string]interface{}{"key": key, "value": value, "category": category, "sensitive": sensitive})
	}

	id := argInt(args, "workspace_id")
	body := tfWorkspaceBody(args)
	body["files"] = files
	fileNames := make([]string, 0, len(files))
	for n := range files {
		fileNames = append(fileNames, n)
	}
	sort.Strings(fileNames)
	varNames := make([]string, 0, len(vars))
	for _, v := range vars {
		k := v["key"].(string)
		if v["sensitive"].(bool) {
			k += " (sensitive)"
		}
		varNames = append(varNames, k)
	}

	var method, path, summary string
	if id > 0 {
		name, locked, err := s.tfWorkspaceName(r.Context(), id)
		if err != nil {
			return "", err
		}
		if locked {
			return "", fmt.Errorf("workspace %q is locked (another run is active or awaiting apply); check tf_runs", name)
		}
		method, path = http.MethodPut, fmt.Sprintf("/api/terraform/workspaces/%d", id)
		summary = fmt.Sprintf("rewrite the Terraform workspace %q (id %d): files %s; variables %s", name, id, strings.Join(fileNames, ", "), strings.Join(varNames, ", "))
	} else {
		name := argStr(args, "name")
		if name == "" {
			return "", fmt.Errorf("name is required to create a workspace, or workspace_id to rewrite one")
		}
		method, path = http.MethodPost, "/api/terraform/workspaces"
		summary = fmt.Sprintf("create the Terraform workspace %q: files %s; variables %s", name, strings.Join(fileNames, ", "), strings.Join(varNames, ", "))
	}
	if err := s.agentGateAs(r, 0, "Terraform", "tf_workspace_scaffold", summary, gateInfra); err != nil {
		return "", err
	}

	m, err := s.loopJSON(r.Context(), method, path, body)
	if err != nil {
		return "", err
	}
	ws, _ := m["workspace"].(map[string]interface{})
	f, _ := ws["id"].(float64)
	wsID := int64(f)
	if wsID < 1 {
		return "", fmt.Errorf("the workspace was not saved")
	}

	// The variables are upserted by key: an existing key is replaced, a new one is added.
	base := fmt.Sprintf("/api/terraform/workspaces/%d/variables", wsID)
	existing, err := s.loopJSON(r.Context(), http.MethodGet, base, nil)
	if err != nil {
		return "", err
	}
	ids := map[string]int64{}
	stored, _ := existing["variables"].([]interface{})
	for _, sv := range stored {
		obj, _ := sv.(map[string]interface{})
		k, _ := obj["key"].(string)
		vf, _ := obj["id"].(float64)
		ids[k] = int64(vf)
	}
	for _, v := range vars {
		key := v["key"].(string)
		if vid, ok := ids[key]; ok {
			_, err = s.loopJSON(r.Context(), http.MethodPut, fmt.Sprintf("%s/%d", base, vid), v)
		} else {
			_, err = s.loopJSON(r.Context(), http.MethodPost, base, v)
		}
		if err != nil {
			return "", fmt.Errorf("variable %q: %w", key, err)
		}
	}

	out := map[string]interface{}{
		"workspace": pick(ws, "id", "name", "description", "environment", "group_name", "tags", "source", "status"),
		"files":     fileNames,
		"variables": varNames,
	}
	if v, _ := args["validate"].(bool); v {
		// The validation is a dry run that changes nothing, so it needs no second approval.
		preview := true
		vm, verr := s.loopJSON(context.WithValue(r.Context(), gateKey{}, &preview), http.MethodPost, fmt.Sprintf("/api/terraform/workspaces/%d/tools/validate", wsID), map[string]interface{}{"files": files})
		if verr != nil {
			out["validate_error"] = verr.Error()
		} else {
			out["validate"] = clipTo(iacJSON(vm), 2000)
		}
	}
	return jsonText(out)
}

func tfWorkspaceBody(args map[string]interface{}) map[string]interface{} {
	body := map[string]interface{}{}
	for _, k := range []string{"name", "description", "environment", "group_name", "source", "git_url", "git_branch", "git_path"} {
		if v := argStr(args, k); v != "" {
			body[k] = v
		}
	}
	if tags, ok := args["tags"].([]interface{}); ok {
		body["tags"] = tags
	}
	if files, ok := args["files"].(map[string]interface{}); ok {
		body["files"] = files
	}
	if id := argInt(args, "git_key_id"); id > 0 {
		body["git_key_id"] = id
		body["git_auth"] = "ssh"
	}
	if _, ok := body["git_auth"]; !ok && argStr(args, "git_url") != "" {
		body["git_auth"] = "none"
	}
	for _, k := range []string{"drift_interval_minutes", "plan_interval_minutes"} {
		if v, ok := args[k].(float64); ok {
			body[k] = int(v)
		}
	}
	if v, ok := args["auto_apply"].(bool); ok {
		body["auto_apply"] = v
	}
	return body
}

func toolTFRunAction(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	action := argStr(args, "action")
	runID := argInt(args, "run_id")
	wsID := argInt(args, "workspace_id")

	switch action {
	case "plan", "refresh", "destroy":
		if wsID < 1 {
			return "", fmt.Errorf("workspace_id is required")
		}
		typ := "plan"
		if action == "destroy" {
			typ = "destroy"
		} else if action == "refresh" {
			typ = "refresh"
		}
		name, locked, err := s.tfWorkspaceName(r.Context(), wsID)
		if err != nil {
			return "", err
		}
		if locked {
			return "", fmt.Errorf("workspace %q is locked (another run is active or awaiting apply); check tf_runs", name)
		}
		gate := gateRoutine
		if typ == "destroy" {
			gate = gateInfra
		}
		summary := fmt.Sprintf("terraform %s on workspace %q (nothing is changed yet)", typ, name)
		if typ == "destroy" {
			summary += "\nA destroy plan lists what would be REMOVED from the infrastructure; it is applied only by tf_run_action apply."
		}
		if err := s.agentGateAs(r, 0, "Terraform", "tf_run_action", summary, gate); err != nil {
			return "", err
		}
		body := map[string]interface{}{"type": typ}
		if msg := argStr(args, "message"); msg != "" {
			body["message"] = clipTo(msg, 200)
		}
		if raw, ok := args["targets"].([]interface{}); ok && len(raw) > 0 {
			body["targets"] = raw
		}
		if raw, ok := args["replace"].([]interface{}); ok && len(raw) > 0 {
			body["replace"] = raw
		}
		m, err := s.loopJSON(r.Context(), http.MethodPost, fmt.Sprintf("/api/terraform/workspaces/%d/runs", wsID), body)
		if err != nil {
			return "", err
		}
		run, _ := m["run"].(map[string]interface{})
		return jsonText(pick(run, "id", "type", "status", "workspace_id"))

	case "cancel":
		if runID < 1 {
			return "", fmt.Errorf("run_id is required")
		}
		m, err := s.loopJSON(r.Context(), http.MethodGet, fmt.Sprintf("/api/terraform/runs/%d", runID), nil)
		if err != nil {
			return "", err
		}
		run, _ := m["run"].(map[string]interface{})
		status, _ := run["status"].(string)
		summary := fmt.Sprintf("cancel the Terraform run #%d (status %s)", runID, status)
		if err := s.agentGateAs(r, 0, "Terraform", "tf_run_action", summary, gateInfra); err != nil {
			return "", err
		}
		out, err := s.loopJSON(r.Context(), http.MethodPost, fmt.Sprintf("/api/terraform/runs/%d/cancel", runID), nil)
		if err != nil {
			return "", err
		}
		updated, _ := out["run"].(map[string]interface{})
		return jsonText(pick(updated, "id", "status"))

	case "discard":
		if runID < 1 {
			return "", fmt.Errorf("run_id is required")
		}
		comment := argStr(args, "comment")
		if comment == "" {
			return "", fmt.Errorf("comment is required")
		}
		m, err := s.loopJSON(r.Context(), http.MethodGet, fmt.Sprintf("/api/terraform/runs/%d", runID), nil)
		if err != nil {
			return "", err
		}
		run, _ := m["run"].(map[string]interface{})
		status, _ := run["status"].(string)
		if status != "awaiting_approval" {
			return "", fmt.Errorf("run %d is %q; only a finished plan (awaiting_approval) can be discarded", runID, status)
		}
		summary := fmt.Sprintf("discard the Terraform plan of run #%d (type %v): it can never be applied.\nreason: %s", runID, run["type"], clipTo(comment, 200))
		if err := s.agentGateAs(r, 0, "Terraform", "tf_run_action", summary, gateInfra); err != nil {
			return "", err
		}
		out, err := s.loopJSON(r.Context(), http.MethodPost, fmt.Sprintf("/api/terraform/runs/%d/discard", runID), map[string]interface{}{"comment": comment})
		if err != nil {
			return "", err
		}
		updated, _ := out["run"].(map[string]interface{})
		return jsonText(pick(updated, "id", "status"))
	}
	return "", fmt.Errorf("unknown action %q", action)
}

func toolTFState(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	id := argInt(args, "workspace_id")
	if id < 1 {
		return "", fmt.Errorf("workspace_id is required")
	}
	base := fmt.Sprintf("/api/terraform/workspaces/%d", id)
	switch argStr(args, "action") {
	case "state":
		m, err := s.loopJSON(r.Context(), http.MethodGet, base+"/state", nil)
		if err != nil {
			return "", err
		}
		return jsonText(pick(m, "serial", "lineage", "terraform_version", "resources"))
	case "outputs":
		m, err := s.loopJSON(r.Context(), http.MethodGet, base+"/outputs", nil)
		if err != nil {
			return "", err
		}
		// Sensitive outputs are hidden by the handler unless reveal=1 is asked for: never ask.
		return jsonText(m["outputs"])
	case "variables":
		m, err := s.loopJSON(r.Context(), http.MethodGet, base+"/variables?effective=1", nil)
		if err != nil {
			return "", err
		}
		return jsonText(m["variables"])
	case "versions":
		q := url.Values{}
		if l := argInt(args, "limit"); l > 0 && l <= 100 {
			q.Set("limit", strconv.FormatInt(l, 10))
		}
		m, err := s.loopJSON(r.Context(), http.MethodGet, base+"/state/versions?"+q.Encode(), nil)
		if err != nil {
			return "", err
		}
		return jsonText(pickAll(m["versions"], "id", "serial", "source", "size", "resource_count", "sha256", "created_at"))
	case "drift":
		m, err := s.loopJSON(r.Context(), http.MethodGet, base+"/drift", nil)
		if err != nil {
			return "", err
		}
		return jsonText(pick(m, "detected", "checked_at", "interval_minutes", "resources"))
	case "diff":
		from, to := argInt(args, "from_version_id"), argInt(args, "to_version_id")
		if from < 1 || to < 1 {
			return "", fmt.Errorf("from_version_id and to_version_id are required")
		}
		q := url.Values{"from": {strconv.FormatInt(from, 10)}, "to": {strconv.FormatInt(to, 10)}}
		m, err := s.loopJSON(r.Context(), http.MethodGet, base+"/state/diff?"+q.Encode(), nil)
		if err != nil {
			return "", err
		}
		return jsonText(m)
	case "audit":
		q := url.Values{"limit": {"50"}}
		if l := argInt(args, "limit"); l > 0 && l <= 200 {
			q.Set("limit", strconv.FormatInt(l, 10))
		}
		q.Set("workspace_id", strconv.FormatInt(id, 10))
		m, err := s.loopJSON(r.Context(), http.MethodGet, "/api/terraform/audit?"+q.Encode(), nil)
		if err != nil {
			return "", err
		}
		return jsonText(pickAll(m["events"], "time", "level", "message", "workspace"))
	}
	return "", fmt.Errorf("unknown action %q", argStr(args, "action"))
}

func toolTFWorkspaceData(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	id := argInt(args, "workspace_id")
	if id < 1 {
		return "", fmt.Errorf("workspace_id is required")
	}
	base := fmt.Sprintf("/api/terraform/workspaces/%d", id)
	action := argStr(args, "action")

	switch action {
	case "state", "outputs", "variables", "versions", "drift", "diff", "audit":
		return toolTFState(s, r, args)
	case "write_file", "delete_file":
		path := argStr(args, "path")
		if path == "" {
			return "", fmt.Errorf("path is required")
		}
		m, err := s.loopJSON(r.Context(), http.MethodGet, base, nil)
		if err != nil {
			return "", err
		}
		ws, _ := m["workspace"].(map[string]interface{})
		files, _ := ws["files"].(map[string]interface{})
		if files == nil {
			files = map[string]interface{}{}
		}
		red := map[string]interface{}{}
		for k, v := range files {
			if txt, ok := v.(string); ok {
				red[k] = txt
			}
		}
		if action == "delete_file" {
			if _, ok := red[path]; !ok {
				return "", fmt.Errorf("the workspace has no file %q", path)
			}
			delete(red, path)
		} else {
			content, _ := args["content"].(string)
			if content == "" {
				return "", fmt.Errorf("content is required")
			}
			red[path] = content
		}
		summary := fmt.Sprintf("%s the file %q of the Terraform workspace (id %d)", strings.ReplaceAll(action, "_", " "), path, id)
		if err := s.agentGateAs(r, 0, "Terraform", "tf_workspace_data", summary, gateInfra); err != nil {
			return "", err
		}
		out, err := s.loopJSON(r.Context(), http.MethodPut, base, map[string]interface{}{"files": red})
		if err != nil {
			return "", err
		}
		updated, _ := out["workspace"].(map[string]interface{})
		return jsonText(pick(updated, "id", "name", "updated_at"))

	case "set_variable", "delete_variable":
		key := argStr(args, "key")
		if key == "" {
			return "", fmt.Errorf("key is required")
		}
		m, err := s.loopJSON(r.Context(), http.MethodGet, base+"/variables", nil)
		if err != nil {
			return "", err
		}
		vars, _ := m["variables"].([]interface{})
		var vid int64
		for _, v := range vars {
			obj, _ := v.(map[string]interface{})
			k, _ := obj["key"].(string)
			if k == key {
				if f, ok := obj["id"].(float64); ok {
					vid = int64(f)
				}
			}
		}
		if action == "delete_variable" {
			if vid < 1 {
				return "", fmt.Errorf("the workspace has no variable %q", key)
			}
			summary := fmt.Sprintf("delete the variable %q of the Terraform workspace (id %d)", key, id)
			if err := s.agentGateAs(r, 0, "Terraform", "tf_workspace_data", summary, gateInfra); err != nil {
				return "", err
			}
			if _, err := s.loopJSON(r.Context(), http.MethodDelete, fmt.Sprintf("%s/variables/%d", base, vid), nil); err != nil {
				return "", err
			}
			return "deleted the variable " + key, nil
		}
		body := map[string]interface{}{"key": key}
		if v, ok := args["value"].(string); ok {
			body["value"] = v
		}
		if v := argStr(args, "category"); v != "" {
			body["category"] = v
		}
		if v, ok := args["sensitive"].(bool); ok {
			body["sensitive"] = v
		}
		if v := argStr(args, "description"); v != "" {
			body["description"] = v
		}
		method, path := http.MethodPost, base+"/variables"
		if vid > 0 {
			method, path = http.MethodPut, fmt.Sprintf("%s/variables/%d", base, vid)
		}
		summary := fmt.Sprintf("%s the variable %q of the Terraform workspace (id %d)", strings.ReplaceAll(action, "_", " "), key, id)
		if err := s.agentGateAs(r, 0, "Terraform", "tf_workspace_data", summary, gateInfra); err != nil {
			return "", err
		}
		out, err := s.loopJSON(r.Context(), method, path, body)
		if err != nil {
			return "", err
		}
		return jsonText(pick(out, "id", "key", "category", "sensitive", "value_set"))

	case "state_op":
		body := map[string]interface{}{"op": argStr(args, "op"), "address": argStr(args, "address")}
		if v := argStr(args, "to"); v != "" {
			body["to"] = v
		}
		if v := argStr(args, "import_id"); v != "" {
			body["import_id"] = v
		}
		summary := fmt.Sprintf("terraform %s on the state of the workspace (id %d): %s", argStr(args, "op"), id, iacJSON(body))
		if err := s.agentGateAs(r, 0, "Terraform", "tf_workspace_data", summary, gateInfra); err != nil {
			return "", err
		}
		m, err := s.loopJSON(r.Context(), http.MethodPost, base+"/state/ops", body)
		if err != nil {
			return "", err
		}
		out := pick(m, "state_version")
		if o, ok := m["output"].(string); ok {
			out["output"] = redactSecrets(clipTo(o, 4<<10))
		}
		return jsonText(out)

	case "state_import":
		state, _ := args["content"].(string)
		if strings.TrimSpace(state) == "" {
			return "", fmt.Errorf("content (the terraform state JSON) is required")
		}
		summary := fmt.Sprintf("import a whole terraform state file into the workspace (id %d) as a new state version", id)
		if err := s.agentGateAs(r, 0, "Terraform", "tf_workspace_data", summary, gateInfra); err != nil {
			return "", err
		}
		m, err := s.loopJSON(r.Context(), http.MethodPost, base+"/state/import", map[string]interface{}{"state": state})
		if err != nil {
			return "", err
		}
		v, _ := m["version"].(map[string]interface{})
		return jsonText(pick(v, "id", "serial", "source", "resource_count", "sha256"))

	case "rollback":
		vid := argInt(args, "version_id")
		if vid < 1 {
			return "", fmt.Errorf("version_id is required")
		}
		summary := fmt.Sprintf("roll the state of the workspace (id %d) back to the state version #%d", id, vid)
		if err := s.agentGateAs(r, 0, "Terraform", "tf_workspace_data", summary, gateInfra); err != nil {
			return "", err
		}
		m, err := s.loopJSON(r.Context(), http.MethodPost, fmt.Sprintf("%s/state/versions/%d/rollback", base, vid), map[string]interface{}{})
		if err != nil {
			return "", err
		}
		v, _ := m["version"].(map[string]interface{})
		return jsonText(pick(v, "id", "serial", "source", "resource_count", "sha256"))

	case "fmt", "validate":
		files, ok := args["files"].(map[string]interface{})
		if !ok || len(files) == 0 {
			m, err := s.loopJSON(r.Context(), http.MethodGet, base, nil)
			if err != nil {
				return "", err
			}
			ws, _ := m["workspace"].(map[string]interface{})
			stored, _ := ws["files"].(map[string]interface{})
			if len(stored) == 0 {
				return "", fmt.Errorf("the workspace has no files")
			}
			files = stored
		}
		// fmt and validate are server-side dry runs: they change nothing, so they need no gate.
		preview := true
		m, err := s.loopJSON(context.WithValue(r.Context(), gateKey{}, &preview), http.MethodPost, base+"/tools/"+action, map[string]interface{}{"files": files})
		if err != nil {
			return "", err
		}
		if action == "fmt" {
			out := map[string]interface{}{}
			if f, ok := m["files"].(map[string]interface{}); ok {
				for k, v := range f {
					if txt, ok := v.(string); ok {
						out[k] = clipTo(txt, 8<<10)
					}
				}
			}
			return jsonText(out)
		}
		return jsonText(m)
	}
	return "", fmt.Errorf("unknown action %q", action)
}

func toolTFCatalog(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	action := argStr(args, "action")
	switch action {
	case "create_policy", "update_policy", "delete_policy", "create_module", "delete_module", "create_varset", "delete_varset":
		return toolTFCatalogAction(s, r, args)
	}
	switch action {
	case "policies":
		m, err := s.loopJSON(r.Context(), http.MethodGet, "/api/terraform/policies", nil)
		if err != nil {
			return "", err
		}
		return jsonText(pickAll(m["policies"], "id", "name", "description", "kind", "enforcement", "enabled", "workspace_id"))
	case "modules":
		m, err := s.loopJSON(r.Context(), http.MethodGet, "/api/terraform/modules", nil)
		if err != nil {
			return "", err
		}
		return jsonText(pickAll(m["modules"], "id", "name", "provider", "description"))
	case "varsets":
		m, err := s.loopJSON(r.Context(), http.MethodGet, "/api/terraform/varsets", nil)
		if err != nil {
			return "", err
		}
		return jsonText(pickAll(m["varsets"], "id", "name", "description", "global", "workspace_ids"))
	case "templates":
		m, err := s.loopJSON(r.Context(), http.MethodGet, "/api/terraform/templates", nil)
		if err != nil {
			return "", err
		}
		return tfTemplateAnswer(args, m["templates"])
	case "registry_modules":
		q := url.Values{}
		if v := argStr(args, "query"); v != "" {
			q.Set("q", v)
		}
		if l := argInt(args, "limit"); l > 0 && l <= 50 {
			q.Set("limit", strconv.FormatInt(l, 10))
		}
		m, err := s.loopJSON(r.Context(), http.MethodGet, "/api/terraform/registry/modules?"+q.Encode(), nil)
		if err != nil {
			return "", err
		}
		return jsonText(m)
	case "registry_providers":
		q := url.Values{}
		if v := argStr(args, "query"); v != "" {
			q.Set("q", v)
		}
		m, err := s.loopJSON(r.Context(), http.MethodGet, "/api/terraform/registry/providers?"+q.Encode(), nil)
		if err != nil {
			return "", err
		}
		return jsonText(m)
	case "status":
		m, err := s.loopJSON(r.Context(), http.MethodGet, "/api/terraform/status", nil)
		if err != nil {
			return "", err
		}
		return jsonText(pick(m, "enabled", "supported", "binaries"))
	}
	return "", fmt.Errorf("unknown action %q", action)
}

// tfTemplateAnswer serves the templates action in one answer: the compact index of every template (filtered by
// query), or the full files of the single template named by template_id. The index is what the model needs to
// choose; the files are only fetched for the template it picked.
func tfTemplateAnswer(args map[string]interface{}, templates interface{}) (string, error) {
	list, _ := templates.([]interface{})
	if id := argStr(args, "template_id"); id != "" {
		for _, t := range list {
			if obj, ok := t.(map[string]interface{}); ok && obj["id"] == id {
				return jsonText(obj)
			}
		}
		return "", fmt.Errorf("no template %q: call tf_catalog with action templates for the index", id)
	}
	q := strings.ToLower(argStr(args, "query"))
	items := make([]map[string]interface{}, 0, len(list))
	for _, t := range list {
		obj, ok := t.(map[string]interface{})
		if !ok {
			continue
		}
		id, _ := obj["id"].(string)
		name, _ := obj["name"].(string)
		provider, _ := obj["provider"].(string)
		desc, _ := obj["description"].(string)
		if q != "" && !strings.Contains(strings.ToLower(id+" "+name+" "+provider+" "+desc), q) {
			continue
		}
		items = append(items, map[string]interface{}{
			"id": id, "name": name, "provider": provider, "category": provider,
			"summary": clipTo(desc, 120), "tags": []string{provider},
		})
	}
	return jsonText(map[string]interface{}{"count": len(items), "templates": items})
}

func toolTFCatalogAction(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	action := argStr(args, "action")
	switch action {
	case "create_policy":
		body := map[string]interface{}{"name": argStr(args, "name")}
		if v := argStr(args, "description"); v != "" {
			body["description"] = v
		}
		if v := argStr(args, "kind"); v != "" {
			body["kind"] = v
		}
		if v := argStr(args, "enforcement"); v != "" {
			body["enforcement"] = v
		}
		if p, ok := args["params"].(map[string]interface{}); ok {
			body["params"] = p
		}
		if v, ok := args["enabled"].(bool); ok {
			body["enabled"] = v
		}
		if id := argInt(args, "workspace_id"); id > 0 {
			body["workspace_id"] = id
		}
		summary := fmt.Sprintf("create the Terraform policy %q\n%s", argStr(args, "name"), clipTo(iacJSON(body), 1500))
		if err := s.agentGateAs(r, 0, "Terraform", "tf_catalog_action", summary, gateInfra); err != nil {
			return "", err
		}
		m, err := s.loopJSON(r.Context(), http.MethodPost, "/api/terraform/policies", body)
		if err != nil {
			return "", err
		}
		return jsonText(pick(m, "id", "name", "kind", "enforcement", "enabled"))

	case "update_policy":
		id := argInt(args, "policy_id")
		if id < 1 {
			return "", fmt.Errorf("policy_id is required")
		}
		body := map[string]interface{}{}
		for _, k := range []string{"name", "description", "kind", "enforcement"} {
			if v := argStr(args, k); v != "" {
				body[k] = v
			}
		}
		if p, ok := args["params"].(map[string]interface{}); ok {
			body["params"] = p
		}
		if v, ok := args["enabled"].(bool); ok {
			body["enabled"] = v
		}
		if ws := argInt(args, "workspace_id"); ws > 0 {
			body["workspace_id"] = ws
		}
		summary := fmt.Sprintf("update the Terraform policy #%d\n%s", id, clipTo(iacJSON(body), 1500))
		if err := s.agentGateAs(r, 0, "Terraform", "tf_catalog_action", summary, gateInfra); err != nil {
			return "", err
		}
		m, err := s.loopJSON(r.Context(), http.MethodPut, fmt.Sprintf("/api/terraform/policies/%d", id), body)
		if err != nil {
			return "", err
		}
		return jsonText(pick(m, "id", "name", "kind", "enforcement", "enabled"))

	case "delete_policy":
		id := argInt(args, "policy_id")
		if id < 1 {
			return "", fmt.Errorf("policy_id is required")
		}
		summary := fmt.Sprintf("delete the Terraform policy #%d", id)
		if err := s.agentGateAs(r, 0, "Terraform", "tf_catalog_action", summary, gateInfra); err != nil {
			return "", err
		}
		if _, err := s.loopJSON(r.Context(), http.MethodDelete, fmt.Sprintf("/api/terraform/policies/%d", id), nil); err != nil {
			return "", err
		}
		return fmt.Sprintf("deleted the Terraform policy #%d", id), nil

	case "create_module":
		body := map[string]interface{}{"name": argStr(args, "name"), "provider": argStr(args, "provider")}
		if v := argStr(args, "description"); v != "" {
			body["description"] = v
		}
		summary := fmt.Sprintf("create the Terraform module %q for provider %q", argStr(args, "name"), argStr(args, "provider"))
		if err := s.agentGateAs(r, 0, "Terraform", "tf_catalog_action", summary, gateInfra); err != nil {
			return "", err
		}
		m, err := s.loopJSON(r.Context(), http.MethodPost, "/api/terraform/modules", body)
		if err != nil {
			return "", err
		}
		return jsonText(pick(m, "id", "name", "provider", "description"))

	case "delete_module":
		id := argInt(args, "module_id")
		if id < 1 {
			return "", fmt.Errorf("module_id is required")
		}
		summary := fmt.Sprintf("delete the Terraform module #%d and its versions", id)
		if err := s.agentGateAs(r, 0, "Terraform", "tf_catalog_action", summary, gateInfra); err != nil {
			return "", err
		}
		if _, err := s.loopJSON(r.Context(), http.MethodDelete, fmt.Sprintf("/api/terraform/modules/%d", id), nil); err != nil {
			return "", err
		}
		return fmt.Sprintf("deleted the Terraform module #%d", id), nil

	case "create_varset":
		body := map[string]interface{}{"name": argStr(args, "name")}
		if v := argStr(args, "description"); v != "" {
			body["description"] = v
		}
		if v, ok := args["global"].(bool); ok {
			body["global"] = v
		}
		summary := fmt.Sprintf("create the Terraform variable set %q", argStr(args, "name"))
		if err := s.agentGateAs(r, 0, "Terraform", "tf_catalog_action", summary, gateInfra); err != nil {
			return "", err
		}
		m, err := s.loopJSON(r.Context(), http.MethodPost, "/api/terraform/varsets", body)
		if err != nil {
			return "", err
		}
		return jsonText(pick(m, "id", "name", "global"))

	case "delete_varset":
		id := argInt(args, "varset_id")
		if id < 1 {
			return "", fmt.Errorf("varset_id is required")
		}
		summary := fmt.Sprintf("delete the Terraform variable set #%d and its variables", id)
		if err := s.agentGateAs(r, 0, "Terraform", "tf_catalog_action", summary, gateInfra); err != nil {
			return "", err
		}
		if _, err := s.loopJSON(r.Context(), http.MethodDelete, fmt.Sprintf("/api/terraform/varsets/%d", id), nil); err != nil {
			return "", err
		}
		return fmt.Sprintf("deleted the Terraform variable set #%d", id), nil

	case "varset_variables":
		id := argInt(args, "varset_id")
		if id < 1 {
			return "", fmt.Errorf("varset_id is required")
		}
		m, err := s.loopJSON(r.Context(), http.MethodGet, fmt.Sprintf("/api/terraform/varsets/%d/variables", id), nil)
		if err != nil {
			return "", err
		}
		return jsonText(m["variables"])

	case "set_varset_variable":
		id := argInt(args, "varset_id")
		if id < 1 {
			return "", fmt.Errorf("varset_id is required")
		}
		key := argStr(args, "key")
		if key == "" {
			return "", fmt.Errorf("key is required")
		}
		body := map[string]interface{}{"key": key}
		if v, ok := args["value"].(string); ok {
			body["value"] = v
		}
		if v := argStr(args, "category"); v != "" {
			body["category"] = v
		}
		if v, ok := args["sensitive"].(bool); ok {
			body["sensitive"] = v
		}
		if v := argStr(args, "description"); v != "" {
			body["description"] = v
		}
		summary := fmt.Sprintf("set the variable %q of the Terraform variable set #%d", key, id)
		if err := s.agentGateAs(r, 0, "Terraform", "tf_catalog_action", summary, gateInfra); err != nil {
			return "", err
		}
		m, err := s.loopJSON(r.Context(), http.MethodPost, fmt.Sprintf("/api/terraform/varsets/%d/variables", id), body)
		if err != nil {
			return "", err
		}
		return jsonText(pick(m, "id", "key", "category", "sensitive", "value_set"))

	case "delete_varset_variable":
		id := argInt(args, "varset_id")
		vid := argInt(args, "variable_id")
		if id < 1 || vid < 1 {
			return "", fmt.Errorf("varset_id and variable_id are required")
		}
		summary := fmt.Sprintf("delete the variable #%d of the Terraform variable set #%d", vid, id)
		if err := s.agentGateAs(r, 0, "Terraform", "tf_catalog_action", summary, gateInfra); err != nil {
			return "", err
		}
		if _, err := s.loopJSON(r.Context(), http.MethodDelete, fmt.Sprintf("/api/terraform/varsets/%d/variables/%d", id, vid), nil); err != nil {
			return "", err
		}
		return fmt.Sprintf("deleted the variable #%d of the variable set #%d", vid, id), nil

	case "cloud_accounts":
		m, err := s.loopJSON(r.Context(), http.MethodGet, "/api/terraform/cloud-accounts", nil)
		if err != nil {
			return "", err
		}
		return jsonText(m["accounts"])

	case "importable_accounts":
		m, err := s.loopJSON(r.Context(), http.MethodGet, "/api/terraform/cloud-accounts/importable", nil)
		if err != nil {
			return "", err
		}
		return jsonText(m)

	case "create_cloud_account":
		name := argStr(args, "name")
		if name == "" {
			return "", fmt.Errorf("name is required")
		}
		impType := argStr(args, "import_type")
		impID := argInt(args, "import_id")
		if impType == "" || impID < 1 {
			return "", fmt.Errorf("import_type (cloudflare or kubernetes) and import_id (the Pantau account id) are required: a secret never crosses the model")
		}
		body := map[string]interface{}{"name": name, "import_from": map[string]interface{}{"type": impType, "id": impID}}
		if v := argStr(args, "description"); v != "" {
			body["description"] = v
		}
		summary := fmt.Sprintf("create the Terraform cloud account %q, copying the credentials of the %s account #%d", name, impType, impID)
		if err := s.agentGateAs(r, 0, "Terraform", "tf_catalog_action", summary, gateInfra); err != nil {
			return "", err
		}
		m, err := s.loopJSON(r.Context(), http.MethodPost, "/api/terraform/cloud-accounts", body)
		if err != nil {
			return "", err
		}
		return jsonText(pick(m, "id", "name", "provider", "description", "secrets_set"))

	case "update_cloud_account":
		id := argInt(args, "account_id")
		if id < 1 {
			return "", fmt.Errorf("account_id is required")
		}
		body := map[string]interface{}{}
		for _, k := range []string{"name", "description"} {
			if v := argStr(args, k); v != "" {
				body[k] = v
			}
		}
		summary := fmt.Sprintf("update the Terraform cloud account #%d\n%s", id, clipTo(iacJSON(body), 500))
		if err := s.agentGateAs(r, 0, "Terraform", "tf_catalog_action", summary, gateInfra); err != nil {
			return "", err
		}
		m, err := s.loopJSON(r.Context(), http.MethodPut, fmt.Sprintf("/api/terraform/cloud-accounts/%d", id), body)
		if err != nil {
			return "", err
		}
		return jsonText(pick(m, "id", "name", "provider", "description", "secrets_set"))

	case "delete_cloud_account":
		id := argInt(args, "account_id")
		if id < 1 {
			return "", fmt.Errorf("account_id is required")
		}
		summary := fmt.Sprintf("delete the Terraform cloud account #%d and its stored credentials", id)
		if err := s.agentGateAs(r, 0, "Terraform", "tf_catalog_action", summary, gateInfra); err != nil {
			return "", err
		}
		if _, err := s.loopJSON(r.Context(), http.MethodDelete, fmt.Sprintf("/api/terraform/cloud-accounts/%d", id), nil); err != nil {
			return "", err
		}
		return fmt.Sprintf("deleted the Terraform cloud account #%d", id), nil

	case "test_cloud_account":
		id := argInt(args, "account_id")
		if id < 1 {
			return "", fmt.Errorf("account_id is required")
		}
		summary := fmt.Sprintf("test the stored credentials of the Terraform cloud account #%d", id)
		if err := s.agentGateAs(r, 0, "Terraform", "tf_catalog_action", summary, gateInfra); err != nil {
			return "", err
		}
		m, err := s.loopJSON(r.Context(), http.MethodPost, fmt.Sprintf("/api/terraform/cloud-accounts/%d/test", id), nil)
		if err != nil {
			return "", err
		}
		return jsonText(m)

	case "set_varset_workspaces":
		id := argInt(args, "varset_id")
		if id < 1 {
			return "", fmt.Errorf("varset_id is required")
		}
		raw, _ := args["workspace_ids"].([]interface{})
		ids := []int64{}
		for _, v := range raw {
			if f, ok := v.(float64); ok {
				ids = append(ids, int64(f))
			}
		}
		summary := fmt.Sprintf("bind the Terraform variable set #%d to the workspaces %v", id, ids)
		if err := s.agentGateAs(r, 0, "Terraform", "tf_catalog_action", summary, gateInfra); err != nil {
			return "", err
		}
		m, err := s.loopJSON(r.Context(), http.MethodPut, fmt.Sprintf("/api/terraform/varsets/%d/workspaces", id), map[string]interface{}{"workspace_ids": ids})
		if err != nil {
			return "", err
		}
		return jsonText(pick(m, "id", "name", "workspace_ids"))
	}
	return "", fmt.Errorf("unknown action %q", action)
}

// ---------------------------------------------------------------- Kubernetes

func toolKubeClusterAction(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	id := argInt(args, "cluster_id")
	if id < 1 {
		return "", fmt.Errorf("cluster_id is required")
	}
	c, err := s.db.GetKubeCluster(id)
	if err != nil || c == nil {
		return "", fmt.Errorf("no cluster #%d; see kube_targets", id)
	}
	switch argStr(args, "action") {
	case "rename":
		name := argStr(args, "name")
		if name == "" {
			return "", fmt.Errorf("name is required")
		}
		summary := fmt.Sprintf("rename the Kubernetes cluster %q (id %d) to %q", c.Name, id, name)
		if err := s.agentGateAs(r, 0, "Kubernetes", "kube_cluster_action", summary, gateInfra); err != nil {
			return "", err
		}
		m, err := s.loopJSON(r.Context(), http.MethodPut, fmt.Sprintf("/api/kube/clusters/%d", id), map[string]interface{}{"name": name, "kube_mode": c.KubeMode})
		if err != nil {
			return "", err
		}
		cl, _ := m["cluster"].(map[string]interface{})
		return jsonText(pick(cl, "id", "name", "kube_mode"))

	case "set_mode":
		mode := argStr(args, "kube_mode")
		if mode == "" {
			return "", fmt.Errorf("kube_mode is required")
		}
		summary := fmt.Sprintf("set the access mode of the Kubernetes cluster %q (id %d) to %q", c.Name, id, mode)
		if mode == "read-only" {
			summary += "\nread-only refuses every change on this cluster from now on."
		}
		if err := s.agentGateAs(r, 0, "Kubernetes", "kube_cluster_action", summary, gateInfra); err != nil {
			return "", err
		}
		m, err := s.loopJSON(r.Context(), http.MethodPut, fmt.Sprintf("/api/kube/clusters/%d", id), map[string]interface{}{"name": c.Name, "kube_mode": mode})
		if err != nil {
			return "", err
		}
		cl, _ := m["cluster"].(map[string]interface{})
		return jsonText(pick(cl, "id", "name", "kube_mode"))

	case "delete":
		summary := fmt.Sprintf("delete the Kubernetes cluster %q (id %d) and its stored credentials.\nThe cluster itself keeps running: nothing on it is changed.", c.Name, id)
		if err := s.agentGateAs(r, 0, "Kubernetes", "kube_cluster_action", summary, gateInfra); err != nil {
			return "", err
		}
		if _, err := s.loopJSON(r.Context(), http.MethodDelete, fmt.Sprintf("/api/kube/clusters/%d", id), nil); err != nil {
			return "", err
		}
		return "deleted the Kubernetes cluster " + c.Name, nil
	}
	return "", fmt.Errorf("unknown action %q", argStr(args, "action"))
}

// ---------------------------------------------------------------- Repositories

func toolRepoAction(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	action := argStr(args, "action")
	switch action {
	case "clone":
		url := argStr(args, "url")
		if url == "" {
			return "", fmt.Errorf("url is required")
		}
		body := map[string]interface{}{"url": url}
		if v := argStr(args, "name"); v != "" {
			body["name"] = v
		}
		if v := argStr(args, "branch"); v != "" {
			body["branch"] = v
		}
		if v := argStr(args, "group"); v != "" {
			body["group"] = v
		}
		auth := argStr(args, "auth")
		if auth == "" {
			auth = "none"
		}
		body["auth"] = auth
		if auth == "ssh" {
			if id := argInt(args, "key_id"); id > 0 {
				body["key_id"] = id
			} else {
				return "", fmt.Errorf("key_id (an SSH Key Library entry id) is required for ssh authentication")
			}
		}
		if auth == "token" {
			return "", fmt.Errorf("token authentication needs a token, which never crosses the model: ask the operator to clone it, or use ssh with a key_id")
		}
		summary := fmt.Sprintf("clone the Git repository %s into the Pantau server (auth %s)", url, auth)
		if err := s.agentGateAs(r, 0, "Repositories", "repo_action", summary, gateInfra); err != nil {
			return "", err
		}
		m, err := s.loopJSON(r.Context(), http.MethodPost, "/api/repos", body)
		if err != nil {
			return "", err
		}
		job, _ := m["job"].(string)
		return "the clone of " + url + " started (job " + job + "); it finishes in the background, list it with repo_list", nil

	case "delete":
		repo, _, err := s.agentRepo(args)
		if err != nil {
			return "", err
		}
		summary := fmt.Sprintf("delete the repository %q and its clone folder %s", repoLabel(repo), repo.Dir)
		if err := s.agentGateAs(r, 0, "Repositories", "repo_action", summary, gateInfra); err != nil {
			return "", err
		}
		if _, err := s.loopJSON(r.Context(), http.MethodDelete, fmt.Sprintf("/api/repos/%d", repo.ID), nil); err != nil {
			return "", err
		}
		return "deleted the repository " + repoLabel(repo), nil

	case "move":
		repo, _, err := s.agentRepo(args)
		if err != nil {
			return "", err
		}
		group := argStr(args, "group")
		summary := fmt.Sprintf("move the repository %q to the folder %q (empty: no folder)", repoLabel(repo), group)
		if err := s.agentGateAs(r, 0, "Repositories", "repo_action", summary, gateInfra); err != nil {
			return "", err
		}
		m, err := s.loopJSON(r.Context(), http.MethodPut, fmt.Sprintf("/api/repos/%d/group", repo.ID), map[string]interface{}{"group": group})
		if err != nil {
			return "", err
		}
		return jsonText(pick(m, "id", "name", "group", "dir"))

	case "folder":
		m, err := s.loopJSON(r.Context(), http.MethodGet, "/api/repos/folder", nil)
		if err != nil {
			return "", err
		}
		return jsonText(pick(m, "folder", "custom"))

	case "set_folder":
		folder := argStr(args, "folder")
		if folder != "" && !strings.HasPrefix(folder, "/") {
			return "", fmt.Errorf("folder must be an absolute path (empty = the default folder)")
		}
		summary := fmt.Sprintf("set the folder new repository clones are created in to %q (empty = the default folder)", folder)
		if err := s.agentGateAs(r, 0, "Repositories", "repo_action", summary, gateInfra); err != nil {
			return "", err
		}
		m, err := s.loopJSON(r.Context(), http.MethodPut, "/api/repos/folder", map[string]interface{}{"folder": folder})
		if err != nil {
			return "", err
		}
		return jsonText(pick(m, "folder", "custom"))

	case "set_auth":
		repo, _, err := s.agentRepo(args)
		if err != nil {
			return "", err
		}
		auth := argStr(args, "auth")
		if auth == "" {
			auth = "none"
		}
		body := map[string]interface{}{"auth": auth}
		if auth == "ssh" {
			if id := argInt(args, "key_id"); id > 0 {
				body["key_id"] = id
			} else {
				return "", fmt.Errorf("key_id (an SSH Key Library entry id) is required for ssh authentication")
			}
		}
		if auth == "token" {
			return "", fmt.Errorf("token authentication needs a token, which never crosses the model: ask the operator to set it in Pantau")
		}
		summary := fmt.Sprintf("change the credentials of the repository %q to %s", repoLabel(repo), auth)
		if err := s.agentGateAs(r, 0, "Repositories", "repo_action", summary, gateInfra); err != nil {
			return "", err
		}
		m, err := s.loopJSON(r.Context(), http.MethodPut, fmt.Sprintf("/api/repos/%d", repo.ID), body)
		if err != nil {
			return "", err
		}
		return jsonText(pick(m, "id", "name", "auth", "key_id", "token_set"))
	}
	return "", fmt.Errorf("unknown action %q", action)
}

// ---------------------------------------------------------------- Docker Hub and self-update

func toolDockerHubSearch(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	switch argStr(args, "action") {
	case "search":
		q := url.Values{}
		q.Set("q", argStr(args, "query"))
		if p := argInt(args, "page"); p > 0 {
			q.Set("page", strconv.FormatInt(p, 10))
		}
		m, err := s.loopJSON(r.Context(), http.MethodGet, "/api/dockerhub/search?"+q.Encode(), nil)
		if err != nil {
			return "", err
		}
		return jsonText(m)
	case "repo":
		ns, name := argStr(args, "namespace"), argStr(args, "name")
		if ns == "" || name == "" {
			return "", fmt.Errorf("namespace and name are required")
		}
		m, err := s.loopJSON(r.Context(), http.MethodGet, "/api/dockerhub/repo/"+url.PathEscape(ns)+"/"+url.PathEscape(name), nil)
		if err != nil {
			return "", err
		}
		return jsonText(m)
	case "tags":
		ns, name := argStr(args, "namespace"), argStr(args, "name")
		if ns == "" || name == "" {
			return "", fmt.Errorf("namespace and name are required")
		}
		q := url.Values{}
		if v := argStr(args, "query"); v != "" {
			q.Set("name", v)
		}
		if p := argInt(args, "page"); p > 0 {
			q.Set("page", strconv.FormatInt(p, 10))
		}
		m, err := s.loopJSON(r.Context(), http.MethodGet, "/api/dockerhub/tags/"+url.PathEscape(ns)+"/"+url.PathEscape(name)+"?"+q.Encode(), nil)
		if err != nil {
			return "", err
		}
		return jsonText(m)
	case "update":
		return toolUpdateCheck(s, r, args)
	}
	return "", fmt.Errorf("unknown action %q", argStr(args, "action"))
}

func toolUpdateCheck(s *Server, r *http.Request, _ map[string]interface{}) (string, error) {
	m, err := s.loopJSON(r.Context(), http.MethodGet, "/api/update/check", nil)
	if err != nil {
		return "", err
	}
	return jsonText(m)
}
