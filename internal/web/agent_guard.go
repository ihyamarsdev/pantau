package web

import "fmt"

// Limits for one user turn. A model that loops over namespaces, or repeats one read, gets a hint and then refusals,
// so it answers from the data it already has instead of sending dozens of round trips.
const (
	agentGuardHintAfterKubeGet   = 4  // the 5th kube_get of a turn carries the hint
	agentGuardHintAfterTFCatalog = 3  // the 4th tf_catalog of a turn carries the batch hint
	agentGuardRepeatLimit        = 12 // a read-only tool is refused beyond this many calls in a turn
	agentGuardTotalLimit         = 60 // every tool call is refused beyond this many calls in a turn
)

// agentTurnGuard counts the tool calls of one turn. Its zero limits refuse everything, so build it with
// newAgentTurnGuard. Tests use small limits to stay fast.
type agentTurnGuard struct {
	hintAfterKubeGet   int
	hintAfterTFCatalog int
	repeatLimit        int
	totalLimit         int
	counts             map[string]int
	total              int
}

func newAgentTurnGuard(hintAfterKubeGet, repeatLimit, totalLimit int) *agentTurnGuard {
	return &agentTurnGuard{
		hintAfterKubeGet:   hintAfterKubeGet,
		hintAfterTFCatalog: agentGuardHintAfterTFCatalog,
		repeatLimit:        repeatLimit,
		totalLimit:         totalLimit,
		counts:             map[string]int{},
	}
}

// before records one call and decides it. A non-empty refuse means the call must not run, and refuse is its result.
// A non-empty hint is appended to the result of a call that runs.
func (g *agentTurnGuard) before(name string, readOnly bool) (refuse, hint string) {
	g.total++
	g.counts[name]++
	switch {
	case g.total > g.totalLimit:
		return fmt.Sprintf("not run: this turn already made %d tool calls; answer from the results you have.", g.total-1), ""
	case readOnly && g.counts[name] > g.repeatLimit:
		return fmt.Sprintf("not run: %s was already called %d times this turn; answer from the results you have.", name, g.counts[name]-1), ""
	case name == "kube_get" && g.counts[name] > g.hintAfterKubeGet:
		return "", "\n[HINT: You have called kube_get several times this turn. Stop looping per namespace. Call kube_workload_summary, or kube_get with namespace='*' and summary_only=true, in one call.]"
	case name == "tf_catalog" && g.counts[name] > g.hintAfterTFCatalog:
		return "", "\n[HINT: Gunakan parameter batch queries/module_ids di tf_catalog dalam 1 panggilan.]"
	}
	return "", ""
}
