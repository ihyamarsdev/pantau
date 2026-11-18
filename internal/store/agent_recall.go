package store

import (
	"strings"
)

// Local RAG over SQLite for the AI agent: instead of putting the whole memory table and the whole inventory
// into the system prompt, one question is turned into a few relevant notes plus a short summary of the stored
// inventory it mentions. Everything here is a database read; nothing talks to a host.

// RecallFact is one line of inventory knowledge that answers part of a question.
type RecallFact struct {
	Kind  string `json:"kind"`  // package | container | domain
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
	if len(tokens) == 0 {
		return out, nil
	}
	// One query per word, not one for the whole question: SearchFleet requires every token to match, so
	// "nginx on web-1" would match nothing while "nginx" alone names the inventory Pantau holds.
	seen := map[string]bool{}
	for _, tok := range tokens {
		res, err := d.SearchFleet(tok, FleetSearchAll, 5)
		if err != nil {
			return nil, err
		}
		for _, g := range res.Groups {
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
	return out, nil
}
