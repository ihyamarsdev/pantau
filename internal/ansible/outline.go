package ansible

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// Outline is a single-play playbook cut into the parts the editor shows separately. The playbook text stays the
// source of truth: SplitPlaybook and JoinPlaybook convert between the two and keep comments and quoting of every
// node, but a playbook with several plays (or none that can be split) is Structured=false and only editable raw.
type Outline struct {
	Structured bool          `json:"structured"`
	Reason     string        `json:"reason,omitempty"`
	Play       string        `json:"play"` // every other play key: name, hosts, vars, become, gather_facts, ...
	PreTasks   string        `json:"pre_tasks"`
	Tasks      string        `json:"tasks"`
	PostTasks  string        `json:"post_tasks"`
	Handlers   string        `json:"handlers"`
	Roles      []OutlineRole `json:"roles"`
}

// OutlineRole is one entry of `roles:`. Params is a YAML mapping of everything but the role name (vars, tags, when).
type OutlineRole struct {
	Name   string `json:"name"`
	Params string `json:"params"`
}

// ownKeys are the play keys that have a part of their own; JoinPlaybook rejects them inside Play.
var ownKeys = map[string]bool{"pre_tasks": true, "roles": true, "tasks": true, "post_tasks": true, "handlers": true}

func encodeNode(n *yaml.Node) (string, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(n); err != nil {
		return "", err
	}
	_ = enc.Close()
	return strings.TrimRight(buf.String(), "\n"), nil
}

// parseKind parses text into a node of the wanted kind ("" text gives nil).
func parseKind(text string, kind yaml.Kind, what string) (*yaml.Node, error) {
	if strings.TrimSpace(text) == "" {
		return nil, nil
	}
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(text), &doc); err != nil {
		return nil, fmt.Errorf("%s: %v", what, err)
	}
	if len(doc.Content) == 0 {
		return nil, nil
	}
	n := doc.Content[0]
	if n.Kind != kind {
		if kind == yaml.SequenceNode {
			return nil, fmt.Errorf("%s must be a YAML list (- name: …)", what)
		}
		return nil, fmt.Errorf("%s must be a YAML mapping (key: value)", what)
	}
	return n, nil
}

func SplitPlaybook(content string) Outline {
	fail := func(reason string) Outline { return Outline{Reason: reason, Roles: []OutlineRole{}} }
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(content), &doc); err != nil {
		return fail("the YAML cannot be parsed: " + err.Error())
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.SequenceNode || len(doc.Content[0].Content) == 0 {
		return fail("the playbook has no play")
	}
	plays := doc.Content[0].Content
	if len(plays) != 1 {
		return fail(fmt.Sprintf("the playbook has %d plays; the parts view handles one play", len(plays)))
	}
	play := plays[0]
	if play.Kind != yaml.MappingNode {
		return fail("the play is not a mapping")
	}
	out := Outline{Structured: true, Roles: []OutlineRole{}}
	rest := &yaml.Node{Kind: yaml.MappingNode, Tag: play.Tag, Style: play.Style, HeadComment: play.HeadComment}
	for i := 0; i+1 < len(play.Content); i += 2 {
		k, v := play.Content[i], play.Content[i+1]
		var err error
		switch k.Value {
		case "pre_tasks", "tasks", "post_tasks", "handlers":
			if v.Kind != yaml.SequenceNode {
				return fail(k.Value + " is not a list")
			}
			var text string
			if text, err = encodeNode(v); err == nil {
				switch k.Value {
				case "tasks":
					out.Tasks = text
				case "pre_tasks":
					out.PreTasks = text
				case "post_tasks":
					out.PostTasks = text
				default:
					out.Handlers = text
				}
			}
		case "roles":
			if v.Kind != yaml.SequenceNode {
				return fail("roles is not a list")
			}
			for _, r := range v.Content {
				var role OutlineRole
				if role, err = splitRole(r); err != nil {
					return fail(err.Error())
				}
				out.Roles = append(out.Roles, role)
			}
		default:
			rest.Content = append(rest.Content, k, v)
		}
		if err != nil {
			return fail(err.Error())
		}
	}
	var err error
	if len(rest.Content) > 0 {
		out.Play, err = encodeNode(rest)
	}
	if err != nil {
		return fail(err.Error())
	}
	return out
}

func splitRole(r *yaml.Node) (OutlineRole, error) {
	switch r.Kind {
	case yaml.ScalarNode:
		return OutlineRole{Name: r.Value}, nil
	case yaml.MappingNode:
		rest := &yaml.Node{Kind: yaml.MappingNode, Tag: r.Tag}
		name := ""
		for i := 0; i+1 < len(r.Content); i += 2 {
			k, v := r.Content[i], r.Content[i+1]
			if name == "" && (k.Value == "role" || k.Value == "name") && v.Kind == yaml.ScalarNode {
				name = v.Value
				continue
			}
			rest.Content = append(rest.Content, k, v)
		}
		if name == "" {
			return OutlineRole{}, errors.New("a role entry has no role name")
		}
		params := ""
		if len(rest.Content) > 0 {
			var err error
			if params, err = encodeNode(rest); err != nil {
				return OutlineRole{}, err
			}
		}
		return OutlineRole{Name: name, Params: params}, nil
	}
	return OutlineRole{}, errors.New("a role entry is neither a name nor a mapping")
}

// JoinPlaybook builds the playbook text again: the other play keys first, then pre_tasks, roles, tasks, post_tasks
// and handlers (Ansible's run order).
func JoinPlaybook(o Outline) (string, error) {
	play := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	head, err := parseKind(o.Play, yaml.MappingNode, "Play settings")
	if err != nil {
		return "", err
	}
	if head != nil {
		play.HeadComment = head.HeadComment
		for i := 0; i+1 < len(head.Content); i += 2 {
			k, v := head.Content[i], head.Content[i+1]
			switch {
			case ownKeys[k.Value]:
				return "", fmt.Errorf("Play settings must not contain %q: it has its own tab", k.Value)
			default:
				play.Content = append(play.Content, k, v)
			}
		}
	}
	key := func(s string) *yaml.Node { return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s} }
	pre, err := parseKind(o.PreTasks, yaml.SequenceNode, "Pre-tasks")
	if err != nil {
		return "", err
	}
	if pre != nil {
		play.Content = append(play.Content, key("pre_tasks"), pre)
	}
	if len(o.Roles) > 0 {
		seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		for _, r := range o.Roles {
			name := strings.TrimSpace(r.Name)
			if name == "" {
				return "", errors.New("a role has no name")
			}
			params, err := parseKind(r.Params, yaml.MappingNode, "Parameters of role "+name)
			if err != nil {
				return "", err
			}
			nameNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: name}
			if params == nil || len(params.Content) == 0 {
				seq.Content = append(seq.Content, nameNode)
				continue
			}
			for i := 0; i < len(params.Content); i += 2 {
				if k := params.Content[i].Value; k == "role" || k == "name" {
					return "", fmt.Errorf("Parameters of role %s must not set %q", name, k)
				}
			}
			m := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{key("role"), nameNode}}
			m.Content = append(m.Content, params.Content...)
			seq.Content = append(seq.Content, m)
		}
		play.Content = append(play.Content, key("roles"), seq)
	}
	tasks, err := parseKind(o.Tasks, yaml.SequenceNode, "Tasks")
	if err != nil {
		return "", err
	}
	if tasks != nil {
		play.Content = append(play.Content, key("tasks"), tasks)
	}
	for _, p := range []struct{ key, label, text string }{{"post_tasks", "Post-tasks", o.PostTasks}, {"handlers", "Handlers", o.Handlers}} {
		seq, err := parseKind(p.text, yaml.SequenceNode, p.label)
		if err != nil {
			return "", err
		}
		if seq != nil {
			play.Content = append(play.Content, key(p.key), seq)
		}
	}
	if len(play.Content) == 0 {
		return "", errors.New("the play is empty")
	}
	doc := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Content: []*yaml.Node{play}}
	text, err := encodeNode(doc)
	if err != nil {
		return "", err
	}
	return text + "\n", nil
}
