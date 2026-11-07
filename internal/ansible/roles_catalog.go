package ansible

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// RoleChoice is a role a playbook can name in `roles:`: installed in the Library, shipped by a Library
// collection (named namespace.collection.role) or present in a system role path.
type RoleChoice struct {
	Name        string `json:"name"`
	Origin      string `json:"origin"` // library | collection | system
	Collection  string `json:"collection,omitempty"`
	Version     string `json:"version,omitempty"`
	Description string `json:"description,omitempty"`
}

const maxRoleChoices = 2000

func roleDescription(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, "meta", "main.yml"))
	if err != nil {
		return ""
	}
	var meta struct {
		Info struct {
			Description string `yaml:"description"`
		} `yaml:"galaxy_info"`
	}
	if yaml.Unmarshal(data, &meta) != nil {
		return ""
	}
	d := strings.Join(strings.Fields(meta.Info.Description), " ")
	if r := []rune(d); len(r) > 200 {
		d = string(r[:200]) + "…"
	}
	return d
}

func isDir(base string, e fs.DirEntry) bool {
	if e.IsDir() {
		return true
	}
	if e.Type()&fs.ModeSymlink != 0 { // ~/.ansible/roles style installs are often links
		st, err := os.Stat(filepath.Join(base, e.Name()))
		return err == nil && st.IsDir()
	}
	return false
}

// ListRoleChoices lists the roles a run can resolve, in the order Ansible searches them (a name found earlier
// hides the same name later). libDir may be "" when no library is configured.
func ListRoleChoices(libDir string) []RoleChoice {
	out := []RoleChoice{}
	seen := map[string]bool{}
	add := func(c RoleChoice) {
		if len(out) < maxRoleChoices && !seen[c.Name] {
			seen[c.Name] = true
			out = append(out, c)
		}
	}
	roots := externalRoleRoots(libDir)
	for i, root := range roots {
		origin := "system"
		if libDir != "" && i == 0 {
			origin = "library"
		}
		entries, _ := os.ReadDir(root)
		for _, e := range entries {
			if !isDir(root, e) || !ValidRoleName(e.Name()) {
				continue
			}
			dir := filepath.Join(root, e.Name())
			c := RoleChoice{Name: e.Name(), Origin: origin, Description: roleDescription(dir)}
			if origin == "library" {
				var info struct {
					Version string `yaml:"version"`
				}
				if data, err := os.ReadFile(filepath.Join(dir, "meta", ".galaxy_install_info")); err == nil {
					_ = yaml.Unmarshal(data, &info)
				}
				c.Version = info.Version
			}
			add(c)
		}
	}
	if libDir != "" {
		base := filepath.Join(LibraryCollectionsDir(libDir), "ansible_collections")
		namespaces, _ := os.ReadDir(base)
		for _, ns := range namespaces {
			colls, _ := os.ReadDir(filepath.Join(base, ns.Name()))
			for _, coll := range colls {
				rolesDir := filepath.Join(base, ns.Name(), coll.Name(), "roles")
				roles, _ := os.ReadDir(rolesDir)
				for _, r := range roles {
					if !r.IsDir() || !ValidRoleName(r.Name()) {
						continue
					}
					cn := ns.Name() + "." + coll.Name()
					add(RoleChoice{Name: cn + "." + r.Name(), Origin: "collection", Collection: cn, Description: roleDescription(filepath.Join(rolesDir, r.Name()))})
				}
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
