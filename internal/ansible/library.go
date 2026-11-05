package ansible

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// The library is a directory on the Pantau server shared by every playbook run:
//
//	<dir>/collections/ansible_collections/<namespace>/<name>   (ansible-galaxy collection layout)
//	<dir>/roles/<role>                                         (ansible-galaxy role layout)
//
// It is the source of truth (no database copy): list scans the disk, so items survive restarts, and it is not part
// of System Snapshots.

// LibraryCollectionsDir is the ANSIBLE_COLLECTIONS_PATH entry of the library.
func LibraryCollectionsDir(dir string) string { return filepath.Join(dir, "collections") }

// LibraryRolesDir is the ANSIBLE_ROLES_PATH entry of the library.
func LibraryRolesDir(dir string) string { return filepath.Join(dir, "roles") }

const galaxyTimeout = 10 * time.Minute

// heartbeatEvery is how often a silent install reports that it is still running.
var heartbeatEvery = 5 * time.Second

// syncWriter serializes writes from the installer's output and the heartbeat.
type syncWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (s *syncWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Write(p)
}

var (
	libMu            sync.Mutex // serializes every mutation of the library
	collectionNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_]{0,63}\.[a-z0-9][a-z0-9_]{0,63}$`)
	roleNameRe       = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)
	galaxyVersionRe  = regexp.MustCompile(`^[<>=!*A-Za-z0-9][<>=!,*A-Za-z0-9._+-]{0,63}$`)
)

type LibraryItem struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type Library struct {
	Collections []LibraryItem `json:"collections"`
	Roles       []LibraryItem `json:"roles"`
}

// GalaxyAvailable reports whether the ansible-galaxy binary (shipped with ansible-core) is in PATH.
func GalaxyAvailable() bool {
	_, err := exec.LookPath("ansible-galaxy")
	return err == nil
}

// ValidCollectionName and ValidRoleName gate every name that reaches a command line or the filesystem.
func ValidCollectionName(n string) bool { return collectionNameRe.MatchString(n) }
func ValidRoleName(n string) bool       { return roleNameRe.MatchString(n) && n != "." && n != ".." }

// ListLibrary scans dir. A missing directory is an empty library.
func ListLibrary(dir string) (Library, error) {
	lib := Library{Collections: []LibraryItem{}, Roles: []LibraryItem{}}
	root := filepath.Join(LibraryCollectionsDir(dir), "ansible_collections")
	namespaces, err := os.ReadDir(root)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return lib, err
	}
	for _, ns := range namespaces {
		names, _ := os.ReadDir(filepath.Join(root, ns.Name()))
		for _, n := range names {
			if !n.IsDir() {
				continue
			}
			var manifest struct {
				Info struct {
					Version string `json:"version"`
				} `json:"collection_info"`
			}
			if data, err := os.ReadFile(filepath.Join(root, ns.Name(), n.Name(), "MANIFEST.json")); err == nil {
				_ = json.Unmarshal(data, &manifest)
			}
			lib.Collections = append(lib.Collections, LibraryItem{ns.Name() + "." + n.Name(), manifest.Info.Version})
		}
	}
	roles, err := os.ReadDir(LibraryRolesDir(dir))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return lib, err
	}
	for _, r := range roles {
		if !r.IsDir() {
			continue
		}
		var info struct {
			Version string `yaml:"version"`
		}
		if data, err := os.ReadFile(filepath.Join(LibraryRolesDir(dir), r.Name(), "meta", ".galaxy_install_info")); err == nil {
			_ = yaml.Unmarshal(data, &info)
		}
		lib.Roles = append(lib.Roles, LibraryItem{r.Name(), info.Version})
	}
	sort.Slice(lib.Collections, func(i, j int) bool { return lib.Collections[i].Name < lib.Collections[j].Name })
	sort.Slice(lib.Roles, func(i, j int) bool { return lib.Roles[i].Name < lib.Roles[j].Name })
	return lib, nil
}

// galaxy runs ansible-galaxy in a throwaway HOME so nothing leaks into the server user's ~/.ansible or ansible.cfg.
// Its combined output is written to log as it is produced (nil discards it).
func galaxy(ctx context.Context, dir string, log io.Writer, args ...string) error {
	bin, err := exec.LookPath("ansible-galaxy")
	if err != nil {
		return errors.New("ansible-galaxy not found in PATH")
	}
	work, err := os.MkdirTemp("", "pantau-galaxy-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)
	cfg := filepath.Join(work, "ansible.cfg")
	if err := os.WriteFile(cfg, nil, 0600); err != nil {
		return err
	}
	// The library paths are set so ansible-galaxy does not warn that its install target is outside the search path.
	env := []string{"PATH=" + os.Getenv("PATH"), "LANG=C.UTF-8", "HOME=" + work, "ANSIBLE_HOME=" + work,
		"ANSIBLE_CONFIG=" + cfg, "ANSIBLE_FORCE_COLOR=0", "ANSIBLE_NOCOLOR=1",
		"ANSIBLE_COLLECTIONS_PATH=" + LibraryCollectionsDir(dir), "ANSIBLE_ROLES_PATH=" + LibraryRolesDir(dir)}
	for _, k := range []string{"HTTP_PROXY", "http_proxy", "HTTPS_PROXY", "https_proxy", "NO_PROXY", "no_proxy", "SSL_CERT_FILE", "REQUESTS_CA_BUNDLE"} {
		if v, ok := os.LookupEnv(k); ok {
			env = append(env, k+"="+v)
		}
	}
	ctx, cancel := context.WithTimeout(ctx, galaxyTimeout)
	defer cancel()
	if log == nil {
		log = io.Discard
	}
	sink := &syncWriter{w: log}
	// ansible-galaxy prints nothing while it resolves and downloads, so show what runs and that it is alive.
	fmt.Fprintf(sink, "$ ansible-galaxy %s\n", strings.Join(args, " "))
	stop, stopped := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(stopped)
		started, tick := time.Now(), time.NewTicker(heartbeatEvery)
		defer tick.Stop()
		for {
			select {
			case <-stop:
				return
			case <-tick.C:
				fmt.Fprintf(sink, "… still working (%ds)\n", int(time.Since(started).Seconds()))
			}
		}
	}()
	defer func() { close(stop); <-stopped }()
	cmd := exec.Command(bin, args...)
	cmd.Dir = work
	cmd.Env = env
	cmd.Stdout, cmd.Stderr = sink, sink
	code, err := runCmd(ctx, cmd)
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("ansible-galaxy failed (exit %d)", code)
	}
	return nil
}

func ensureLibrary(dir string) error {
	for _, d := range []string{LibraryCollectionsDir(dir), LibraryRolesDir(dir)} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	return nil
}

// InstallGalaxy downloads one collection ("ns.name") or role from Ansible Galaxy into the library, streaming
// the installer's output to log (nil to discard).
func InstallGalaxy(ctx context.Context, dir, kind, name, version string, log io.Writer) error {
	if version != "" && !galaxyVersionRe.MatchString(version) {
		return fmt.Errorf("invalid version %q", version)
	}
	libMu.Lock()
	defer libMu.Unlock()
	if err := ensureLibrary(dir); err != nil {
		return err
	}
	switch kind {
	case "collection":
		if !ValidCollectionName(name) {
			return fmt.Errorf("invalid collection name %q (expected namespace.name)", name)
		}
		spec := name
		if version != "" {
			spec += ":" + version
		}
		if err := galaxy(ctx, dir, log, "collection", "install", "--force", "-p", LibraryCollectionsDir(dir), spec); err != nil {
			return fmt.Errorf("%w (if %q is a role, choose kind Role)", err, name)
		}
		return nil
	case "role":
		if !ValidRoleName(name) {
			return fmt.Errorf("invalid role name %q", name)
		}
		spec := name
		if version != "" {
			spec += "," + version
		}
		if err := galaxy(ctx, dir, log, "role", "install", "--force", "-p", LibraryRolesDir(dir), spec); err != nil {
			return fmt.Errorf("%w (if %q is a collection, choose kind Collection)", err, name)
		}
		return nil
	}
	return fmt.Errorf("invalid kind %q (collection or role)", kind)
}

type requirementItem struct{ Name, Version string }

// parseRequirements accepts a requirements.yml ({collections: [...], roles: [...]} or a bare roles list) but keeps
// only name and version: git/URL sources are rejected so the library only ever contains Galaxy content.
func parseRequirements(content string) (collections, roles []requirementItem, err error) {
	if len(content) > 64*1024 {
		return nil, nil, errors.New("requirements exceed 64 KiB")
	}
	var doc any
	if err := yaml.Unmarshal([]byte(content), &doc); err != nil {
		return nil, nil, fmt.Errorf("invalid YAML: %w", err)
	}
	var rawCollections, rawRoles []any
	switch v := doc.(type) {
	case []any:
		rawRoles = v
	case map[string]any:
		rawCollections, _ = v["collections"].([]any)
		rawRoles, _ = v["roles"].([]any)
		for k := range v {
			if k != "collections" && k != "roles" {
				return nil, nil, fmt.Errorf("unsupported requirements key %q", k)
			}
		}
	default:
		return nil, nil, errors.New("requirements must be a list or a map with collections/roles")
	}
	item := func(raw any, collection bool) (requirementItem, error) {
		var name, version string
		switch v := raw.(type) {
		case string:
			name = v
		case map[string]any:
			for k, val := range v {
				s, _ := val.(string)
				switch k {
				case "name":
					name = s
				case "src":
					if name == "" {
						name = s
					}
				case "version":
					version = s
				case "type":
				default:
					return requirementItem{}, fmt.Errorf("unsupported requirement field %q (only name and version)", k)
				}
			}
		default:
			return requirementItem{}, errors.New("invalid requirement entry")
		}
		if collection && !ValidCollectionName(name) || !collection && !ValidRoleName(name) {
			return requirementItem{}, fmt.Errorf("invalid name %q: only Galaxy names are supported", name)
		}
		if version != "" && !galaxyVersionRe.MatchString(version) {
			return requirementItem{}, fmt.Errorf("invalid version %q", version)
		}
		return requirementItem{name, version}, nil
	}
	for _, r := range rawCollections {
		it, err := item(r, true)
		if err != nil {
			return nil, nil, err
		}
		collections = append(collections, it)
	}
	for _, r := range rawRoles {
		it, err := item(r, false)
		if err != nil {
			return nil, nil, err
		}
		roles = append(roles, it)
	}
	if len(collections)+len(roles) == 0 {
		return nil, nil, errors.New("requirements list no collections or roles")
	}
	return collections, roles, nil
}

// InstallRequirements installs every Galaxy collection and role of a requirements.yml, streaming progress to log.
func InstallRequirements(ctx context.Context, dir, content string, log io.Writer) error {
	collections, roles, err := parseRequirements(content)
	if err != nil {
		return err
	}
	libMu.Lock()
	defer libMu.Unlock()
	if err := ensureLibrary(dir); err != nil {
		return err
	}
	step := func(kind, name, spec string, args ...string) error {
		if log != nil {
			fmt.Fprintf(log, "== %s %s ==\n", kind, name)
		}
		return galaxy(ctx, dir, log, append([]string{kind, "install", "--force", "-p"}, append(args, spec)...)...)
	}
	for _, c := range collections {
		spec := c.Name
		if c.Version != "" {
			spec += ":" + c.Version
		}
		if err := step("collection", c.Name, spec, LibraryCollectionsDir(dir)); err != nil {
			return err
		}
	}
	for _, r := range roles {
		spec := r.Name
		if r.Version != "" {
			spec += "," + r.Version
		}
		if err := step("role", r.Name, spec, LibraryRolesDir(dir)); err != nil {
			return err
		}
	}
	return nil
}

// InstallLocalCollection installs a collection from a source directory (galaxy.yml at its root) or a
// collection tarball file, streaming progress to log. Dependencies are not fetched: local installs work offline.
func InstallLocalCollection(ctx context.Context, dir, src string, log io.Writer) error {
	libMu.Lock()
	defer libMu.Unlock()
	if err := ensureLibrary(dir); err != nil {
		return err
	}
	return galaxy(ctx, dir, log, "collection", "install", "--force", "--no-deps", "-p", LibraryCollectionsDir(dir), src)
}

// InstallLocalRole copies a role directory (already extracted and validated) into the library as name.
func InstallLocalRole(dir, name, src string) error {
	if !ValidRoleName(name) {
		return fmt.Errorf("invalid role name %q", name)
	}
	if !hasAnyDir(src, "tasks", "meta", "defaults", "handlers", "vars", "templates", "files") {
		return errors.New("folder does not look like a role (expected tasks/, meta/ or defaults/ at its root)")
	}
	libMu.Lock()
	defer libMu.Unlock()
	if err := ensureLibrary(dir); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(LibraryRolesDir(dir), ".incoming-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	if err := copyTree(src, stage); err != nil {
		return err
	}
	final := filepath.Join(LibraryRolesDir(dir), name)
	if err := os.RemoveAll(final); err != nil {
		return err
	}
	return os.Rename(stage, final)
}

func hasAnyDir(root string, names ...string) bool {
	for _, n := range names {
		if st, err := os.Stat(filepath.Join(root, n)); err == nil && st.IsDir() {
			return true
		}
	}
	return false
}

// copyTree copies regular files and directories only; the source is an already validated extraction.
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		switch {
		case d.IsDir():
			return os.MkdirAll(target, 0o755)
		case d.Type().IsRegular():
			info, err := d.Info()
			if err != nil {
				return err
			}
			in, err := os.Open(p)
			if err != nil {
				return err
			}
			defer in.Close()
			out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644|info.Mode().Perm()&0o100)
			if err != nil {
				return err
			}
			_, err = io.Copy(out, in)
			if cerr := out.Close(); err == nil {
				err = cerr
			}
			return err
		}
		return nil
	})
}

// RemoveLibraryItem deletes a collection ("ns.name") or role from the library.
func RemoveLibraryItem(dir, kind, name string) error {
	libMu.Lock()
	defer libMu.Unlock()
	switch kind {
	case "collections":
		if !ValidCollectionName(name) {
			return fmt.Errorf("invalid collection name %q", name)
		}
		ns, n, _ := strings.Cut(name, ".")
		root := filepath.Join(LibraryCollectionsDir(dir), "ansible_collections")
		target := filepath.Join(root, ns, n)
		if _, err := os.Stat(target); err != nil {
			return fs.ErrNotExist
		}
		if err := os.RemoveAll(target); err != nil {
			return err
		}
		infos, _ := filepath.Glob(filepath.Join(root, name+"-*.info"))
		for _, i := range infos {
			_ = os.RemoveAll(i)
		}
		_ = os.Remove(filepath.Join(root, ns)) // only when empty
		return nil
	case "roles":
		if !ValidRoleName(name) {
			return fmt.Errorf("invalid role name %q", name)
		}
		target := filepath.Join(LibraryRolesDir(dir), name)
		if _, err := os.Stat(target); err != nil {
			return fs.ErrNotExist
		}
		return os.RemoveAll(target)
	}
	return fmt.Errorf("invalid kind %q", kind)
}
