package inspector

// The Inspector itself: the SSH path to a host, the inspection run, and the
// metrics, rule and container helpers it drives.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"pantau/internal/logger"
	"pantau/internal/notify"
	"pantau/internal/sshrunner"
	"pantau/internal/store"
	"pantau/internal/vpnnet"
)

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

var (
	ErrAlreadyInspecting     = errors.New("inspection already in progress for this host")
	ErrPackageScanInProgress = errors.New("package scan already in progress for this host")
)

// sensitivePorts lists the services that should never face the internet directly (databases, caches,
// admin/control planes, legacy cleartext, file sharing, remote desktop). Shared by the port exposure
// report and the Docker container audit.
var sensitivePorts = map[string]bool{
	"21": true, "23": true, "69": true, "111": true, "135": true, "139": true, "161": true,
	"389": true, "445": true, "512": true, "513": true, "514": true, "873": true,
	"1433": true, "1521": true, "2049": true, "2375": true, "2376": true, "2379": true, "2380": true,
	"3306": true, "3389": true, "5000": true, "5432": true, "5601": true, "5900": true, "5984": true,
	"6379": true, "8086": true, "9042": true, "9200": true, "9300": true, "10250": true,
	"11211": true, "15672": true, "27017": true, "50070": true,
}

type Inspector struct {
	db               *store.DB
	factory          sshrunner.RunnerFactory
	paths            PathResolver
	notifier         *notify.Dispatcher
	inFlight         sync.Map
	packageInFlight  sync.Map
	securityInFlight sync.Map
	// packageUpdateInFlight guards the Package Update Check against overlapping runs on one host.
	packageUpdateInFlight sync.Map
	drainInFlight         sync.Map
	dockerSecInFlight     sync.Map
	debouncer             *AlertDebouncer
	triage                *TriageWorker
	// sudoRetryAt remembers, per host, when passwordless sudo may be probed again after it failed, so a
	// host that requires a sudo password logs at most one failed attempt per sudoRetryAfter.
	sudoMu        sync.Mutex
	sudoRetryAt   map[int64]time.Time
	onInspectMu   sync.RWMutex
	onInspectDone func(int64)
	// teleportDir is the data directory; Teleport kubeconfigs live in its teleport/ subfolder. Set once at startup.
	teleportDir string
}

// SetOnInspectDone registers a callback invoked after a host inspection persists its status.
func (ins *Inspector) SetOnInspectDone(fn func(int64)) {
	ins.onInspectMu.Lock()
	ins.onInspectDone = fn
	ins.onInspectMu.Unlock()
}

func (ins *Inspector) notifyInspectDone(hostID int64) {
	ins.onInspectMu.RLock()
	fn := ins.onInspectDone
	ins.onInspectMu.RUnlock()
	if fn != nil {
		fn(hostID)
	}
}

func New(db *store.DB, factory sshrunner.RunnerFactory, notifier *notify.Dispatcher) *Inspector {
	if factory == nil {
		factory = sshrunner.DefaultFactory()
	}
	return &Inspector{
		db:        db,
		factory:   factory,
		notifier:  notifier,
		debouncer: NewAlertDebouncer(),
		triage:    NewTriageWorker(db, nil),
	}
}

// SetTriageWorker installs the proactive auto-triage worker and starts it.
func (ins *Inspector) SetTriageWorker(ctx context.Context, w *TriageWorker) {
	ins.triage = w
	if w != nil {
		go w.Start(ctx)
	}
}

// TriageWorker returns the inspector's triage worker, for the web layer to
// install its LLM client and settings reader.
func (ins *Inspector) TriageWorker() *TriageWorker { return ins.triage }

// PathResolver turns a Host's VPN network into a dial path; nil keeps every Host on the OS routing table.
type PathResolver interface {
	Ensure(ctx context.Context, networkID int64) (*vpnnet.Path, error)
}

func (ins *Inspector) SetPathResolver(r PathResolver) { ins.paths = r }

// netPath resolves the Host's network, tagging failures as network problems rather than a dead Host.
func (ins *Inspector) netPath(h *store.Host) (*vpnnet.Path, error) {
	if h.NetworkID == 0 || ins.paths == nil {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), vpnnet.DialBudget)
	defer cancel()
	path, err := ins.paths.Ensure(ctx, h.NetworkID)
	if err != nil {
		return nil, &networkDownError{Name: ins.networkName(h), cause: err}
	}
	return path, nil
}

// networkName resolves the VPN network's name for the machine-readable "network down: <name>"
// marker. It never fails an inspection: a missing/unreadable row falls back to the numeric ID.
func (ins *Inspector) networkName(h *store.Host) string {
	if h == nil || h.NetworkID == 0 || ins.db == nil {
		return "direct"
	}
	if n, err := ins.db.GetVPNNetwork(h.NetworkID); err == nil && n != nil && n.Name != "" {
		return n.Name
	}
	return fmt.Sprintf("id=%d", h.NetworkID)
}

// networkDownError marks a failed VPN path. It wraps ErrNetworkUnavailable so callers keep using
// errors.Is, and carries the exact network name so the "network down: <name>" marker is built from
// a field rather than parsed back out of formatted text.
type networkDownError struct {
	Name  string
	cause error
}

func (e *networkDownError) Error() string {
	return fmt.Sprintf("%v: network down: %s: %v", sshrunner.ErrNetworkUnavailable, e.Name, e.cause)
}

func (e *networkDownError) Unwrap() error { return sshrunner.ErrNetworkUnavailable }

// networkDownMarker returns the canonical "network down: <name>" marker. When err came from
// netPath it uses the typed name; otherwise it falls back to the supplied name.
func networkDownMarker(err error, fallbackName string) string {
	var nde *networkDownError
	if errors.As(err, &nde) && nde.Name != "" {
		return "network down: " + nde.Name
	}
	return "network down: " + fallbackName
}

// IsInspecting reports whether an inspection is currently in-flight for the given host.
func (ins *Inspector) IsInspecting(hostID int64) bool {
	_, loaded := ins.inFlight.Load(hostID)
	return loaded
}

// InFlightCount returns the number of hosts currently being inspected.
func (ins *Inspector) InFlightCount() int {
	count := 0
	ins.inFlight.Range(func(key, value any) bool {
		count++
		return true
	})
	return count
}

// GetRunnerForHost connects to h, through its bastion chain when it has one (ProxyJump).
func (ins *Inspector) GetRunnerForHost(h *store.Host) (sshrunner.Runner, error) {
	t, err := ins.routedTarget(h)
	if err != nil {
		return nil, err
	}
	return ins.factory(t)
}

// routedTarget is the dial target for h together with the bastions it is reached through.
func (ins *Inspector) routedTarget(h *store.Host) (sshrunner.Target, error) {
	t, err := ins.hopTarget(h)
	if err != nil {
		return t, err
	}
	chain, err := ins.db.JumpChain(h)
	if err != nil {
		return t, err
	}
	hops := make([]sshrunner.Target, 0, len(chain))
	for _, b := range chain {
		bt, err := ins.hopTarget(b)
		if err != nil {
			return t, fmt.Errorf("bastion %s: %w", b.Name, err)
		}
		hops = append(hops, bt)
	}
	t.Via = sshrunner.Chain(hops)
	return t, nil
}

// hopTarget is the dial target for one Host, without its bastions: its credentials and VPN path.
func (ins *Inspector) hopTarget(h *store.Host) (sshrunner.Target, error) {
	key, err := ins.db.ResolveHostPrivateKey(h)
	if err != nil {
		return sshrunner.Target{}, err
	}
	path, err := ins.netPath(h)
	if err != nil {
		return sshrunner.Target{}, err
	}
	return sshrunner.Target{Host: h.Host, Port: h.Port, User: h.User, Key: key, Agent: h.SSHAgent, AgentFingerprint: h.SSHAgentFingerprint, Net: path, LegacySSH: h.AllowLegacySSH}, nil
}

// InspectHost performs a full health check, metrics scrape, and drift detection on a single host.
func (ins *Inspector) InspectHost(hostID int64) error {
	return ins.InspectHostWithTimeout(hostID, 45*time.Second)
}

// InspectHostWithTimeout executes an inspection with a bounded timeout budget to prevent hanging on unreachable hosts.
func (ins *Inspector) InspectHostWithTimeout(hostID int64, timeout time.Duration) error {
	if _, loaded := ins.inFlight.LoadOrStore(hostID, struct{}{}); loaded {
		// ponytail: Concurrency guard / singleflight prevents overlapping inspections and SSH process stampedes.
		return ErrAlreadyInspecting
	}
	defer ins.inFlight.Delete(hostID)

	if timeout <= 0 {
		timeout = 45 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	startTime := time.Now()

	host, err := ins.db.GetHost(hostID)
	if err != nil {
		return err
	}
	if host == nil {
		return fmt.Errorf("host %d not found", hostID)
	}

	runner, err := ins.GetRunnerForHost(host)
	if err != nil {
		duration := time.Since(startTime).Milliseconds()
		// An unreachable Host is down whether the SSH dial or the VPN path failed; the notes name which.
		networkDown := errors.Is(err, sshrunner.ErrNetworkUnavailable)
		host.Status = "down"
		summary := "SSH connection failed"
		details := err.Error()
		if networkDown {
			// Machine-readable marker "network down: <name>" is shared with the API and the
			// /api/hosts/{id}/test toast. It makes the classification explicit so alerting can
			// tell a path failure apart from a dead Host.
			marker := networkDownMarker(err, ins.networkName(host))
			summary = "Network path unavailable: " + marker
			details = marker + ": " + err.Error()
		}
		host.LastDurationMs = duration
		host.LifecycleNotes = fmt.Sprintf("%s: %v", summary, err)
		_ = ins.db.UpdateHostInspection(host)
		_ = ins.db.RecordInspectionRun(&store.InspectionRun{
			HostID:     host.ID,
			StartedAt:  startTime,
			DurationMs: duration,
			Status:     host.Status,
			Summary:    summary,
			Details:    details,
		})
		ins.notifyInspectDone(host.ID)
		if networkDown {
			logger.Warn("inspect", host.Name, summary, "duration_ms", duration, "details", details, "status", host.Status)
		} else {
			logger.Error("inspect", host.Name, "SSH connection failed: "+err.Error(), "duration_ms", duration, "status", host.Status)
		}
		// Network-path failure is ONE infrastructure event shared by every Host behind the
		// network. It is deliberately NOT a per-host alert: no db.CreateAlert, no
		// notifier.SendAlert and no notify.SendEvent belong on this path. Any future alert
		// emitter must stay below this early-return guard, or a single VPN drop fans out into
		// one alert per Host (the exact false-positive storm section 7 suppresses).
		return err
	}
	defer runner.Close()

	// Ensure runner connection is immediately aborted if context timeout expires, unblocking any hung commands.
	doneChan := make(chan struct{})
	defer close(doneChan)
	go func() {
		select {
		case <-doneChan:
		case <-ctx.Done():
			_ = runner.Close()
		}
	}()

	// 1. Inspect System Metrics
	if err := ins.scrapeSystemMetrics(host, runner); err != nil || ctx.Err() != nil {
		if err == nil {
			err = ctx.Err()
		}
		duration := time.Since(startTime).Milliseconds()
		status := "degraded"
		summary := "Failed to scrape system metrics"
		details := err.Error()
		if ctx.Err() == context.DeadlineExceeded {
			// The SSH connection was established, so the Host is up; the inspection budget ran out.
			summary = fmt.Sprintf("Inspection timed out after %v", timeout)
			details = "Inspection cancelled: context deadline exceeded"
		}
		host.Status = status
		host.LastDurationMs = duration
		_ = ins.db.UpdateHostInspection(host)
		_ = ins.db.RecordInspectionRun(&store.InspectionRun{
			HostID:     host.ID,
			StartedAt:  startTime,
			DurationMs: duration,
			Status:     status,
			Summary:    summary,
			Details:    details,
		})
		ins.notifyInspectDone(host.ID)
		logger.Warn("inspect", host.Name, summary, "duration_ms", duration, "details", details, "status", host.Status)
		return err
	}

	// 2. Evaluate Desired State Rules & Detect Drift
	driftFound, driftSummaries, ruleCount, err := ins.evaluateDesiredRules(host, runner)
	if err != nil || ctx.Err() != nil {
		if err == nil {
			err = ctx.Err()
		}
		duration := time.Since(startTime).Milliseconds()
		status := "error"
		summary := "Error evaluating desired state rules"
		details := err.Error()
		if ctx.Err() == context.DeadlineExceeded {
			// Connected already: a blown inspection budget is a degraded run, not a dead Host.
			summary = fmt.Sprintf("Inspection timed out after %v", timeout)
			details = "Inspection cancelled: context deadline exceeded"
		}
		host.Status = "degraded"
		host.LastDurationMs = duration
		_ = ins.db.UpdateHostInspection(host)
		_ = ins.db.RecordInspectionRun(&store.InspectionRun{
			HostID:     host.ID,
			StartedAt:  startTime,
			DurationMs: duration,
			Status:     status,
			Summary:    summary,
			Details:    details,
		})
		ins.notifyInspectDone(host.ID)
		logger.Warn("inspect", host.Name, summary, "duration_ms", duration, "details", details, "status", host.Status)
		return err
	}

	// 3. Refresh the stored container listing for the fleet search. Cheap and
	// best-effort: a docker error leaves the stored rows untouched.
	if deadline, ok := ctx.Deadline(); ok {
		ins.collectHostContainers(host, runner, min(time.Until(deadline), dockerProbeTimeout))
	}

	duration := time.Since(startTime).Milliseconds()
	host.LastDurationMs = duration

	runStatus := "ok"
	summary := fmt.Sprintf("Healthy (%d rules checked)", ruleCount)
	details := ""
	if driftFound {
		host.Status = "degraded"
		runStatus = "drift"
		summary = fmt.Sprintf("Drift detected in %d rule(s)", len(driftSummaries))
		details = strings.Join(driftSummaries, "\n")
	} else {
		host.Status = "healthy"
		// ponytail: Auto-resolve/acknowledge active alerts when host recovers to fully healthy/desired state.
		_ = ins.db.AcknowledgeHostAlerts(host.ID)
	}

	_ = ins.db.RecordInspectionRun(&store.InspectionRun{
		HostID:     host.ID,
		StartedAt:  startTime,
		DurationMs: duration,
		Status:     runStatus,
		Summary:    summary,
		Details:    details,
	})

	updateErr := ins.db.UpdateHostInspection(host)
	ins.notifyInspectDone(host.ID)
	if driftFound {
		logger.Warn("inspect", host.Name, summary, "duration_ms", duration, "drift_rules", len(driftSummaries), "status", host.Status)
	} else {
		logger.Info("inspect", host.Name, summary, "duration_ms", duration, "status", host.Status)
	}
	return updateErr
}
