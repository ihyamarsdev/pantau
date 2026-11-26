package web

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"pantau/internal/store"
)

func TestDomainRouterToolSubsets(t *testing.T) {
	has := func(specs []llmToolSpec, name string) bool {
		for _, s := range specs {
			if s.Name == name {
				return true
			}
		}
		return false
	}

	cases := []struct {
		domain    toolDomain
		mustHave  []string
		mustAvoid []string
	}{
		{
			domain:    domainCore,
			mustHave:  []string{"list_hosts", "get_host_state", "list_alerts", "exec_command", "find_tools", "set_tool_domain", "memory_save"},
			mustAvoid: []string{"systemd_list", "kube_get", "docker_list", "ansible_runs", "tf_runs", "security_status"},
		},
		{
			domain:    domainHost,
			mustHave:  []string{"list_hosts", "systemd_list", "systemd_action", "host_info", "disk_usage", "pkg_updates"},
			mustAvoid: []string{"kube_get", "docker_list", "ansible_runs", "tf_runs"},
		},
		{
			domain:    domainKubernetes,
			mustHave:  []string{"list_hosts", "kube_targets", "kube_get", "kube_describe", "kube_logs", "kube_apply"},
			mustAvoid: []string{"systemd_list", "docker_list", "ansible_runs", "tf_runs", "cf_dns_list"},
		},
		{
			domain:    domainDocker,
			mustHave:  []string{"list_hosts", "docker_list", "docker_logs", "docker_inspect", "docker_action", "dockerhub_search"},
			mustAvoid: []string{"systemd_list", "kube_get", "ansible_runs", "tf_runs", "docker_security"},
		},
		{
			domain:    domainAnsible,
			mustHave:  []string{"list_hosts", "ansible_playbooks", "ansible_runs", "ansible_run", "gateway_cli", "list_vault_keys"},
			mustAvoid: []string{"systemd_list", "kube_get", "docker_list", "tf_runs", "cf_dns_list"},
		},
		{
			domain:    domainTerraform,
			mustHave:  []string{"list_hosts", "tf_workspaces", "tf_runs", "tf_run_action", "gateway_cli", "list_vault_keys"},
			mustAvoid: []string{"systemd_list", "kube_get", "docker_list", "ansible_runs", "cf_dns_list"},
		},
		{
			domain:    domainSecurity,
			mustHave:  []string{"list_hosts", "security_status", "security_scan", "vuln_status", "docker_security", "iac_findings", "fim_diff"},
			mustAvoid: []string{"systemd_list", "kube_get", "docker_list", "ansible_runs", "tf_runs"},
		},
	}

	for _, tc := range cases {
		specs := agentToolSpecsFor(tc.domain)
		for _, want := range tc.mustHave {
			if !has(specs, want) {
				t.Errorf("domain %s missing expected tool %s", tc.domain, want)
			}
		}
		for _, forbid := range tc.mustAvoid {
			if has(specs, forbid) {
				t.Errorf("domain %s must not include tool %s", tc.domain, forbid)
			}
		}
	}
}

func TestRouteAgentDomainIntentAndContext(t *testing.T) {
	srv, _ := nativeAgent(t, &fakeLLM{})
	c := srv.agentConfig()

	tests := []struct {
		page string
		msg  string
		want toolDomain
	}{
		{"", "check pod status", domainAll},
		{"kube", "check anything", domainKubernetes},
		{"docker", "check anything", domainDocker},
		{"ansible", "check anything", domainAnsible},
		{"terraform", "check anything", domainTerraform},
		{"security", "check anything", domainSecurity},
		{"global", "why is pod api-0 in CrashLoopBackOff?", domainKubernetes},
		{"hosts", "docker container nginx keeps restarting", domainDocker},
		{"global", "run terraform plan on prod workspace", domainTerraform},
		{"hosts", "ansible playbook failed on web1", domainAnsible},
		{"global", "check CVE vulnerabilities and hardening", domainSecurity},
		{"global", "hello", domainCore},
		{"hosts", "hello", domainHost},
	}
	for _, tc := range tests {
		got := srv.routeAgentDomain(context.Background(), c, tc.page, tc.msg)
		if got != tc.want {
			t.Errorf("routeAgentDomain(page=%q, msg=%q) = %q, want %q", tc.page, tc.msg, got, tc.want)
		}
	}
}

func TestDynamicSkillPromptAndScopedMemory(t *testing.T) {
	// Default SOUL with a concrete domain uses the compact core + domain skill prompt, saving tokens.
	k8sPrompt := agentSystemPromptFor(defaultAgentSoul, domainKubernetes)
	if len(k8sPrompt) >= len(defaultAgentSoul) {
		t.Fatalf("dynamic skill prompt (%d bytes) should be smaller than full default SOUL (%d bytes)", len(k8sPrompt), len(defaultAgentSoul))
	}
	if !strings.Contains(k8sPrompt, "Pantau Operator") || !strings.Contains(k8sPrompt, "kube_diff") {
		t.Fatalf("k8s prompt missing expected content")
	}
	if strings.Contains(k8sPrompt, "cf_dns_change") {
		t.Fatalf("k8s prompt should not include unrelated cloudflare skill instructions")
	}

	// Custom SOUL is preserved and augmented with the domain skill.
	custom := agentSystemPromptFor("Custom operator rules", domainDocker)
	if !strings.Contains(custom, "Custom operator rules") || !strings.Contains(custom, "docker_stack_file") {
		t.Fatalf("custom SOUL must be preserved with domain skill appended")
	}

	// Scoped memory notes for the routed domain are injected even when the question omits the domain keyword.
	srv, _ := nativeAgent(t, &fakeLLM{})
	if _, err := srv.db.AddAgentMemoryScoped(store.AgentMemoryScope{Domain: "kubernetes"}, "Ingress controller runs in ingress-nginx namespace", "operator"); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.db.AddAgentMemoryScoped(store.AgentMemoryScope{Domain: "terraform"}, "State bucket is s3://pantau-tfstate", "operator"); err != nil {
		t.Fatal(err)
	}
	mem := srv.agentMemoryPromptForDomain("why is pod failing?", domainKubernetes)
	if !strings.Contains(mem, "ingress-nginx") {
		t.Fatalf("expected kubernetes scoped memory note in prompt, got: %s", mem)
	}
	if strings.Contains(mem, "pantau-tfstate") {
		t.Fatalf("unrelated terraform memory note should not be injected for kubernetes domain")
	}
}

func TestModelTieringFallbackAndSettingsSecurity(t *testing.T) {
	// 1. Empty FastModel falls back completely to primary Reasoning Model.
	baseCfg := agentCfg{
		Provider: "anthropic",
		BaseURL:  "https://api.anthropic.com",
		Key:      "sk-ant-secret",
		Model:    "claude-sonnet-4-6",
	}
	p, u, k, m := resolveFastModelConfig(baseCfg)
	if p != "anthropic" || u != "https://api.anthropic.com" || k != "sk-ant-secret" || m != "claude-sonnet-4-6" {
		t.Fatalf("fallback mismatch: got (%q, %q, %q, %q)", p, u, k, m)
	}

	// 2. Separate FastModel (e.g. local Ollama) uses its own provider/baseURL/key without inheriting primary key.
	ollamaCfg := baseCfg
	ollamaCfg.FastProvider = "openai"
	ollamaCfg.FastBaseURL = "http://localhost:11434/v1"
	ollamaCfg.FastModel = "qwen2.5:7b"
	p, u, k, m = resolveFastModelConfig(ollamaCfg)
	if p != "openai" || u != "http://localhost:11434/v1" || k != "" || m != "qwen2.5:7b" {
		t.Fatalf("ollama tier mismatch: got (%q, %q, %q, %q)", p, u, k, m)
	}

	// 3. FastModel with empty provider/URL/key inherits primary provider/URL/key.
	sameProviderCfg := baseCfg
	sameProviderCfg.FastModel = "claude-haiku-4-5"
	p, u, k, m = resolveFastModelConfig(sameProviderCfg)
	if p != "anthropic" || u != "https://api.anthropic.com" || k != "sk-ant-secret" || m != "claude-haiku-4-5" {
		t.Fatalf("same-provider tier mismatch: got (%q, %q, %q, %q)", p, u, k, m)
	}

	// 4. Write-only API keys in agentCfg and /api/agent/settings.
	rawCfg, err := json.Marshal(agentCfg{Key: "secret-1", FastKey: "secret-2"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(rawCfg), "secret-1") || strings.Contains(string(rawCfg), "secret-2") {
		t.Fatalf("agentCfg JSON must never expose API keys: %s", string(rawCfg))
	}

	srv, _ := nativeAgent(t, &fakeLLM{})
	postBody := `{"fast_provider":"openai","fast_base_url":"http://localhost:11434/v1","fast_api_key":"sk-fast-secret","fast_model":"qwen2.5:7b"}`
	req := httptest.NewRequest(http.MethodPost, "/api/agent/settings", strings.NewReader(postBody))
	rec := httptest.NewRecorder()
	srv.handleAgentSettings(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST settings failed: %d %s", rec.Code, rec.Body.String())
	}

	// GET settings must report fast_api_key_set=true and NEVER return the secret.
	getReq := httptest.NewRequest(http.MethodGet, "/api/agent/settings", nil)
	getRec := httptest.NewRecorder()
	srv.handleAgentSettings(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("GET settings failed: %d", getRec.Code)
	}
	if strings.Contains(getRec.Body.String(), "sk-fast-secret") {
		t.Fatalf("GET settings leaked fast_api_key: %s", getRec.Body.String())
	}
	var got map[string]interface{}
	if err := json.Unmarshal(getRec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["fast_api_key_set"] != true || got["fast_model"] != "qwen2.5:7b" || got["fast_provider"] != "openai" {
		t.Fatalf("unexpected GET settings payload: %+v", got)
	}

	// Posting empty fast_api_key preserves the stored key.
	req2 := httptest.NewRequest(http.MethodPost, "/api/agent/settings", bytes.NewBufferString(`{"fast_api_key":"","fast_model":"qwen2.5:14b"}`))
	rec2 := httptest.NewRecorder()
	srv.handleAgentSettings(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("POST settings preserve key failed: %d", rec2.Code)
	}
	if srv.agentConfig().FastKey != "sk-fast-secret" {
		t.Fatalf("empty fast_api_key must preserve stored key")
	}
}

func TestFastModelUsedForCompactionAndThreadTitle(t *testing.T) {
	f := &fakeLLM{results: []llmResult{
		{Text: "Compacted summary"},
		{Text: "Nginx Service Crash Investigation"},
	}}
	srv, _ := nativeAgent(t, f)
	_ = srv.db.SetSetting("agent_fast_llm_provider", "openai")
	_ = srv.db.SetSetting("agent_fast_llm_base_url", "http://localhost:11434/v1")
	_ = srv.db.SetSetting("agent_fast_llm_model", "qwen2.5:7b")
	c := srv.agentConfig()

	history := []llmMessage{
		{Role: llmRoleUser, Text: "u1"},
		{Role: llmRoleAssistant, Text: "a1"},
		{Role: llmRoleUser, Text: "u2"},
		{Role: llmRoleAssistant, Text: "a2"},
		{Role: llmRoleUser, Text: "u3"},
	}
	out := srv.compactHistory(context.Background(), f, c, history)
	if len(out) >= len(history) {
		t.Fatalf("expected compacted history")
	}
	if f.call(0).Model != "qwen2.5:7b" {
		t.Fatalf("compactHistory must use FastModel, got %q", f.call(0).Model)
	}

	th, err := srv.db.CreateAgentThread("global", 0, "s-title", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.generateThreadTitle(context.Background(), c, th.ID, "nginx won't start on web1", "Fixed config syntax"); err != nil {
		t.Fatal(err)
	}
	if f.call(1).Model != "qwen2.5:7b" {
		t.Fatalf("generateThreadTitle must use FastModel, got %q", f.call(1).Model)
	}
	updated, err := srv.db.GetAgentThread(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Title != "Nginx Service Crash Investigation" {
		t.Fatalf("unexpected thread title: %q", updated.Title)
	}
}

func TestAnthropicToolPromptCachingHeader(t *testing.T) {
	cl := &anthropicLLM{base: "https://api.anthropic.com/v1", key: "sk-test"}
	raw, err := cl.body(llmRequest{
		Model:    "claude-sonnet-4-6",
		System:   "sys",
		Messages: []llmMessage{{Role: llmRoleUser, Text: "hi"}},
		Tools: []llmToolSpec{
			{Name: "t1", Description: "d1"},
			{Name: "t2", Description: "d2"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	tools := payload["tools"].([]interface{})
	t1 := tools[0].(map[string]interface{})
	t2 := tools[1].(map[string]interface{})
	if _, ok := t1["cache_control"]; ok {
		t.Fatalf("only the last tool should carry cache_control")
	}
	cc, ok := t2["cache_control"].(map[string]interface{})
	if !ok || cc["type"] != "ephemeral" {
		t.Fatalf("last tool must carry ephemeral cache_control, got %v", t2["cache_control"])
	}
}
