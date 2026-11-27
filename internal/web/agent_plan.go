package web

// Plan-and-execute for the in-process agent. The agent proposes a structured plan of inspect, mutate and verify
// steps. The operator approves the whole plan once on its card, and the steps then run in order. A plan never
// carries a destructive step, so those still ask one at a time. A verify failure offers the rollback plan,
// which runs only after the operator clicks again. Nothing here applies to plans kept after a restart: they
// live in memory, like the pending approvals.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"pantau/internal/logger"
)

// Plan states. A plan moves only through agentPlanNext.
const (
	planProposed       = "proposed"
	planRunning        = "running"
	planDone           = "done"
	planFailed         = "failed"
	planVerifyFailed   = "verify_failed"
	planRollingBack    = "rolling_back"
	planRolledBack     = "rolled_back"
	planRollbackFailed = "rollback_failed"
	planCancelled      = "cancelled"
)

const (
	planMaxSteps    = 12 // steps of one plan, and of its rollback plan
	planMaxRetained = 50 // plans kept in memory; finished ones are dropped first
)

// planForbiddenTools cannot be a step: a plan must not nest another plan or investigation, or change the domain.
var planForbiddenTools = map[string]bool{"agent_propose_plan": true, "delegate_investigate": true, "set_tool_domain": true}

// agentPlanNext returns the state an action leads to, and false when the action is not allowed in that state.
func agentPlanNext(state, action string) (string, bool) {
	switch {
	case state == planProposed && action == "approve":
		return planRunning, true
	case state == planProposed && action == "cancel":
		return planCancelled, true
	case (state == planRunning || state == planRollingBack) && action == "cancel":
		return planCancelled, true
	case (state == planVerifyFailed || state == planFailed) && action == "rollback":
		return planRollingBack, true
	}
	return "", false
}

type agentPlanStep struct {
	ID          string                 `json:"id"`
	Type        string                 `json:"type"` // inspect | mutate | verify (rollback steps are mutate)
	Tool        string                 `json:"tool"`
	Target      string                 `json:"target,omitempty"`
	Description string                 `json:"description"`
	Args        map[string]interface{} `json:"args"`
	Status      string                 `json:"status"` // pending | running | done | failed | skipped
	Result      string                 `json:"result,omitempty"`
}

// agentPlan is one plan. The fields after finished are only touched under the agentState mutex or by the one
// goroutine that executes the plan.
type agentPlan struct {
	ID       string          `json:"id"`
	Goal     string          `json:"goal"`
	State    string          `json:"state"`
	Steps    []agentPlanStep `json:"steps"`
	Rollback []agentPlanStep `json:"rollback_plan"`
	Message  string          `json:"message,omitempty"`
	created  time.Time
	cancel   context.CancelFunc
	finished chan struct{} // closed when an execution ends
}

// planGrant travels with the context of one plan step. It lets agentGateAs pass the single tool the operator
// approved with the plan card, instead of asking again for every change in the plan.
type planGrant struct {
	PlanID string
	Tool   string
}

type planGrantKey struct{}

func planGrantFor(r *http.Request, tool string) (planGrant, bool) {
	g, ok := r.Context().Value(planGrantKey{}).(planGrant)
	return g, ok && g.Tool == tool
}

// parsePlanSteps reads the steps of a proposal and refuses every step the plan rules forbid. Steps are checked
// here, before the operator sees the card, so a card never offers something that cannot run.
func parsePlanSteps(raw interface{}, field string, rollback bool) ([]agentPlanStep, error) {
	list, _ := raw.([]interface{})
	if len(list) > planMaxSteps || (!rollback && len(list) == 0) {
		return nil, fmt.Errorf("%s needs 1 to %d steps", field, planMaxSteps)
	}
	prefix := "s"
	if rollback {
		prefix = "r"
	}
	out := make([]agentPlanStep, 0, len(list))
	for i, item := range list {
		obj, _ := item.(map[string]interface{})
		if obj == nil {
			return nil, fmt.Errorf("%s[%d] must be an object", field, i)
		}
		st := agentPlanStep{
			ID: fmt.Sprintf("%s%d", prefix, i+1), Type: argStr(obj, "type"), Tool: argStr(obj, "tool"),
			Target: clipTo(argStr(obj, "target"), 120), Description: clipTo(argStr(obj, "description"), 300),
			Status: "pending",
		}
		if st.Description == "" || st.Tool == "" {
			return nil, fmt.Errorf("%s[%d] needs a tool and a description", field, i)
		}
		args, _ := obj["args"].(map[string]interface{})
		if args == nil {
			args = map[string]interface{}{}
		}
		st.Args = args
		if rollback && st.Type != "mutate" {
			return nil, fmt.Errorf("%s[%d]: a rollback step must be type mutate", field, i)
		}
		if !rollback && st.Type != "inspect" && st.Type != "mutate" && st.Type != "verify" {
			return nil, fmt.Errorf("%s[%d]: type must be inspect, mutate or verify", field, i)
		}
		if planForbiddenTools[st.Tool] {
			return nil, fmt.Errorf("%s[%d]: %s cannot be a plan step", field, i, st.Tool)
		}
		tool := findAgentTool(st.Tool)
		if tool == nil {
			return nil, fmt.Errorf("%s[%d]: unknown tool %q", field, i, st.Tool)
		}
		if err := checkArgs(tool.InputSchema, args); err != nil {
			return nil, fmt.Errorf("%s[%d]: %v", field, i, err)
		}
		tier := riskTierOfTool(st.Tool, args)
		switch {
		case tier == riskDestructive:
			return nil, fmt.Errorf("%s[%d]: %s is destructive and cannot be in a plan; ask for it on its own so it asks each time", field, i, st.Tool)
		case st.Type == "mutate" && tier == riskReadOnly:
			return nil, fmt.Errorf("%s[%d]: %s does not change anything, use type inspect or verify", field, i, st.Tool)
		case st.Type != "mutate" && tier != riskReadOnly:
			return nil, fmt.Errorf("%s[%d]: %s changes state, use type mutate", field, i, st.Tool)
		}
		out = append(out, st)
	}
	return out, nil
}

func init() { agentTools = append(agentTools, planTools...) }

var planTools = []agentTool{
	{Name: "agent_propose_plan", Description: "Propose a multi-step plan for a task that changes things (scaffolding, configuration changes, chained remediation). The operator approves the whole plan once on its card; nothing runs before that. goal is one sentence. steps run in order; each step is {type, tool, args, target, description}: type inspect (read only), mutate (changes something; the approved plan covers it, nothing else), or verify (read only, after a change; if it fails the rollback offer appears). rollback_plan is the mutate steps that undo the change, run only if the operator clicks Run Rollback. Destructive tools (exec_command, destroy, apply, file deletion) are refused in a plan: ask for them one at a time.",
		InputSchema: schema([]string{"goal", "steps"}, map[string]interface{}{
			"goal":          str,
			"steps":         map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "object"}},
			"rollback_plan": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "object"}},
		}), run: toolProposePlan},
}

// toolProposePlan stores a plan for the operator to approve. It changes nothing by itself.
func toolProposePlan(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	goal := clipTo(argStr(args, "goal"), 300)
	if goal == "" {
		return "", fmt.Errorf("goal is required")
	}
	steps, err := parsePlanSteps(args["steps"], "steps", false)
	if err != nil {
		return "", err
	}
	rollback, err := parsePlanSteps(args["rollback_plan"], "rollback_plan", true)
	if err != nil {
		return "", err
	}
	p := &agentPlan{ID: randHex(8), Goal: goal, State: planProposed, Steps: steps, Rollback: rollback, created: time.Now()}
	s.agent.mu.Lock()
	s.storePlanLocked(p)
	s.agent.mu.Unlock()
	return jsonText(map[string]interface{}{
		"plan_id": p.ID, "state": planProposed, "steps": len(steps), "rollback_steps": len(rollback),
		"note": "The operator approves the plan on its card. Do not run these steps yourself; report the plan and wait.",
	})
}

// storePlanLocked keeps a plan and drops the oldest finished ones beyond the cap. A running plan is never dropped.
func (s *Server) storePlanLocked(p *agentPlan) {
	if s.agent.plans == nil {
		s.agent.plans = map[string]*agentPlan{}
	}
	s.agent.plans[p.ID] = p
	if len(s.agent.plans) <= planMaxRetained {
		return
	}
	var old []*agentPlan
	for _, q := range s.agent.plans {
		if q.State != planRunning && q.State != planRollingBack {
			old = append(old, q)
		}
	}
	sort.Slice(old, func(i, j int) bool { return old[i].created.Before(old[j].created) })
	for _, q := range old {
		if len(s.agent.plans) <= planMaxRetained {
			break
		}
		delete(s.agent.plans, q.ID)
	}
}

// planSnapshot is a copy that is safe to encode while the plan keeps running.
func (s *Server) planSnapshot(id string) (agentPlan, bool) {
	s.agent.mu.Lock()
	defer s.agent.mu.Unlock()
	p, ok := s.agent.plans[id]
	if !ok {
		return agentPlan{}, false
	}
	c := agentPlan{ID: p.ID, Goal: p.Goal, State: p.State, Message: p.Message,
		Steps: append([]agentPlanStep{}, p.Steps...), Rollback: append([]agentPlanStep{}, p.Rollback...)}
	return c, true
}

// setPlanState records a plan's state under the lock, so a poll never sees a torn plan.
func (s *Server) setPlanState(p *agentPlan, state, msg string) {
	s.agent.mu.Lock()
	defer s.agent.mu.Unlock()
	p.State = state
	p.Message = msg
}

// executePlan runs the steps of one phase in order. A failed inspect or mutate step ends the plan as failed; a
// failed verify step ends it as verify_failed, which offers the rollback plan. The operator's cancel stops it
// before the next step, and the state is then cancelled.
func (s *Server) executePlan(ctx context.Context, p *agentPlan, rollbackPhase bool) {
	s.agent.mu.Lock()
	steps := p.Steps
	if rollbackPhase {
		steps = p.Rollback
	}
	finished := p.finished
	s.agent.mu.Unlock()
	defer close(finished)

	for i := range steps {
		if ctx.Err() != nil {
			s.setPlanState(p, planCancelled, "stopped by the operator before step "+steps[i].ID)
			return
		}
		s.markStep(p, steps, i, "running", "")
		text, isErr := s.runPlanStep(ctx, p.ID, steps[i])
		if ctx.Err() != nil {
			s.markStep(p, steps, i, "failed", "cancelled while running: "+text)
			s.setPlanState(p, planCancelled, "stopped by the operator during step "+steps[i].ID)
			return
		}
		if isErr {
			s.markStep(p, steps, i, "failed", text)
			switch {
			case rollbackPhase:
				s.setPlanState(p, planRollbackFailed, fmt.Sprintf("rollback step %s failed: %s", steps[i].ID, clipTo(text, 200)))
			case steps[i].Type == "verify":
				s.setPlanState(p, planVerifyFailed, fmt.Sprintf("verification %s failed: %s", steps[i].ID, clipTo(text, 200)))
			default:
				s.setPlanState(p, planFailed, fmt.Sprintf("step %s failed: %s", steps[i].ID, clipTo(text, 200)))
			}
			return
		}
		s.markStep(p, steps, i, "done", text)
	}
	if rollbackPhase {
		s.setPlanState(p, planRolledBack, "rollback finished")
	} else {
		s.setPlanState(p, planDone, "all steps finished")
	}
}

func (s *Server) markStep(p *agentPlan, steps []agentPlanStep, i int, status, result string) {
	s.agent.mu.Lock()
	defer s.agent.mu.Unlock()
	steps[i].Status = status
	steps[i].Result = clipTo(result, 600)
}

// runPlanStep calls the step's tool with the plan's grant in its context, so the one approved change passes its
// gate and every other change still asks.
func (s *Server) runPlanStep(ctx context.Context, planID string, st agentPlanStep) (string, bool) {
	gctx := context.WithValue(ctx, planGrantKey{}, planGrant{PlanID: planID, Tool: st.Tool})
	r, err := http.NewRequestWithContext(gctx, http.MethodPost, "/api/agent/plans", nil)
	if err != nil {
		return err.Error(), true
	}
	logger.Info("agent", st.Tool, "plan step started", "plan", planID, "step", st.ID, "type", st.Type)
	return s.callAgentTool(r, st.Tool, st.Args)
}

// handleAgentPlans serves /api/agent/plans/{id}: GET the plan; POST {id}/approve, {id}/cancel, {id}/rollback.
func (s *Server) handleAgentPlans(w http.ResponseWriter, r *http.Request) {
	id, action, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/api/agent/plans/"), "/")
	if id == "" {
		http.NotFound(w, r)
		return
	}
	if r.Method == http.MethodGet && action == "" {
		p, ok := s.planSnapshot(id)
		if !ok {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, http.StatusOK, p)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(r.Body, 1024))
	switch action {
	case "approve", "cancel", "rollback":
	default:
		http.NotFound(w, r)
		return
	}
	code, msg := s.planAction(id, action)
	if code != http.StatusOK {
		writeJSON(w, code, map[string]string{"error": msg})
		return
	}
	snap, _ := s.planSnapshot(id)
	writeJSON(w, http.StatusAccepted, snap)
}

// planAction applies one operator action. The execution of approve and rollback runs in its own goroutine.
func (s *Server) planAction(id, action string) (int, string) {
	s.agent.mu.Lock()
	p, ok := s.agent.plans[id]
	if !ok {
		s.agent.mu.Unlock()
		return http.StatusNotFound, "no such plan"
	}
	next, allowed := agentPlanNext(p.State, action)
	if !allowed {
		state := p.State
		s.agent.mu.Unlock()
		return http.StatusConflict, fmt.Sprintf("the plan is %s: %s is not possible", state, action)
	}
	if action == "rollback" && len(p.Rollback) == 0 {
		s.agent.mu.Unlock()
		return http.StatusConflict, "this plan has no rollback steps"
	}
	p.State = next
	if action == "cancel" {
		if p.cancel != nil {
			p.cancel()
		}
		p.Message = "stopped by the operator"
		s.agent.mu.Unlock()
		return http.StatusOK, ""
	}
	ctx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel
	p.finished = make(chan struct{})
	s.agent.mu.Unlock()
	go s.executePlan(ctx, p, action == "rollback")
	return http.StatusOK, ""
}

// planIDFromResult returns the plan id of a successful agent_propose_plan result, so the chat can show its card.
func planIDFromResult(name string, out agentOutcome) string {
	if name != "agent_propose_plan" || out.isErr {
		return ""
	}
	var v struct {
		PlanID string `json:"plan_id"`
	}
	if err := json.Unmarshal([]byte(out.text), &v); err != nil {
		return ""
	}
	return v.PlanID
}
