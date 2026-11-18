package inspector

// Delta-only daily infrastructure briefing. Once a day the service aggregates
// ONLY anomalies seen in the last 24 hours. With zero anomalies it writes an
// "All systems nominal" summary locally in Go without calling the LLM (0 tokens).
// With anomalies it makes ONE single-shot, tool-free call with a hard token cap.

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"pantau/internal/logger"
	"pantau/internal/store"
)

const briefingMaxTokens = 400

// BriefingService generates the delta-only daily briefing.
type BriefingService struct {
	db       *store.DB
	llm      TriageLLM
	settings func(key string) string
	now      func() time.Time
}

// NewBriefingService creates the service. llm may be nil until SetLLM is called.
func NewBriefingService(db *store.DB, llm TriageLLM) *BriefingService {
	return &BriefingService{db: db, llm: llm, now: time.Now}
}

func (b *BriefingService) SetLLM(llm TriageLLM) { b.llm = llm }

func (b *BriefingService) SetSettingsReader(f func(string) string) { b.settings = f }

// Run generates (or regenerates) the briefing for the current UTC day.
func (b *BriefingService) Run(ctx context.Context) (*store.Briefing, error) {
	if b == nil || b.db == nil {
		return nil, nil
	}
	now := time.Now().UTC()
	if b.now != nil {
		now = b.now().UTC()
	}
	day := now.Format("2006-01-02")
	since := now.Add(-24 * time.Hour)
	items := b.collectAnomalies(since, now)
	sort.Strings(items)

	brief := &store.Briefing{Day: day, Items: items, AnomalyCount: len(items)}

	if len(items) == 0 {
		// Deterministic local summary: zero tokens, zero network.
		brief.Summary = "All systems nominal: no active alerts, critical package updates, expiring TLS certificates or stale backups in the last 24 hours."
		brief.TokensUsed = 0
	} else {
		prompt := "Summarize these infrastructure anomalies from the last 24 hours in at most three short sentences for an operator. Plain text only, no markdown.\n\n" +
			strings.Join(items, "\n")
		model := ""
		if b.settings != nil {
			model = strings.TrimSpace(b.settings("agent_triage_model"))
		}
		if b.llm != nil {
			d, tokens, err := b.llm.Triage(ctx, model, "You are a terse SRE briefing assistant.", Truncate(prompt, 6000), briefingMaxTokens)
			if err == nil {
				brief.Summary = Truncate(d.RootCause, 800)
				if brief.Summary == "" {
					brief.Summary = Truncate(d.Impact, 800)
				}
				brief.TokensUsed = tokens
				if tokens > 0 {
					_ = b.db.AddTriageBudget(day, tokens)
				}
			} else {
				logger.Warn("briefing", "", "LLM briefing failed: "+err.Error())
			}
		}
		if brief.Summary == "" {
			// Local fallback keeps the briefing useful without a model.
			brief.Summary = fmt.Sprintf("%d anomaly/anomalies in the last 24 hours across the fleet.", len(items))
		}
	}

	if err := b.db.SaveBriefing(brief); err != nil {
		return nil, err
	}
	logger.Emit(slog.LevelInfo, "briefing", "", "Daily infrastructure briefing generated",
		"day", day, "anomalies", brief.AnomalyCount, "tokens", brief.TokensUsed)
	return brief, nil
}

// StartBriefingScheduler runs the briefing at the configured local time each day,
// and once shortly after startup when no briefing exists for today yet.
func (b *BriefingService) StartBriefingScheduler(ctx context.Context) {
	if b == nil {
		return
	}
	readTime := func() string {
		if b.settings == nil {
			return ""
		}
		return strings.TrimSpace(b.settings("agent_briefing_time"))
	}
	// Initial run on startup (idempotent per day: skip if today's briefing exists).
	day := time.Now().Format("2006-01-02")
	if existing, _ := b.db.GetBriefing(day); existing == nil {
		if readTime() != "" {
			go func() { _, _ = b.Run(ctx) }()
		}
	}
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	lastDay := ""
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			tm := readTime()
			if tm == "" {
				continue
			}
			now := time.Now()
			if now.Format("15:04") != tm || now.Format("2006-01-02") == lastDay {
				continue
			}
			lastDay = now.Format("2006-01-02")
			go func() { _, _ = b.Run(ctx) }()
		}
	}
}

// collectAnomalies gathers the delta-only anomaly list from four sources.
func (b *BriefingService) collectAnomalies(since, now time.Time) []string {
	var out []string
	// 1. Active alerts created in the window.
	alerts, _ := b.db.ListActiveAlerts()
	for _, a := range alerts {
		if a.CreatedAt.After(since) && a.CreatedAt.Before(now.Add(time.Minute)) {
			out = append(out, fmt.Sprintf("ALERT [%s] %s: %s", a.HostName, a.Level, Truncate(a.Message, 160)))
		}
	}
	hosts, _ := b.db.ListHosts()
	for _, h := range hosts {
		// 2. Critical package updates checked in the window.
		if pc, err := b.db.GetPackageUpdateCheck(h.ID); err == nil && pc != nil && pc.CheckedAt.After(since) {
			if pc.PendingSecurity > 0 || pc.RebootRequired {
				out = append(out, fmt.Sprintf("UPDATES %s: %d security pending, reboot=%t", h.Name, pc.PendingSecurity, pc.RebootRequired))
			}
		}
		// 3 & 4. Expiring TLS certs and stale backups from the last inspection.
		items, _ := b.db.ListActualItems(h.ID)
		for _, it := range items {
			if it.Status != "drift" || !it.UpdatedAt.After(since) {
				continue
			}
			switch it.Kind {
			case "tls":
				out = append(out, fmt.Sprintf("TLS %s %s: %s", h.Name, it.Target, Truncate(it.Details, 120)))
			case "backup":
				out = append(out, fmt.Sprintf("BACKUP %s %s: %s", h.Name, it.Target, Truncate(it.Details, 120)))
			}
		}
	}
	return out
}

// marshalBriefingItems is used by tests and the API layer to encode the items
// exactly as the store does.
func marshalBriefingItems(items []string) string {
	if items == nil {
		items = []string{}
	}
	raw, _ := json.Marshal(items)
	return string(raw)
}
