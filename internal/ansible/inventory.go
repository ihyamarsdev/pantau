package ansible

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

var (
	sanitizerRegex = regexp.MustCompile(`[^A-Za-z0-9_.-]`)
)

func sanitizeAlias(raw string) string {
	s := sanitizerRegex.ReplaceAllString(raw, "_")
	s = strings.Trim(s, "_.-")
	if s == "" {
		s = "host"
	}
	return s
}

func sanitizeGroupName(raw string) string {
	s := sanitizerRegex.ReplaceAllString(raw, "_")
	s = strings.Trim(s, "_.-")
	if s == "" {
		s = "group"
	}
	return s
}

type hostEntry struct {
	AnsibleHost         string `json:"ansible_host"`
	AnsiblePort         int    `json:"ansible_port,omitempty"`
	AnsibleUser         string `json:"ansible_user,omitempty"`
	AnsibleSSHKeyFile   string `json:"ansible_ssh_private_key_file,omitempty"`
	AnsiblePythonInterp string `json:"ansible_python_interpreter"`
}

type groupEntry struct {
	Hosts map[string]hostEntry `json:"hosts"`
}

type allEntry struct {
	Hosts    map[string]hostEntry  `json:"hosts,omitempty"`
	Children map[string]groupEntry `json:"children,omitempty"`
}

type inventoryWrapper struct {
	All allEntry `json:"all"`
}

// InventoryJSON membangun inventory JSON (format YAML-inventory ansible: all.hosts + all.children.<group>.hosts)
// dengan ansible_host/port/user/ansible_ssh_private_key_file=keyPaths[alias]/ansible_python_interpreter=auto_silent.
// Mengembalikan juga peta alias -> Target.ID.
func InventoryJSON(targets []Target, keyPath func(alias string) string) (inv []byte, aliases map[string]int64, err error) {
	aliases = make(map[string]int64, len(targets))
	usedAliases := make(map[string]int)

	allHosts := make(map[string]hostEntry)
	children := make(map[string]groupEntry)

	for _, t := range targets {
		baseAlias := sanitizeAlias(t.Name)
		alias := baseAlias
		count, exists := usedAliases[baseAlias]
		if exists {
			count++
			usedAliases[baseAlias] = count
			alias = fmt.Sprintf("%s_%d", baseAlias, count)
		} else {
			usedAliases[baseAlias] = 1
		}

		aliases[alias] = t.ID

		var keyFile string
		if keyPath != nil && strings.TrimSpace(t.PrivateKey) != "" {
			keyFile = keyPath(alias)
		}

		entry := hostEntry{
			AnsibleHost:         t.Address,
			AnsiblePort:         t.Port,
			AnsibleUser:         t.User,
			AnsibleSSHKeyFile:   keyFile,
			AnsiblePythonInterp: "auto_silent",
		}

		if strings.TrimSpace(t.Group) != "" {
			grpName := sanitizeGroupName(t.Group)
			grp, ok := children[grpName]
			if !ok {
				grp = groupEntry{
					Hosts: make(map[string]hostEntry),
				}
			}
			grp.Hosts[alias] = entry
			children[grpName] = grp
		} else {
			allHosts[alias] = entry
		}
	}

	payload := inventoryWrapper{
		All: allEntry{
			Hosts:    allHosts,
			Children: children,
		},
	}

	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return nil, nil, err
	}
	return data, aliases, nil
}
