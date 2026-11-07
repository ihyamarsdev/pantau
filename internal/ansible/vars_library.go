package ansible

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

const (
	extRoleMaxFiles = 400
	extRoleMaxFile  = 256 << 10
)

// externalRoleRoots mirrors the ANSIBLE_ROLES_PATH of a run (see Run): the shared Library first, then the
// system locations. ~/.ansible/roles is not listed because a run sets HOME/ANSIBLE_HOME to a private workspace.
func externalRoleRoots(libDir string) []string {
	var roots []string
	if libDir != "" {
		roots = append(roots, LibraryRolesDir(libDir))
	}
	return append(roots, "/usr/share/ansible/roles", "/etc/ansible/roles")
}

// externalRole loads a role that is not part of the project (Library, system path or a Library collection) into the
// analyzer under a virtual "library/roles/<name>" directory, so its defaults, vars, tasks and templates are read
// like a project role: variables it defines or sets itself (ansistrano_repo_dir, ansistrano_release_path, ...) must
// not be reported as inputs the operator has to supply. Returns "" when the role is not installed.
func (a *analyzer) externalRole(name string) string {
	if dir, ok := a.ext[name]; ok {
		return dir
	}
	a.ext[name] = ""
	if strings.ContainsAny(name, `/\`) {
		return ""
	}
	var src string
	for _, root := range externalRoleRoots(a.lib) {
		if st, err := os.Stat(filepath.Join(root, name)); err == nil && st.IsDir() {
			src = filepath.Join(root, name)
			break
		}
	}
	if parts := strings.Split(name, "."); src == "" && a.lib != "" && len(parts) == 3 { // namespace.collection.role
		d := filepath.Join(LibraryCollectionsDir(a.lib), "ansible_collections", parts[0], parts[1], "roles", parts[2])
		if st, err := os.Stat(d); err == nil && st.IsDir() {
			src = d
		}
	}
	if src == "" {
		return ""
	}
	dir := path.Join("library", "roles", name)
	n := 0
	for _, kind := range []string{"defaults", "vars", "tasks", "handlers", "templates", "meta"} {
		_ = filepath.WalkDir(filepath.Join(src, kind), func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.Type()&fs.ModeSymlink != 0 {
				return nil // a symlink could point outside the role
			}
			if d.IsDir() {
				return nil
			}
			ext := strings.ToLower(filepath.Ext(p))
			if ext != ".yml" && ext != ".yaml" && ext != ".j2" && ext != ".jinja2" {
				return nil
			}
			if info, err := d.Info(); err != nil || info.Size() > extRoleMaxFile || n >= extRoleMaxFiles {
				return nil
			}
			data, err := os.ReadFile(p)
			if err != nil {
				return nil
			}
			rel, _ := filepath.Rel(src, p)
			vp := path.Join(dir, filepath.ToSlash(rel))
			a.files[vp] = string(data)
			a.paths = append(a.paths, vp)
			n++
			return nil
		})
	}
	sort.Strings(a.paths) // hasDir binary-searches a.paths
	a.ext[name] = dir
	return dir
}

// roleProducers reads the task files of a role that main.yml does not reach statically (include_tasks with a
// templated name, e.g. "update-code/{{ ansistrano_deploy_via }}.yml"). They are only mined for what they produce
// (set_fact, register): their references are dropped, because which file runs depends on values we cannot see.
func (a *analyzer) roleProducers(dir string) {
	prefix := path.Join(dir, "tasks") + "/"
	saved := a.used
	a.used = map[string]*usage{}
	for _, p := range a.paths {
		if ext := strings.ToLower(path.Ext(p)); strings.HasPrefix(p, prefix) && (ext == ".yml" || ext == ".yaml") {
			a.tasks(p)
		}
	}
	a.used = saved
}
