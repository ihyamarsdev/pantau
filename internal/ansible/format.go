package ansible

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// FormatPlaybook validates a playbook (a YAML list of plays, each with hosts or import_playbook) and returns it
// re-encoded with 2-space indent. Comments and quoting are preserved by node encoding. Errors name the play or
// line so the editor can point at the problem.
func FormatPlaybook(content string) (string, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(content), &doc); err != nil {
		return "", fmt.Errorf("the YAML cannot be parsed: %v", err)
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.SequenceNode || len(doc.Content[0].Content) == 0 {
		return "", fmt.Errorf("a playbook is a YAML list of plays (- hosts: …)")
	}
	if err := checkDupKeys(&doc); err != nil {
		return "", err
	}
	for i, play := range doc.Content[0].Content {
		if play.Kind != yaml.MappingNode {
			return "", fmt.Errorf("play %d is not a mapping", i+1)
		}
		hasHosts := false
		for j := 0; j+1 < len(play.Content); j += 2 {
			if k := play.Content[j].Value; k == "hosts" || k == "import_playbook" {
				hasHosts = true
				break
			}
		}
		if !hasHosts {
			return "", fmt.Errorf("play %d has neither hosts nor import_playbook", i+1)
		}
	}
	out, err := encodeNode(&doc)
	if err != nil {
		return "", err
	}
	if strings.HasPrefix(strings.TrimLeft(content, " \t"), "---") {
		out = "---\n" + out
	}
	return out + "\n", nil
}

// checkDupKeys rejects duplicate keys in any mapping; yaml.v3 keeps them in a Node without complaining.
func checkDupKeys(n *yaml.Node) error {
	switch n.Kind {
	case yaml.MappingNode:
		seen := map[string]int{}
		for j := 0; j+1 < len(n.Content); j += 2 {
			k := n.Content[j].Value
			if _, dup := seen[k]; dup {
				return fmt.Errorf("duplicate key '%s' at line %d", k, n.Content[j].Line)
			}
			seen[k] = 1
		}
	}
	for _, c := range n.Content {
		if err := checkDupKeys(c); err != nil {
			return err
		}
	}
	return nil
}
