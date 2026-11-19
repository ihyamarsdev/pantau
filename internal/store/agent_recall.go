package store

import (
	"strings"
)

// Local RAG over SQLite for the AI agent: instead of putting the whole memory table and the whole inventory
// into the system prompt, one question is turned into a few relevant notes plus a short summary of the stored
// inventory it mentions. Everything here is a database read; nothing talks to a host.

type RecallFact struct {
	Kind  string `json:"kind"`  // host | package | container | domain
	Name  string `json:"name"`  // the matched item
	Hosts string `json:"hosts"` // the hosts that have it, comma separated, capped
	Count int    `json:"count"` // how many hosts have it (Hosts may be shorter)
}

// AgentRecall is what one question pulls out of Pantau's own storage.
type AgentRecall struct {
	Memories  []AgentMemory // notes relevant to the question, best first
	Facts     []RecallFact  // inventory matches for the words of the question
	Truncated bool          // more facts matched than were kept
}

// recallFactHosts caps how many host names one fact lists.
const recallFactHosts = 6

// RecallContext answers a question from what Pantau already stores: the notes that match it and a summary of
// the inventory items it names. The scope narrows the notes to one host, cluster or domain. It returns an
// empty result for a question that names nothing Pantau stores, so the caller can skip the section entirely.
func (d *DB) RecallContext(scope AgentMemoryScope, query string, limit int) (*AgentRecall, error) {
	out := &AgentRecall{}
	if limit <= 0 {
		limit = 12
	}
	memories, err := d.RecallAgentMemory(scope, query, limit)
	if err != nil {
		return nil, err
	}
	out.Memories = memories

	// Only words that look like an inventory item are worth a fleet query: a stopword never matches one.
	tokens := recallTokens(query)
	// One query per word, not one for the whole question: SearchFleet requires every token to match, so
	// "nginx on web-1" would match nothing while "nginx" alone names the inventory Pantau holds.
	seen := map[string]bool{}
	for _, tok := range tokens {
		res, err := d.SearchFleet(tok, FleetSearchAll, 5)
		if err != nil {
			return nil, err
		}
		for _, g := range res.Groups {
			// Host-name matches are handled below with HostNameMatches, which respects
			// separators. The fleet-search host category uses loose LIKE matching and
			// would surface near misses (macmini-40 for "macmini-4").
			if g.Kind == FleetSearchHostKind {
				continue
			}
			key := g.Kind + "\x00" + g.Name
			if seen[key] {
				continue
			}
			seen[key] = true
			names := make([]string, 0, len(g.Hosts))
			for _, h := range g.Hosts {
				if len(names) == recallFactHosts {
					break
				}
				names = append(names, h.Name)
			}
			out.Facts = append(out.Facts, RecallFact{
				Kind: g.Kind, Name: g.Name, Hosts: strings.Join(names, ", "), Count: g.HostCount,
			})
		}
		if res.Truncated {
			out.Truncated = true
		}
	}
	// Host names are not part of FleetSearch (which indexes packages/containers/domains), so a question
	// like "adakah host macmini 4" would otherwise return no fact at all. Match host names loosely and
	// surface them as facts, so the model sees the canonical spelling even before it calls list_hosts.
	// Added after inventory facts so the existing "first fact is the inventory match" contract holds.
	if hosts, err := d.ListHosts(); err == nil {
		for _, h := range hosts {
			if HostNameMatches(query, h.Name) {
				out.Facts = append(out.Facts, RecallFact{Kind: "host", Name: h.Name, Hosts: h.Name, Count: 1})
			}
		}
	}
	return out, nil
}
