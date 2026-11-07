package web

import (
	"encoding/json"
	"fmt"
	"strings"

	"pantau/internal/logger"
	"pantau/internal/store"
	"pantau/internal/terraform"
)

// tfEventExcerptMax bounds the log excerpt sent to notification channels.
const tfEventExcerptMax = 300

// tfIsProduction reports whether an environment label is production (case-insensitive).
func tfIsProduction(env string) bool {
	return strings.EqualFold(strings.TrimSpace(env), "production")
}

// tfRunSummary decodes the stored plan summary of a run (nil when absent).
func tfRunSummary(run *store.TFRun) *terraform.PlanSummary {
	var sum terraform.PlanSummary
	if run == nil || run.PlanSummary == "" || json.Unmarshal([]byte(run.PlanSummary), &sum) != nil {
		return nil
	}
	return &sum
}

// tfEmit sends a Terraform run event when a sink is wired and the event is enabled.
func (m *TerraformRunManager) tfEmit(ws *store.TFWorkspace, event, summary, detail string) {
	if m.events == nil || !tfNotifyEnabled(m.db, event) {
		return
	}
	m.events("Terraform "+ws.Name+" ("+ws.Environment+")", summary, detail)
}

// tfEventDetail builds "run N, counts, excerpt"; secrets are masked before truncation.
func tfEventDetail(runID int64, sum *terraform.PlanSummary, text string, secrets []string) string {
	d := fmt.Sprintf("run %d", runID)
	if sum != nil {
		d += fmt.Sprintf(", +%d ~%d -%d", sum.Create, sum.Update, sum.Delete)
		if sum.Replace > 0 {
			d += fmt.Sprintf(" -/+%d", sum.Replace)
		}
	}
	if ex := tfExcerpt(text, secrets); ex != "" {
		d += "\n" + ex
	}
	return d
}

// tfExcerpt returns the part of a log most likely to explain a failure (from the last "Error:", else the tail).
func tfExcerpt(text string, secrets []string) string {
	text = logger.RedactString(text)
	for _, s := range secrets {
		if s != "" {
			text = strings.ReplaceAll(text, s, "[REDACTED]")
		}
	}
	text = strings.TrimSpace(text)
	if i := strings.LastIndex(text, "Error:"); i >= 0 {
		text = text[i:]
	}
	if r := []rune(text); len(r) > tfEventExcerptMax {
		text = string(r[:tfEventExcerptMax]) + "..."
	}
	return text
}

// tfSecrets lists the run's secret values (cloud env, user env, sensitive variables, env files).
func tfSecrets(cfg terraform.Config) []string {
	var out []string
	for k, v := range cfg.Env {
		if !cfg.PlainEnv[k] {
			out = append(out, v)
		}
	}
	for _, v := range cfg.Vars {
		if v.Sensitive {
			out = append(out, v.Value)
		}
	}
	for _, v := range cfg.EnvFiles {
		out = append(out, v)
	}
	return out
}

// tfWantsAutoApply re-reads the workspace so a later change to production wins over the stored flag.
func (m *TerraformRunManager) tfWantsAutoApply(ws *store.TFWorkspace, run *store.TFRun, policyStatus string) bool {
	// Webhook plans always need a human: whoever holds the webhook secret must not cause unattended applies.
	if run.Type != "plan" || run.TriggerKind == "webhook" || policyStatus != "passed" {
		return false
	}
	cur, err := m.db.GetTFWorkspace(ws.ID)
	return err == nil && cur.AutoApply && !tfIsProduction(cur.Environment)
}

// tfStartAutoApply applies an awaiting_approval run; on failure it stays awaiting approval and notifies.
func (m *TerraformRunManager) tfStartAutoApply(wsID, runID int64) {
	ws, err := m.db.GetTFWorkspace(wsID)
	if err == nil {
		var run *store.TFRun
		if run, err = m.db.GetTFRun(runID); err == nil {
			err = m.StartApply(ws, run, "auto-apply")
		}
	}
	if err == nil {
		logger.Info("terraform", ws.Name, fmt.Sprintf("Auto-apply started for run %d", runID))
		return
	}
	logger.Warn("terraform", fmt.Sprintf("workspace %d", wsID), fmt.Sprintf("Auto-apply of run %d failed to start: %v", runID, err))
	if ws != nil {
		m.tfEmit(ws, "awaiting_approval", "Plan awaiting approval (auto-apply failed to start)", fmt.Sprintf("run %d", runID))
	}
}
