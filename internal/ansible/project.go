package ansible

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	maxProjectFiles     = 1000
	maxProjectBytes     = 4 << 20
	maxProjectFileBytes = 256 << 10
)

// ProjectFile is one text file of a playbook project, path relative to the project root.
type ProjectFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// Project is the readable part of a playbook source: Ansible code and its text dependencies. Secrets-like files
// (.env, keys, vendored collections, binaries) are never included.
type Project struct {
	Entry     string        `json:"entry"`
	Commit    string        `json:"commit,omitempty"`
	Files     []ProjectFile `json:"files"`
	Truncated bool          `json:"truncated"` // some readable files were skipped (count, size or total cap)
}

var projectTextExt = map[string]bool{
	".yml": true, ".yaml": true, ".j2": true, ".jinja2": true, ".json": true, ".ini": true, ".cfg": true,
	".conf": true, ".md": true, ".txt": true, ".sh": true, ".py": true, ".toml": true, ".service": true,
}

var projectSkipDir = map[string]bool{".git": true, ".ansible": true, "collections": true, "__pycache__": true, "node_modules": true}

func projectReadable(name string) bool {
	varsDir := false
	for _, seg := range strings.Split(path.Dir(name), "/") {
		if projectSkipDir[seg] {
			return false
		}
		varsDir = varsDir || seg == "group_vars" || seg == "host_vars"
	}
	ext := strings.ToLower(path.Ext(name))
	return projectTextExt[ext] || (varsDir && ext == "")
}

type projectCandidate struct {
	path string
	size int64
	read func() ([]byte, error)
}

// buildProject applies the caps in a deterministic order: entry first, then by path.
func buildProject(entry string, cands []projectCandidate) *Project {
	sort.Slice(cands, func(i, j int) bool {
		if (cands[i].path == entry) != (cands[j].path == entry) {
			return cands[i].path == entry
		}
		return cands[i].path < cands[j].path
	})
	p := &Project{Entry: entry}
	var total int64
	for _, c := range cands {
		if len(p.Files) >= maxProjectFiles || c.size > maxProjectFileBytes || total+c.size > maxProjectBytes {
			p.Truncated = true
			continue
		}
		data, err := c.read()
		if err != nil || !utf8.Valid(data) {
			continue
		}
		total += int64(len(data))
		p.Files = append(p.Files, ProjectFile{Path: c.path, Content: string(data)})
	}
	return p
}

// LoadGit shallow-clones g into a throwaway workspace and lists its readable files.
func LoadGit(ctx context.Context, g GitSource) (*Project, error) {
	if err := g.Validate(); err != nil {
		return nil, err
	}
	return loadGit(ctx, g, "https:ssh")
}

func loadGit(ctx context.Context, g GitSource, protocols string) (*Project, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	work, err := os.MkdirTemp("", "pantau-ansible-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temporary workspace: %w", err)
	}
	defer os.RemoveAll(work)

	var log bytes.Buffer
	repo, _, sha, err := cloneGit(ctx, g, work, protocols, &log)
	if err != nil {
		// git's own stderr (auth failure, unknown branch, ...) explains far more than the exit code.
		if tail := strings.TrimSpace(log.String()); tail != "" {
			lines := strings.Split(tail, "\n")
			err = fmt.Errorf("%w: %s", err, strings.Join(lines[max(0, len(lines)-3):], " | "))
		}
		return nil, err
	}
	var cands []projectCandidate
	err = filepath.WalkDir(repo, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(repo, p)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if projectSkipDir[d.Name()] && rel != "." {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() || !projectReadable(rel) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		cands = append(cands, projectCandidate{rel, info.Size(), func() ([]byte, error) { return os.ReadFile(p) }})
		return nil
	})
	if err != nil {
		return nil, err
	}
	proj := buildProject(g.Path, cands)
	proj.Commit = sha
	return proj, nil
}
