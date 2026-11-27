package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// planTestState is what the fake plan tools record.
type planTestState struct {
	mu          sync.Mutex
	mutations   int
	verifyFails bool
	started     chan struct{}
}

// addPlanTestTools registers the fake tools of the plan tests on the shared tool list. The tests that call it are
// not parallel, and the list is restored when they end. test_plan_mutate is classed as a write.
func addPlanTestTools(t *testing.T) *planTestState {
	t.Helper()
	st := &planTestState{started: make(chan struct{}, 4)}
	mutate := agentTool{Name: "test_plan_mutate", Description: "test", InputSchema: schema(nil, map[string]interface{}{}), gate: gateInfra,
		run: func(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
			if err := s.agentGateAs(r, 0, "Test", "test_plan_mutate", "change the test thing", gateInfra); err != nil {
				return "", err
			}
			st.mu.Lock()
			st.mutations++
			st.mu.Unlock()
			return "changed", nil
		}}
	verify := agentTool{Name: "test_plan_verify", Description: "test", InputSchema: schema(nil, map[string]interface{}{}),
		run: func(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
			st.mu.Lock()
			fail := st.verifyFails
			st.mu.Unlock()
			if fail {
				return "", errTestVerify
			}
			return "verified", nil
		}}
	inspect := agentTool{Name: "test_plan_inspect", Description: "test", InputSchema: schema(nil, map[string]interface{}{}),
		run: func(s *Server, r *http.Request, args map[string]interface{}) (string, error) { return "inspected", nil }}
	block := agentTool{Name: "test_plan_block", Description: "test", InputSchema: schema(nil, map[string]interface{}{}),
		run: func(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
			st.started <- struct{}{}
			<-r.Context().Done()
			return "", r.Context().Err()
		}}
	saved := agentTools
	t.Cleanup(func() { agentTools = saved })
	agentTools = append(append([]agentTool{}, agentTools...), mutate, verify, inspect, block)
	iacWriteTools["test_plan_mutate"] = true
	t.Cleanup(func() { delete(iacWriteTools, "test_plan_mutate") })
	return st
}

var errTestVerify = errors.New("the check failed")

// proposePlanForTest proposes a plan through the tool and returns its id.
func proposePlanForTest(t *testing.T, srv *Server, body string) string {
	t.Helper()
	out, isErr := callTool(t, srv, "agent_propose_plan", body)
	if isErr {
		t.Fatalf("propose failed: %s", out)
	}
	var v struct {
		PlanID string `json:"plan_id"`
	}
	if err := json.Unmarshal([]byte(out), &v); err != nil || v.PlanID == "" {
		t.Fatalf("no plan id in %s", out)
	}
	return v.PlanID
}

// waitPlanFinished waits for the execution that the last plan action started. The channel is read under the lock.
func waitPlanFinished(t *testing.T, srv *Server, id string) {
	t.Helper()
	srv.agent.mu.Lock()
	ch := srv.agent.plans[id].finished
	srv.agent.mu.Unlock()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("the plan did not finish")
	}
}

func planStateOf(t *testing.T, srv *Server, id string) string {
	t.Helper()
	p, ok := srv.planSnapshot(id)
	if !ok {
		t.Fatalf("plan %s is gone", id)
	}
	return p.State
}

func TestAgentPlanNextTransitions(t *testing.T) {
	t.Parallel()
	cases := []struct {
		state, action, want string
		ok                  bool
	}{
		{planProposed, "approve", planRunning, true},
		{planProposed, "cancel", planCancelled, true},
		{planProposed, "rollback", "", false},
		{planRunning, "approve", "", false},
		{planRunning, "cancel", planCancelled, true},
		{planRunning, "rollback", "", false},
		{planVerifyFailed, "rollback", planRollingBack, true},
		{planFailed, "rollback", planRollingBack, true},
		{planDone, "rollback", "", false},
		{planRolledBack, "approve", "", false},
		{planRollingBack, "cancel", planCancelled, true},
		{planCancelled, "approve", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.state+"/"+tc.action, func(t *testing.T) {
			t.Parallel()
			got, ok := agentPlanNext(tc.state, tc.action)
			if ok != tc.ok || got != tc.want {
				t.Fatalf("agentPlanNext(%s, %s) = %q, %v; want %q, %v", tc.state, tc.action, got, ok, tc.want, tc.ok)
			}
		})
	}
}

// A plan's steps are checked before the card is shown: unknown, destructive, mistyped and forbidden steps fail.
func TestAgentPlanStepRules(t *testing.T) {
	addPlanTestTools(t)
	step := func(typ, tool string) map[string]interface{} {
		return map[string]interface{}{"type": typ, "tool": tool, "description": "d"}
	}
	cases := []struct {
		name    string
		steps   []interface{}
		rolls   []interface{}
		wantErr bool
	}{
		{"inspect and verify are read only", []interface{}{step("inspect", "list_hosts"), step("verify", "test_plan_verify")}, nil, false},
		{"mutate with a write tool", []interface{}{step("mutate", "test_plan_mutate")}, nil, false},
		{"unknown tool", []interface{}{step("inspect", "no_such_tool")}, nil, true},
		{"destructive tool is refused", []interface{}{step("mutate", "exec_command")}, nil, true},
		{"mutate with a read tool", []interface{}{step("mutate", "list_hosts")}, nil, true},
		{"inspect with a write tool", []interface{}{step("inspect", "test_plan_mutate")}, nil, true},
		{"nested plan is refused", []interface{}{step("inspect", "agent_propose_plan")}, nil, true},
		{"unknown step type", []interface{}{step("apply", "list_hosts")}, nil, true},
		{"no steps", []interface{}{}, nil, true},
		{"too many steps", repeatStep(step("inspect", "list_hosts"), planMaxSteps+1), nil, true},
		{"rollback must be mutate", []interface{}{step("mutate", "test_plan_mutate")}, []interface{}{step("inspect", "list_hosts")}, true},
		{"rollback of writes", []interface{}{step("mutate", "test_plan_mutate")}, []interface{}{step("mutate", "test_plan_mutate")}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, _, cleanup := setupTestServer(t)
			t.Cleanup(cleanup)
			args := map[string]interface{}{"goal": "test", "steps": tc.steps}
			if tc.rolls != nil {
				args["rollback_plan"] = tc.rolls
			}
			raw, _ := json.Marshal(args)
			out, isErr := callTool(t, srv, "agent_propose_plan", string(raw))
			if isErr != tc.wantErr {
				t.Fatalf("error = %v, want %v: %s", isErr, tc.wantErr, out)
			}
		})
	}
}

func repeatStep(s map[string]interface{}, n int) []interface{} {
	out := make([]interface{}, n)
	for i := range out {
		out[i] = s
	}
	return out
}

// An approved plan runs its steps in order; the one approval covers the write, and no second card appears.
func TestAgentPlanExecutesWithOneApproval(t *testing.T) {
	st := addPlanTestTools(t)
	srv, _, cleanup := setupTestServer(t)
	t.Cleanup(cleanup)
	id := proposePlanForTest(t, srv, `{"goal":"change the thing","steps":[
		{"type":"inspect","tool":"test_plan_inspect","description":"look first"},
		{"type":"mutate","tool":"test_plan_mutate","description":"change it"},
		{"type":"verify","tool":"test_plan_verify","description":"check it"}]}`)
	if code, msg := srv.planAction(id, "approve"); code != http.StatusOK {
		t.Fatalf("approve: %d %s", code, msg)
	}
	waitPlanFinished(t, srv, id)

	p, _ := srv.planSnapshot(id)
	if p.State != planDone {
		t.Fatalf("state = %s, want done: %+v", p.State, p)
	}
	for _, s := range p.Steps {
		if s.Status != "done" {
			t.Fatalf("step %s = %s, want done", s.ID, s.Status)
		}
	}
	st.mu.Lock()
	muts := st.mutations
	st.mu.Unlock()
	if muts != 1 {
		t.Fatalf("the write ran %d times, want once", muts)
	}
	if n := pendingCount(srv); n != 0 {
		t.Fatalf("the plan asked the operator again (%d pending)", n)
	}
}

// A failed verify stops the plan as verify_failed, and the rollback steps run only after a second click.
func TestAgentPlanVerifyFailureThenRollback(t *testing.T) {
	st := addPlanTestTools(t)
	st.mu.Lock()
	st.verifyFails = true
	st.mu.Unlock()
	srv, _, cleanup := setupTestServer(t)
	t.Cleanup(cleanup)
	id := proposePlanForTest(t, srv, `{"goal":"change and check","steps":[
		{"type":"mutate","tool":"test_plan_mutate","description":"change it"},
		{"type":"verify","tool":"test_plan_verify","description":"check it"}],
		"rollback_plan":[{"type":"mutate","tool":"test_plan_mutate","description":"undo it"}]}`)
	srv.planAction(id, "approve")
	waitPlanFinished(t, srv, id)
	if got := planStateOf(t, srv, id); got != planVerifyFailed {
		t.Fatalf("state = %s, want verify_failed", got)
	}
	st.mu.Lock()
	before := st.mutations
	st.mu.Unlock()
	if before != 1 {
		t.Fatalf("rollback ran before the click: %d writes", before)
	}

	if code, msg := srv.planAction(id, "rollback"); code != http.StatusOK {
		t.Fatalf("rollback: %d %s", code, msg)
	}
	waitPlanFinished(t, srv, id)
	if got := planStateOf(t, srv, id); got != planRolledBack {
		t.Fatalf("state = %s, want rolled_back", got)
	}
	st.mu.Lock()
	after := st.mutations
	st.mu.Unlock()
	if after != 2 {
		t.Fatalf("the rollback write did not run: %d writes", after)
	}
}

// Stop cancels a running step: the plan ends cancelled and its execution finishes.
func TestAgentPlanCancelMidFlight(t *testing.T) {
	st := addPlanTestTools(t)
	srv, _, cleanup := setupTestServer(t)
	t.Cleanup(cleanup)
	id := proposePlanForTest(t, srv, `{"goal":"wait","steps":[{"type":"inspect","tool":"test_plan_block","description":"blocks"}]}`)
	if code, msg := srv.planAction(id, "approve"); code != http.StatusOK {
		t.Fatalf("approve: %d %s", code, msg)
	}
	select {
	case <-st.started:
	case <-time.After(5 * time.Second):
		t.Fatal("the step never started")
	}
	if code, msg := srv.planAction(id, "cancel"); code != http.StatusOK {
		t.Fatalf("cancel: %d %s", code, msg)
	}
	waitPlanFinished(t, srv, id)
	if got := planStateOf(t, srv, id); got != planCancelled {
		t.Fatalf("state = %s, want cancelled", got)
	}
}

// Actions are checked against the state: an action out of order is a conflict, and an unknown plan is not found.
func TestAgentPlanActionsAreStateChecked(t *testing.T) {
	addPlanTestTools(t)
	srv, _, cleanup := setupTestServer(t)
	t.Cleanup(cleanup)
	id := proposePlanForTest(t, srv, `{"goal":"g","steps":[{"type":"inspect","tool":"list_hosts","description":"d"}]}`)
	if code, _ := srv.planAction(id, "rollback"); code != http.StatusConflict {
		t.Fatalf("rollback of a proposed plan = %d, want conflict", code)
	}
	if code, _ := srv.planAction(id, "cancel"); code != http.StatusOK {
		t.Fatalf("cancel of a proposed plan = %d", code)
	}
	if code, _ := srv.planAction(id, "approve"); code != http.StatusConflict {
		t.Fatalf("approve after cancel = %d, want conflict", code)
	}
	if code, _ := srv.planAction("nope", "approve"); code != http.StatusNotFound {
		t.Fatalf("unknown plan = %d, want not found", code)
	}
}

// The HTTP surface: GET returns the plan, and an unknown action or method is refused.
func TestAgentPlanHTTP(t *testing.T) {
	addPlanTestTools(t)
	srv, _, cleanup := setupTestServer(t)
	t.Cleanup(cleanup)
	id := proposePlanForTest(t, srv, `{"goal":"g","steps":[{"type":"inspect","tool":"list_hosts","description":"d"}]}`)

	get := httptest.NewRecorder()
	srv.handleAgentPlans(get, httptest.NewRequest(http.MethodGet, "/api/agent/plans/"+id, nil))
	if get.Code != http.StatusOK || !strings.Contains(get.Body.String(), `"state":"proposed"`) {
		t.Fatalf("GET: %d %s", get.Code, get.Body.String())
	}
	bad := httptest.NewRecorder()
	srv.handleAgentPlans(bad, httptest.NewRequest(http.MethodPost, "/api/agent/plans/"+id+"/explode", nil))
	if bad.Code != http.StatusNotFound {
		t.Fatalf("unknown action = %d", bad.Code)
	}
	wrong := httptest.NewRecorder()
	srv.handleAgentPlans(wrong, httptest.NewRequest(http.MethodDelete, "/api/agent/plans/"+id, nil))
	if wrong.Code != http.StatusMethodNotAllowed {
		t.Fatalf("DELETE = %d", wrong.Code)
	}
}

// A step's grant covers its own tool only: another tool's gate still asks the operator.
func TestPlanGrantCoversOnlyItsTool(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	ctx := r.Context()
	r = r.WithContext(context.WithValue(ctx, planGrantKey{}, planGrant{PlanID: "p1", Tool: "tf_workspace_scaffold"}))
	if _, ok := planGrantFor(r, "tf_workspace_scaffold"); !ok {
		t.Fatal("the granted tool must pass")
	}
	if _, ok := planGrantFor(r, "tf_workspace_action"); ok {
		t.Fatal("a different tool must not pass")
	}
	if _, ok := planGrantFor(httptest.NewRequest(http.MethodPost, "/", nil), "tf_workspace_scaffold"); ok {
		t.Fatal("no grant in the context must not pass")
	}
}
