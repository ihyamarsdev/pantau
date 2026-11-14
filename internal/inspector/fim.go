package inspector

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"pantau/internal/sshrunner"
	"pantau/internal/store"
)

const (
	// FIMMaxPaths caps how many files one file_integrity rule may track.
	FIMMaxPaths = 20
	// FIMMaxManifestEntries caps how many manifest entries a baseline may hold.
	FIMMaxManifestEntries = 500
	// FIMMaxChangedFiles caps how many changed paths the drift report names.
	FIMMaxChangedFiles = 20
	// fimTimeout bounds the manifest command (find over / can be slow).
	fimTimeout = 90 * time.Second
)

// FIM preset targets.
const (
	FIMPresetAuth = "preset:auth"
	FIMTargetSUID = "suid"
)

// FIMFile is one tracked file and its sha256 digest.
type FIMFile struct {
	Path string `json:"path"`
	Hash string `json:"hash"`
}

// FIMBaseline is the accepted state of a file_integrity rule.
type FIMBaseline struct {
	Digest string    `json:"digest"` // sha256 over the canonical manifest
	Files  []FIMFile `json:"files"`  // sorted by path
}

// FIMDiff lists the difference between a baseline and the current host state.
type FIMDiff struct {
	Added    []FIMFile `json:"added"`
	Removed  []FIMFile `json:"removed"`
	Modified []FIMFile `json:"modified"` // carries the current hash
}

// FIMCapture is the raw result of one manifest run.
type FIMCapture struct {
	Files      map[string]string // path -> sha256
	Missing    []string
	Denied     []string
	NoHashTool bool
	SudoDenied bool
	TimedOut   bool
}

// fimPresetAuthPaths / fimPresetAuthGlobs define the preset:auth target set.
var (
	fimPresetAuthPaths = []string{
		"/etc/passwd",
		"/etc/group",
		"/etc/sudoers",
		"/etc/ssh/sshd_config",
		"/root/.ssh/authorized_keys",
	}
	fimPresetAuthGlobs = []string{
		"/etc/sudoers.d/*",
		"/etc/ssh/sshd_config.d/*",
	}
)

// ValidateFIMTarget validates the target of a file_integrity rule.
// Accepted forms: the preset "preset:auth", the special target "suid", or a list
// of up to FIMMaxPaths absolute file paths separated by comma or newline.
func ValidateFIMTarget(target string) error {
	t := strings.TrimSpace(target)
	if t == "" {
		return fmt.Errorf("target is required")
	}
	if t == FIMTargetSUID {
		return nil
	}
	if strings.HasPrefix(t, "preset:") {
		if t != FIMPresetAuth {
			return fmt.Errorf("unknown preset %q (supported: %s)", t, FIMPresetAuth)
		}
		return nil
	}
	paths := fimSplitPaths(t)
	if len(paths) == 0 {
		return fmt.Errorf("no file path given")
	}
	if len(paths) > FIMMaxPaths {
		return fmt.Errorf("too many paths: %d (max %d)", len(paths), FIMMaxPaths)
	}
	for _, p := range paths {
		if err := validateFIMPath(p); err != nil {
			return err
		}
	}
	return nil
}

// validateFIMPath accepts only absolute, literal paths (no globs, no traversal).
func validateFIMPath(p string) error {
	if p == "" {
		return fmt.Errorf("empty path in target")
	}
	if !strings.HasPrefix(p, "/") {
		return fmt.Errorf("path %q must be absolute", p)
	}
	if strings.ContainsAny(p, "\x00\n\r") {
		return fmt.Errorf("path %q contains a control character", p)
	}
	// The manifest encoding uses ',' and '=' as separators, so they cannot appear in a path.
	if strings.ContainsAny(p, ",=") {
		return fmt.Errorf("path %q may not contain ',' or '='", p)
	}
	// Shell metacharacters are rejected outright; every value is quoted with shellQuote anyway.
	if strings.ContainsAny(p, ";&|`$()<>\"'*?[]{}!#~\\") {
		return fmt.Errorf("path %q contains a forbidden character", p)
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return fmt.Errorf("path %q may not contain '..'", p)
		}
	}
	return nil
}

// fimSplitPaths splits a target into its path list (comma or newline separated).
func fimSplitPaths(target string) []string {
	fields := strings.FieldsFunc(target, func(r rune) bool { return r == ',' || r == '\n' })
	var out []string
	for _, f := range fields {
		f = strings.TrimSpace(f)
		if f != "" {
			out = append(out, f)
		}
	}
	return out
}

// EncodeFIMBaseline encodes a manifest into the rule `expected` value:
// "sha256:<digest>|m:<path>=<hash>,<path>=<hash>...". The digest covers the canonical
// (sorted, newline separated) manifest, so a tampered manifest is detected.
func EncodeFIMBaseline(files []FIMFile) (string, error) {
	if len(files) == 0 {
		return "", fmt.Errorf("manifest is empty")
	}
	if len(files) > FIMMaxManifestEntries {
		return "", fmt.Errorf("manifest too large: %d entries (max %d)", len(files), FIMMaxManifestEntries)
	}
	sorted := append([]FIMFile(nil), files...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Path < sorted[j].Path })
	var canon strings.Builder
	var entries []string
	for _, f := range sorted {
		if f.Path == "" || f.Hash == "" {
			return "", fmt.Errorf("manifest entry is incomplete")
		}
		// The manifest encoding uses ',' and '=' as separators and one path per line,
		// so a path holding them (or a control character) cannot be encoded unambiguously.
		if strings.ContainsAny(f.Path, ",=\n\r\x00") {
			return "", fmt.Errorf("path %q cannot be encoded in a manifest", f.Path)
		}
		canon.WriteString(f.Path)
		canon.WriteString("=")
		canon.WriteString(f.Hash)
		canon.WriteString("\n")
		entries = append(entries, f.Path+"="+f.Hash)
	}
	sum := sha256.Sum256([]byte(canon.String()))
	return "sha256:" + hex.EncodeToString(sum[:]) + "|m:" + strings.Join(entries, ","), nil
}

// ParseFIMExpected parses the `expected` value of a file_integrity rule and verifies
// that the embedded manifest matches its digest.
func ParseFIMExpected(expected string) (*FIMBaseline, error) {
	e := strings.TrimSpace(expected)
	if e == "" {
		return nil, fmt.Errorf("expected is empty: capture a baseline first")
	}
	if strings.EqualFold(e, "baseline") {
		return nil, fmt.Errorf("expected 'baseline' is not accepted: capture the baseline explicitly first")
	}
	digestPart, manifestPart, ok := strings.Cut(e, "|m:")
	if !ok {
		return nil, fmt.Errorf("invalid expected: want sha256:<digest>|m:<manifest>")
	}
	if !strings.HasPrefix(digestPart, "sha256:") {
		return nil, fmt.Errorf("invalid expected: missing sha256 digest")
	}
	declared := strings.TrimPrefix(digestPart, "sha256:")
	if len(declared) != 64 {
		return nil, fmt.Errorf("invalid expected: malformed sha256 digest")
	}
	var files []FIMFile
	for _, entry := range strings.Split(manifestPart, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		path, hash, ok := strings.Cut(entry, "=")
		if !ok || path == "" || len(hash) != 64 {
			return nil, fmt.Errorf("invalid expected: malformed manifest entry %q", entry)
		}
		files = append(files, FIMFile{Path: path, Hash: hash})
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("invalid expected: manifest is empty")
	}
	encoded, err := EncodeFIMBaseline(files)
	if err != nil {
		return nil, err
	}
	reDigest, _, _ := strings.Cut(encoded, "|m:")
	if reDigest != digestPart {
		return nil, fmt.Errorf("invalid expected: manifest does not match its digest")
	}
	return &FIMBaseline{Digest: strings.TrimPrefix(digestPart, "sha256:"), Files: files}, nil
}

// DiffFIM compares a baseline with the current manifest.
// denied holds paths that exist but could not be read; they are not counted as removed
// (an unreadable file is not a deleted file) but they do make the result not-assessed.
func DiffFIM(base *FIMBaseline, current map[string]string, denied map[string]bool) *FIMDiff {
	d := &FIMDiff{}
	basePaths := make(map[string]bool, len(base.Files))
	for _, f := range base.Files {
		basePaths[f.Path] = true
		cur, ok := current[f.Path]
		switch {
		case !ok:
			if denied[f.Path] {
				continue // unreadable, not deleted
			}
			d.Removed = append(d.Removed, FIMFile{Path: f.Path, Hash: f.Hash})
		case cur != f.Hash:
			d.Modified = append(d.Modified, FIMFile{Path: f.Path, Hash: cur})
		}
	}
	curPaths := make([]string, 0, len(current))
	for p := range current {
		curPaths = append(curPaths, p)
	}
	sort.Strings(curPaths)
	for _, p := range curPaths {
		if !basePaths[p] {
			d.Added = append(d.Added, FIMFile{Path: p, Hash: current[p]})
		}
	}
	return d
}

// FIMTargetPaths returns the literal paths a target expands to (preset included).
func FIMTargetPaths(target string) []string {
	t := strings.TrimSpace(target)
	if t == FIMPresetAuth {
		return append([]string(nil), fimPresetAuthPaths...)
	}
	return fimSplitPaths(t)
}

// FIMExpectedSummary shortens a file_integrity baseline for alert messages and the UI.
func FIMExpectedSummary(expected string) string {
	e := strings.TrimSpace(expected)
	if e == "" {
		return ""
	}
	digest := ""
	if strings.HasPrefix(e, "sha256:") {
		digest = e[len("sha256:"):]
		if len(digest) > 12 {
			digest = digest[:12]
		}
	}
	manifest := ""
	if _, m, ok := strings.Cut(e, "|m:"); ok {
		manifest = fmt.Sprintf("%d file(s)", len(strings.Split(m, ",")))
	}
	switch {
	case digest != "" && manifest != "":
		return fmt.Sprintf("sha256:%s…, %s", digest, manifest)
	case manifest != "":
		return manifest
	default:
		return e
	}
}

// ruleExpectedSummary shortens the expected value of a rule for alert messages.
func ruleExpectedSummary(rule store.DesiredRule) string {
	if rule.Kind == "file_integrity" {
		return FIMExpectedSummary(rule.Expected)
	}
	return rule.Expected
}

// fimHashBlock hashes the file in "$p" and prints one marker line.
func fimHashBlock() string {
	return `if [ -e "$p" ]; then if o=$($S $H "$p" 2>/dev/null); then echo "FIM:OK $p $(printf '%s' "$o" | awk '{print $1}')"; else echo "FIM:DENIED $p"; fi; else echo "FIM:MISSING $p"; fi`
}

// BuildFIMManifestCommand builds the read-only manifest command for a target.
// It never writes to the host: it only hashes files and prints marker lines.
// When trySudo is set, passwordless `sudo -n` is used for files the SSH user cannot read.
func BuildFIMManifestCommand(target string, trySudo bool) string {
	t := strings.TrimSpace(target)
	var b strings.Builder
	if trySudo {
		b.WriteString(`S=''; if [ "$(id -u)" != 0 ]; then if sudo -n true 2>/dev/null; then S='sudo -n'; else echo 'FIM:SUDO_DENIED'; exit 3; fi; fi; `)
	} else {
		b.WriteString(`S=''; `)
	}
	b.WriteString(`if command -v sha256sum >/dev/null 2>&1; then H='sha256sum'; elif command -v shasum >/dev/null 2>&1; then H='shasum -a 256'; else echo 'FIM:NO_HASH_TOOL'; exit 127; fi; `)

	switch {
	case t == FIMTargetSUID:
		b.WriteString(`if command -v find >/dev/null 2>&1; then $S find / -xdev -type f \( -perm -4000 -o -perm -2000 \) 2>/dev/null | LC_ALL=C sort | while IFS= read -r p; do `)
		b.WriteString(fimHashBlock())
		b.WriteString(`; done; else echo 'FIM:NO_FIND'; fi; `)
	case t == FIMPresetAuth:
		for _, p := range fimPresetAuthPaths {
			b.WriteString(`p=` + shellQuote(p) + `; `)
			b.WriteString(fimHashBlock())
			b.WriteString(`; `)
		}
		for _, g := range fimPresetAuthGlobs {
			b.WriteString(`for p in ` + g + `; do [ -e "$p" ] || continue; `)
			b.WriteString(fimHashBlock())
			b.WriteString(`; done; `)
		}
	default:
		for _, p := range fimSplitPaths(t) {
			b.WriteString(`p=` + shellQuote(p) + `; `)
			b.WriteString(fimHashBlock())
			b.WriteString(`; `)
		}
	}
	b.WriteString(`echo 'FIM:DONE'; `)
	return "sh -c " + shellQuote(b.String())
}

// ParseFIMOutput parses the marker lines of a manifest run.
func ParseFIMOutput(out string) *FIMCapture {
	c := &FIMCapture{Files: map[string]string{}}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		switch {
		case strings.HasPrefix(line, "FIM:OK "):
			rest := strings.TrimPrefix(line, "FIM:OK ")
			idx := strings.LastIndex(rest, " ")
			if idx <= 0 {
				continue
			}
			c.Files[rest[:idx]] = rest[idx+1:]
		case strings.HasPrefix(line, "FIM:MISSING "):
			c.Missing = append(c.Missing, strings.TrimPrefix(line, "FIM:MISSING "))
		case strings.HasPrefix(line, "FIM:DENIED "):
			c.Denied = append(c.Denied, strings.TrimPrefix(line, "FIM:DENIED "))
		case line == "FIM:NO_HASH_TOOL":
			c.NoHashTool = true
		case line == "FIM:SUDO_DENIED":
			c.SudoDenied = true
		}
	}
	return c
}

// captureFIM runs the manifest command once and returns the parsed result.
func captureFIM(runner sshrunner.Runner, target string, trySudo bool) (*FIMCapture, error) {
	cmd := BuildFIMManifestCommand(target, trySudo)
	out, stderr, exitCode, err := runner.ExecTimeout(cmd, fimTimeout)
	if err != nil {
		return nil, fmt.Errorf("manifest command failed: %w", err)
	}
	c := ParseFIMOutput(out)
	if exitCode == 124 || strings.Contains(stderr, "timed out") {
		c.TimedOut = true
	}
	return c, nil
}

// FIMCaptureFiles turns a capture into a sorted manifest.
func FIMCaptureFiles(c *FIMCapture) []FIMFile {
	return fimFilesFromCapture(c)
}

// CaptureFIMManifest runs the manifest command for a host target and returns the current state.
func (ins *Inspector) CaptureFIMManifest(hostID int64, runner sshrunner.Runner, target string) (*FIMCapture, error) {
	c, err := captureFIM(runner, target, false)
	if err != nil {
		return nil, err
	}
	// Retry with passwordless sudo only when the plain read could not read something.
	if len(c.Denied) > 0 && ins.sudoProbeAllowed(hostID) {
		c2, err := captureFIM(runner, target, true)
		if err != nil {
			return nil, err
		}
		switch {
		case c2.SudoDenied:
			ins.noteSudo(hostID, true)
		case len(c2.Denied) < len(c.Denied):
			ins.noteSudo(hostID, false)
			c = c2
		}
	}
	return c, nil
}

// fimFilesFromCapture turns a capture into a sorted manifest.
func fimFilesFromCapture(c *FIMCapture) []FIMFile {
	paths := make([]string, 0, len(c.Files))
	for p := range c.Files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	files := make([]FIMFile, 0, len(paths))
	for _, p := range paths {
		files = append(files, FIMFile{Path: p, Hash: c.Files[p]})
	}
	return files
}

// FIMDiffAgainst compares the current host state with a baseline manifest.
func (ins *Inspector) FIMDiffAgainst(hostID int64, runner sshrunner.Runner, target string, base *FIMBaseline) (*FIMDiff, *FIMCapture, error) {
	c, err := ins.CaptureFIMManifest(hostID, runner, target)
	if err != nil {
		return nil, nil, err
	}
	return DiffFIM(base, c.Files, fimDeniedSet(c)), c, nil
}

// fimDeniedSet maps the unreadable paths of a capture to a set.
func fimDeniedSet(c *FIMCapture) map[string]bool {
	set := make(map[string]bool, len(c.Denied))
	for _, p := range c.Denied {
		set[p] = true
	}
	return set
}

// evaluateFIMRule evaluates a file_integrity rule.
func (ins *Inspector) evaluateFIMRule(h *store.Host, runner sshrunner.Runner, rule *store.DesiredRule) (current, status, details, rootCause string) {
	if err := ValidateFIMTarget(rule.Target); err != nil {
		return "invalid_target", "drift", "", err.Error()
	}
	base, err := ParseFIMExpected(rule.Expected)
	if err != nil {
		return "invalid_baseline", "drift", "", err.Error()
	}
	c, err := ins.CaptureFIMManifest(h.ID, runner, rule.Target)
	if err != nil {
		return "not_assessed", "not_assessed", "manifest could not be read: " + err.Error(), err.Error()
	}
	if c.NoHashTool {
		return "not_assessed", "not_assessed", "no sha256 tool (sha256sum/shasum) on the host", "no sha256 tool (sha256sum/shasum) on the host"
	}
	if c.TimedOut {
		return "not_assessed", "not_assessed", "manifest command timed out", "manifest command timed out"
	}
	if c.SudoDenied {
		return "not_assessed", "not_assessed", fmt.Sprintf("%d file(s) could not be read and passwordless sudo is unavailable", len(c.Denied)), "passwordless sudo unavailable; some files could not be read"
	}

	diff := DiffFIM(base, c.Files, fimDeniedSet(c))
	changed := len(diff.Added) + len(diff.Removed) + len(diff.Modified)
	if changed > 0 {
		parts := []string{}
		parts = append(parts, "modified: "+fimFormatFiles(diff.Modified))
		if len(diff.Removed) > 0 {
			parts = append(parts, "removed: "+fimFormatFiles(diff.Removed))
		}
		if len(diff.Added) > 0 {
			parts = append(parts, "added: "+fimFormatFiles(diff.Added))
		}
		details = strings.Join(parts, "\n")
		if len(c.Denied) > 0 {
			details += fmt.Sprintf("\nnot readable: %s", strings.Join(c.Denied, ", "))
		}
		return "drift", "drift", details, "File integrity drift detected\n" + details
	}
	if len(c.Denied) > 0 {
		// Honest coverage: something could not be read, so this is not a clean result.
		msg := fmt.Sprintf("%d file(s) could not be read: %s", len(c.Denied), strings.Join(c.Denied, ", "))
		return "not_assessed", "not_assessed", msg, msg
	}
	return "ok", "ok", fmt.Sprintf("%d file(s) match the baseline", len(c.Files)), ""
}

// fimFormatFiles lists changed paths, capped.
func fimFormatFiles(files []FIMFile) string {
	if len(files) == 0 {
		return "-"
	}
	limit := len(files)
	if limit > FIMMaxChangedFiles {
		limit = FIMMaxChangedFiles
	}
	names := make([]string, 0, limit)
	for _, f := range files[:limit] {
		names = append(names, f.Path)
	}
	out := strings.Join(names, ", ")
	if len(files) > limit {
		out += fmt.Sprintf(" ... and %d more", len(files)-limit)
	}
	return out
}
