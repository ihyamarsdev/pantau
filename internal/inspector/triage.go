package inspector

// Token-efficient proactive auto-triage. The alert pipeline submits a compact
// job to a non-blocking worker; the worker tries, in order: a 24 h signature
// cache hit (0 tokens), a deterministic fast path (0 tokens), then ONE single-
// shot LLM call with no tools and max_tokens <= 300. Every result is persisted
// in SQLite and broadcast on the Activity Console SSE stream (ADR 0030).

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"time"

	"pantau/internal/logger"
	"pantau/internal/store"
)

const (
	triageCacheWindow   = 24 * time.Hour
	triageMaxLogChars   = 1500
	triageMaxLogLines   = 20
	triageMaxTokens     = 300
	triageDefaultBudget = 10000 // daily token cap when the setting is absent or invalid
)

// TriageJob is one alert handed to the background worker. It carries only the
// fields needed for a signature, a fast-path decision, or a compact LLM bundle.
type TriageJob struct {
	AlertID    int64
	HostID     int64
	HostName   string
	AlertType  string // desired-rule kind: container, service, disk, tls, backup, …
	Target     string // unit / container / path
	RootCause  string // Root Cause Excerpt from the inspection
	ExitStatus string // e.g. "137" or "exit 1"
	Logs       string // raw diagnostic lines; trimmed to a compact bundle
	Resources  string // top resource consumers, already formatted by Go
}

// TriageDiagnosis is the structured answer of one triage attempt.
type TriageDiagnosis struct {
	RootCause         string `json:"root_cause"`
	Impact            string `json:"impact"`
	Remediation       string `json:"remediation"`
	RemediationAction string `json:"remediation_action"` // systemd_restart | docker_restart | "" (manual)
	Source            string `json:"source"`             // fast_path | cache | llm
	TokensUsed        int    `json:"tokens_used"`
}

// TriageLLM is the single-shot model call the worker needs. web.Server implements
// it with the in-process provider client; the worker never sees tool definitions.
type TriageLLM interface {
	Triage(ctx context.Context, model, system, prompt string, maxTokens int) (TriageDiagnosis, int, error)
}

// ---------------------------------------------------------------- fast path

var (
	reOOMKilled   = regexp.MustCompile(`(?i)(oom[- ]?killed|out of memory|memory limit|killed process.*oom)`)
	reExit137     = regexp.MustCompile(`(?i)(^|\D)137(\D|$)|exit(ed)?\s*(with\s*)?(code\s*)?137`)
	reDiskFull    = regexp.MustCompile(`(?i)(no space left|disk full|filesystem.*full|disk usage.*(9[5-9]|100)\s*%)`)
	reDiskPercent = regexp.MustCompile(`(?i)(disk|filesystem|partition).{0,40}?(\d{2,3})\s*%`)
	reTLSExpired  = regexp.MustCompile(`(?i)(certificate.*expired|expired.*certificate|certificate.*(has )?expired|tls.*expired|ssl.*expired|x509: certificate)`)
	reBackupStale = regexp.MustCompile(`(?i)(stale backup|backup.*(stale|outdated|old|missing)|no recent backup|backup.*\d+\s*(day|hour)s?\s*(old|ago))`)
)

// FastPathTriage returns a structured diagnosis for obvious failures without
// calling the LLM. nil means the case is not deterministic and needs the model.
func FastPathTriage(j TriageJob) *TriageDiagnosis {
	text := j.RootCause + "\n" + j.Logs + "\n" + j.ExitStatus
	lowerTarget := strings.ToLower(j.Target)

	switch {
	case reOOMKilled.MatchString(text) || reExit137.MatchString(j.ExitStatus) || reExit137.MatchString(j.RootCause):
		action := "systemd_restart"
		if j.AlertType == "container" || lowerTarget != "" && strings.Contains(lowerTarget, "container") {
			action = "docker_restart"
		}
		return &TriageDiagnosis{
			RootCause:         "Process was OOM-killed (exit 137): the kernel killed it after the memory limit was exceeded.",
			Impact:            "The affected " + orDefault(j.AlertType, "service") + " stopped; dependent workloads may fail.",
			Remediation:       "Restart the unit and inspect its memory limit and recent memory growth before the restart recurs.",
			RemediationAction: action,
			Source:            "fast_path",
		}
	case reDiskFull.MatchString(text) || diskOver95(text):
		return &TriageDiagnosis{
			RootCause:   "Disk is full (usage above 95%): writes fail with ENOSPC.",
			Impact:      "Services cannot write logs or data; databases and uploads may be interrupted.",
			Remediation: "Free space on the affected filesystem (prune logs, old images or temporary files), then verify the rule recovers.",
			Source:      "fast_path",
		}
	case reTLSExpired.MatchString(text) || (j.AlertType == "tls" && reTLSExpired.MatchString(j.RootCause)):
		return &TriageDiagnosis{
			RootCause:   "TLS certificate has expired; clients reject the handshake.",
			Impact:      "HTTPS traffic to the affected host or domain fails until the certificate is renewed.",
			Remediation: "Renew the certificate and reload the reverse proxy, then confirm the TLS rule is healthy.",
			Source:      "fast_path",
		}
	case reBackupStale.MatchString(text) || j.AlertType == "backup":
		return &TriageDiagnosis{
			RootCause:   "Backup is stale: the expected fresh dump is missing, empty or older than the retention window.",
			Impact:      "Recent data cannot be restored; a failure now would lose the window since the last good backup.",
			Remediation: "Re-run the database backup and verify the dump exists, is non-empty and has a recent timestamp.",
			Source:      "fast_path",
		}
	}
	return nil
}

func orDefault(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return strings.TrimSpace(v)
}

// diskOver95 reports an explicit disk usage percentage at or above 95.
func diskOver95(text string) bool {
	for _, m := range reDiskPercent.FindAllStringSubmatch(text, -1) {
		if len(m) >= 3 {
			n, err := strconv.Atoi(m[2])
			if err == nil && n >= 95 {
				return true
			}
		}
	}
	return false
}

// ---------------------------------------------------------------- signature

var (
	reSigNumber = regexp.MustCompile(`\d+`)
	reSigSpace  = regexp.MustCompile(`\s+`)
	reSigPunct  = regexp.MustCompile(`[^\pL\pN]+`)
	reSigUUID   = regexp.MustCompile(`(?i)[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)
	reSigPath   = regexp.MustCompile(`(?i)(/[\w.\-]+){2,}`)
	reSigQuoted = regexp.MustCompile(`"[^"]*"|'[^']*'`)
)

// ErrorSignature builds a normalized hash of (alert_type + unit/container +
// stripped_error_line). Volatile details — numbers, paths, quoted values and
// UUIDs — are removed so the same class of failure reuses one cache entry.
func ErrorSignature(alertType, unit, errorLine string) string {
	parts := []string{alertType, unit, errorLine}
	for i, p := range parts {
		p = strings.ToLower(strings.TrimSpace(p))
		p = reSigUUID.ReplaceAllString(p, " ")
		p = reSigQuoted.ReplaceAllString(p, " ")
		p = reSigPath.ReplaceAllString(p, " ")
		p = reSigNumber.ReplaceAllString(p, " ")
		p = reSigPunct.ReplaceAllString(p, " ")
		p = reSigSpace.ReplaceAllString(strings.TrimSpace(p), " ")
		parts[i] = p
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return hex.EncodeToString(sum[:])
}

// ---------------------------------------------------------------- bundle

var reErrorLine = regexp.MustCompile(`(?i)(error|fail|fatal|panic|exception|oom|killed|refused|denied|timeout|timed out|critical|segfault)`)

// BuildDiagnosticBundle deterministically gathers the compact context for an
// unknown failure: the last 20 error-filtered log lines capped at 1500 chars,
// the exit status and the top resource consumers. No tool calls, no multi-turn.
func BuildDiagnosticBundle(logs, exitStatus, resources string) string {
	lines := strings.Split(strings.ReplaceAll(logs, "\r\n", "\n"), "\n")
	kept := make([]string, 0, triageMaxLogLines)
	for i := len(lines) - 1; i >= 0 && len(kept) < triageMaxLogLines; i-- {
		line := strings.TrimSpace(lines[i])
		if line != "" && reErrorLine.MatchString(line) {
			kept = append(kept, line)
		}
	}
	// kept is newest-first; restore chronological order for the model.
	for i, j := 0, len(kept)-1; i < j; i, j = i+1, j-1 {
		kept[i], kept[j] = kept[j], kept[i]
	}
	logText := Truncate(strings.Join(kept, "\n"), triageMaxLogChars)
	var b strings.Builder
	b.WriteString("exit_status: " + orDefault(exitStatus, "unknown") + "\n")
	b.WriteString("top_consumers: " + orDefault(resources, "unavailable") + "\n")
	b.WriteString("error_logs:\n" + logText)
	return b.String()
}

// Truncate cuts s to at most max runes, appending an ellipsis when cut.
func Truncate(s string, max int) string {
	if max <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	if max == 1 {
		return "…"
	}
	return string(r[:max-1]) + "…"
}

// ---------------------------------------------------------------- worker

type triageJob struct {
	job       TriageJob
	signature string
}

// TriageWorker is the non-blocking background triage worker. Submit never waits
// for the LLM; a full queue drops the job with a warning rather than stalling
// the inspection that produced the alert.
type TriageWorker struct {
	db       *store.DB
	llm      TriageLLM
	ctx      context.Context
	jobs     chan triageJob
	settings func(key string) string
}

// NewTriageWorker creates a worker. llm may be nil at construction and set
// later with SetLLM once the web server (which implements it) exists.
func NewTriageWorker(db *store.DB, llm TriageLLM) *TriageWorker {
	return &TriageWorker{
		db:   db,
		llm:  llm,
		jobs: make(chan triageJob, 128),
	}
}

// SetLLM installs the single-shot model client.
func (w *TriageWorker) SetLLM(llm TriageLLM) { w.llm = llm }

// SetSettingsReader installs a reader for triage settings (budget, enable flag).
func (w *TriageWorker) SetSettingsReader(f func(string) string) { w.settings = f }

// Submit hands an alert to the worker without blocking the caller.
func (w *TriageWorker) Submit(j TriageJob) {
	if w == nil {
		return
	}
	if w.settings != nil && strings.TrimSpace(w.settings("agent_triage_enabled")) == "0" {
		return
	}
	sig := ErrorSignature(j.AlertType, j.Target, firstErrorLine(j.RootCause+"\n"+j.Logs))
	select {
	case w.jobs <- triageJob{job: j, signature: sig}:
	default:
		logger.Warn("triage", j.HostName, "triage queue full; alert skipped", "alert_id", j.AlertID)
	}
}

func firstErrorLine(text string) string {
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && reErrorLine.MatchString(line) {
			return line
		}
	}
	return Truncate(strings.TrimSpace(text), 200)
}

// Start runs the worker until ctx is cancelled.
func (w *TriageWorker) Start(ctx context.Context) {
	if w == nil {
		return
	}
	w.ctx = ctx
	for {
		select {
		case <-ctx.Done():
			return
		case item := <-w.jobs:
			w.process(item)
		}
	}
}

func (w *TriageWorker) process(item triageJob) {
	if w.db == nil {
		return
	}
	// 1. Signature cache hit: identical failure triaged in the last 24 h costs 0 tokens.
	if cached, err := w.db.LatestTriageBySignature(item.signature, time.Now().Add(-triageCacheWindow)); err == nil {
		w.persistAndBroadcast(item, TriageDiagnosis{
			RootCause: cached.RootCause, Impact: cached.Impact, Remediation: cached.Remediation,
			RemediationAction: cached.RemediationAction, Source: "cache", TokensUsed: 0,
		})
		return
	}
	// 2. Deterministic fast path: 0 tokens.
	if d := FastPathTriage(item.job); d != nil {
		w.persistAndBroadcast(item, *d)
		return
	}
	// 3. Single-shot LLM with no tools and a hard token cap.
	if w.llm == nil {
		return
	}
	budget := triageDefaultBudget
	if w.settings != nil {
		if v, err := strconv.Atoi(strings.TrimSpace(w.settings("agent_triage_daily_budget"))); err == nil && v >= 0 {
			budget = v
		}
	}
	day := time.Now().UTC().Format("2006-01-02")
	spent, _ := w.db.TriageBudgetDay(day)
	if spent >= budget {
		logger.Warn("triage", item.job.HostName, "daily triage budget reached; alert left undiagnosed", "spent", spent, "cap", budget)
		return
	}
	model := ""
	if w.settings != nil {
		model = strings.TrimSpace(w.settings("agent_triage_model"))
	}
	prompt := "Diagnose this infrastructure failure. Reply with ONLY a compact JSON object with keys root_cause, impact, remediation (one short sentence each) and remediation_action (one of systemd_restart, docker_restart, or empty string). No prose, no markdown.\n\n" +
		"alert_type: " + item.job.AlertType + "\nunit: " + item.job.Target + "\n" +
		BuildDiagnosticBundle(item.job.Logs, item.job.ExitStatus, item.job.Resources)
	ctx := w.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	d, tokens, err := w.llm.Triage(ctx, model, "You are a terse SRE triage assistant.", Truncate(prompt, 6000), triageMaxTokens)
	if err != nil {
		logger.Warn("triage", item.job.HostName, "LLM triage failed: "+err.Error())
		return
	}
	if tokens > 0 {
		_ = w.db.AddTriageBudget(day, tokens)
	}
	d.Source = "llm"
	d.TokensUsed = tokens
	w.persistAndBroadcast(item, d)
}

func (w *TriageWorker) persistAndBroadcast(item triageJob, d TriageDiagnosis) {
	result := &store.TriageResult{
		Signature:         item.signature,
		AlertID:           item.job.AlertID,
		HostID:            item.job.HostID,
		Unit:              item.job.Target,
		Source:            d.Source,
		RootCause:         Truncate(d.RootCause, 500),
		Impact:            Truncate(d.Impact, 500),
		Remediation:       Truncate(d.Remediation, 500),
		RemediationAction: d.RemediationAction,
		TokensUsed:        d.TokensUsed,
	}
	if _, err := w.db.SaveTriageResult(result); err != nil {
		logger.Warn("triage", item.job.HostName, "could not persist triage: "+err.Error())
		return
	}
	logger.Emit(slog.LevelInfo, "triage", item.job.HostName,
		"AI auto-diagnosis ready ("+d.Source+")",
		"alert_id", item.job.AlertID, "tokens", d.TokensUsed, "root_cause", result.RootCause)
}
