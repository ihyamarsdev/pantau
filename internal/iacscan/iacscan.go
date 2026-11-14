// Package iacscan scans a directory tree for IaC and secret problems without leaving the Pantau server:
// no external binary, no network, no dependency beyond the standard library.
//
// The scanner is deliberately pragmatic: Terraform and Ansible are matched with text and brace-aware rules
// instead of a full parser, so a finding is a hint for a human, not a verdict.
package iacscan

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// DefaultMaxFileSize is the biggest file the scanner reads (1 MiB).
	DefaultMaxFileSize int64 = 1 << 20
	// DefaultMaxFindings caps the findings of one scan.
	DefaultMaxFindings = 1000
)

// Categories of a finding.
const (
	CategorySecret     = "secret"
	CategoryTerraform  = "terraform"
	CategoryAnsible    = "ansible"
	CategoryDockerfile = "dockerfile"
)

// Severities of a finding.
const (
	SeverityCritical = "critical"
	SeverityHigh     = "high"
	SeverityMedium   = "medium"
	SeverityLow      = "low"
)

// Options tunes one scan.
type Options struct {
	// MaxFileSize skips bigger files (default 1 MiB).
	MaxFileSize int64
	// MaxFindings caps the findings (default 1000).
	MaxFindings int
	// SkipNames skips files whose base name is one of them (Pantau's own generated files).
	SkipNames []string
}

func (o Options) maxFileSize() int64 {
	if o.MaxFileSize <= 0 {
		return DefaultMaxFileSize
	}
	return o.MaxFileSize
}

func (o Options) maxFindings() int {
	if o.MaxFindings <= 0 {
		return DefaultMaxFindings
	}
	return o.MaxFindings
}

// Finding is one problem found in a file.
type Finding struct {
	RuleID      string `json:"rule_id"`
	Category    string `json:"category"`
	Severity    string `json:"severity"`
	File        string `json:"file"`
	Line        int    `json:"line"`
	Title       string `json:"title"`
	Detail      string `json:"detail"`
	Remediation string `json:"remediation"`
	Snippet     string `json:"snippet"` // the source line with every secret value redacted
}

// Summary counts the findings of a scan.
type Summary struct {
	Total      int            `json:"total"`
	Critical   int            `json:"critical"`
	High       int            `json:"high"`
	Medium     int            `json:"medium"`
	Low        int            `json:"low"`
	ByCategory map[string]int `json:"by_category"`
}

// Result is the outcome of one scan.
type Result struct {
	Root         string    `json:"root"`
	ScannedAt    time.Time `json:"scanned_at"`
	Status       string    `json:"status"` // assessed | partial | not_assessed
	FilesScanned int       `json:"files_scanned"`
	Truncated    bool      `json:"truncated"`
	Summary      Summary   `json:"summary"`
	cap          int       `json:"-"`
	Findings     []Finding `json:"findings"`
	Notes        []string  `json:"notes"`
}

// Blocking reports whether the result must stop an apply or a run (critical or high).
func (r Result) Blocking() bool {
	return r.Summary.Critical > 0 || r.Summary.High > 0
}

// skipDirs are never descended into.
var skipDirs = map[string]bool{
	".git":         true,
	".terraform":   true,
	"node_modules": true,
	"vendor":       true,
}

// ScanDir walks root and reports the IaC and secret problems it finds.
// It never leaves root: symlinks are skipped, not followed.
func ScanDir(root string, opts Options) Result {
	res := Result{
		Root:      root,
		ScannedAt: time.Now().UTC(),
		Status:    "assessed",
		Findings:  []Finding{},
		Notes:     []string{},
		Summary:   Summary{ByCategory: map[string]int{}},
	}
	res.cap = opts.maxFindings()
	st, err := os.Stat(root)
	if err != nil {
		res.Status = "not_assessed"
		res.Notes = append(res.Notes, fmt.Sprintf("directory cannot be read: %v", err))
		return res
	}
	if !st.IsDir() {
		res.Status = "not_assessed"
		res.Notes = append(res.Notes, "source is not a directory")
		return res
	}

	var files []string
	var skippedSymlinks, skippedLarge, skippedBinary int
	maxSize := opts.maxFileSize()
	walkErr := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if p != root && skipDirs[d.Name()] {
				return fs.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() { // symlinks, sockets, devices: never followed
			skippedSymlinks++
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			return nil
		}
		if info.Size() > maxSize {
			skippedLarge++
			return nil
		}
		files = append(files, p)
		return nil
	})
	if walkErr != nil {
		res.Notes = append(res.Notes, fmt.Sprintf("walk stopped early: %v", walkErr))
	}

	for _, p := range files {
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil {
			continue
		}
		rel = filepath.ToSlash(rel)
		if opts.skipFile(rel) {
			continue
		}
		data, rerr := os.ReadFile(p)
		if rerr != nil {
			continue
		}
		if isBinary(data) {
			skippedBinary++
			continue
		}
		res.FilesScanned++
		scanFile(&res, rel, string(data), opts)
	}

	if skippedSymlinks > 0 {
		res.Notes = append(res.Notes, fmt.Sprintf("%d symlink(s) skipped (never followed)", skippedSymlinks))
	}
	if skippedLarge > 0 {
		res.Notes = append(res.Notes, fmt.Sprintf("%d file(s) larger than %d bytes skipped", skippedLarge, maxSize))
	}
	if skippedBinary > 0 {
		res.Notes = append(res.Notes, fmt.Sprintf("%d binary file(s) skipped", skippedBinary))
	}
	if skippedSymlinks+skippedLarge+skippedBinary > 0 {
		res.Status = "partial"
	}
	res.Summary = summarize(res.Findings)
	return res
}

func (o Options) skipFile(rel string) bool {
	base := filepath.Base(rel)
	for _, n := range o.SkipNames {
		if n != "" && strings.EqualFold(base, n) {
			return true
		}
	}
	return false
}

func isBinary(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	head := data
	if len(head) > 8000 {
		head = head[:8000]
	}
	return bytes.IndexByte(head, 0) >= 0
}

func summarize(findings []Finding) Summary {
	s := Summary{ByCategory: map[string]int{}}
	for _, f := range findings {
		s.Total++
		switch f.Severity {
		case SeverityCritical:
			s.Critical++
		case SeverityHigh:
			s.High++
		case SeverityMedium:
			s.Medium++
		case SeverityLow:
			s.Low++
		}
		s.ByCategory[f.Category]++
	}
	return s
}
