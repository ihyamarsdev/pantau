package inspector

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"pantau/internal/sshrunner"
	"pantau/internal/store"
)

var (
	backupPatternRe = regexp.MustCompile(`^[A-Za-z0-9_./*?+@%:,=~-]+$`)
)

// BackupExpected holds parsed expectations for kind "backup".
type BackupExpected struct {
	MaxHours float64 // default 24.0
	Verify   string  // "gzip", "tar", "zstd", "xz", "sha256", or ""
}

// ValidateBackupPattern validates target glob/file pattern for backup rules strictly.
// Must be non-empty, absolute path (starts with /), match ^[A-Za-z0-9_./*?+@%:,=~-]+$,
// and not contain directory traversal "..".
func ValidateBackupPattern(target string) error {
	if strings.ContainsAny(target, " \t\r\n") {
		return fmt.Errorf("backup target cannot contain whitespace or newlines")
	}
	target = strings.TrimSpace(target)
	if target == "" {
		return fmt.Errorf("backup target cannot be empty")
	}
	if !strings.HasPrefix(target, "/") {
		return fmt.Errorf("backup target must be an absolute path starting with /")
	}
	if strings.Contains(target, "..") {
		return fmt.Errorf("backup target contains directory traversal (..)")
	}
	if !backupPatternRe.MatchString(target) {
		return fmt.Errorf("backup target contains invalid characters (allowed: [A-Za-z0-9_./*?+@%%:,=~-])")
	}
	return nil
}

// ParseBackupExpected parses Expected for kind "backup".
// Format: fresh_<N>h[,verify=<gzip|tar|zstd|xz|sha256>] or fresh_<N>d
// Default maxHours is 24 if unspecified or not matching fresh_<N>h.
func ParseBackupExpected(expected string) (*BackupExpected, error) {
	exp := &BackupExpected{
		MaxHours: 24.0,
	}
	expected = strings.TrimSpace(expected)
	if expected == "" {
		return exp, nil
	}

	parts := strings.Split(expected, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if strings.HasPrefix(part, "fresh_") {
			raw := strings.TrimPrefix(part, "fresh_")
			if strings.HasSuffix(raw, "h") {
				h, err := strconv.ParseFloat(strings.TrimSuffix(raw, "h"), 64)
				if err != nil || h <= 0 {
					return nil, fmt.Errorf("invalid fresh window: %s", part)
				}
				exp.MaxHours = h
			} else if strings.HasSuffix(raw, "d") {
				d, err := strconv.ParseFloat(strings.TrimSuffix(raw, "d"), 64)
				if err != nil || d <= 0 {
					return nil, fmt.Errorf("invalid fresh window: %s", part)
				}
				exp.MaxHours = d * 24.0
			} else {
				h, err := strconv.ParseFloat(raw, 64)
				if err != nil || h <= 0 {
					return nil, fmt.Errorf("invalid fresh window: %s", part)
				}
				exp.MaxHours = h
			}
		} else if strings.HasPrefix(part, "verify=") {
			v := strings.ToLower(strings.TrimPrefix(part, "verify="))
			switch v {
			case "gzip", "tar", "zstd", "xz", "sha256":
				exp.Verify = v
			default:
				return nil, fmt.Errorf("invalid backup verify type: %s (allowed: gzip, tar, zstd, xz, sha256)", v)
			}
		} else {
			return nil, fmt.Errorf("invalid backup expected token: %s", part)
		}
	}
	return exp, nil
}

// BuildBackupStatCommand builds command to locate newest file matching pattern and stat it.
// Uses `ls -t -- PATTERN | head -n 1` so glob expands in shell, then stat on macOS or Linux.
func BuildBackupStatCommand(pattern string) string {
	// sh -c 'F=$(ls -t -- PATTERN 2>/dev/null | head -n 1); [ -n "$F" ] || exit 1; if [ "$(uname -s)" = "Darwin" ]; then stat -f "%m %z $F" "$F"; else stat -c "%s %Y %n" "$F" 2>/dev/null || ls -l "$F"; fi'
	// Notice pattern is passed inside single quotes but with glob active in sh:
	// To allow glob expansion in sh without quoting metacharacters, we validate pattern strictly first!
	// In sh, passing PATTERN unquoted (or with ls -t -- PATTERN) expands glob.
	return fmt.Sprintf(`sh -c 'F=$(ls -t -d -- %s 2>/dev/null | head -n 1); [ -n "$F" ] || exit 1; if [ "$(uname -s)" = "Darwin" ]; then stat -f "%%m %%z $F" "$F" 2>/dev/null; else stat -c "%%s %%Y $F" "$F" 2>/dev/null || ls -l "$F" 2>/dev/null; fi'`, pattern)
}

// BuildBackupVerifyCommand builds read-only verify command for a specific file.
// verify is one of: gzip, tar, zstd, xz, sha256.
func BuildBackupVerifyCommand(file, verifyType string) string {
	qFile := shellQuote(file)
	var verifySubCmd string
	switch verifyType {
	case "gzip":
		verifySubCmd = fmt.Sprintf(`if ! command -v gzip >/dev/null 2>&1; then echo "gzip: command not found" >&2; exit 127; fi; gzip -t %s`, qFile)
	case "tar":
		verifySubCmd = fmt.Sprintf(`if ! command -v tar >/dev/null 2>&1; then echo "tar: command not found" >&2; exit 127; fi; tar -tf %s >/dev/null`, qFile)
	case "zstd":
		verifySubCmd = fmt.Sprintf(`if ! command -v zstd >/dev/null 2>&1; then echo "zstd: command not found" >&2; exit 127; fi; zstd -t %s`, qFile)
	case "xz":
		verifySubCmd = fmt.Sprintf(`if ! command -v xz >/dev/null 2>&1; then echo "xz: command not found" >&2; exit 127; fi; xz -t %s`, qFile)
	case "sha256":
		sidecar := file + ".sha256"
		verifySubCmd = fmt.Sprintf(`if [ ! -f %[1]s ]; then echo "no checksum file: %[1]s" >&2; exit 2; fi; if ! command -v sha256sum >/dev/null 2>&1; then echo "sha256sum: command not found" >&2; exit 127; fi; cd %[2]s && sha256sum -c %[3]s`,
			shellQuote(sidecar), shellQuote(dirOf(file)), shellQuote(baseOf(sidecar)))
	}

	return "sh -c " + shellQuote(fmt.Sprintf(`if command -v timeout >/dev/null 2>&1; then timeout 60 sh -c %s; else %s; fi`,
		shellQuote(verifySubCmd), verifySubCmd))
}

func dirOf(p string) string {
	idx := strings.LastIndex(p, "/")
	if idx <= 0 {
		return "/"
	}
	return p[:idx]
}

func baseOf(p string) string {
	idx := strings.LastIndex(p, "/")
	if idx < 0 {
		return p
	}
	return p[idx+1:]
}

// formatBytes formats byte sizes nicely e.g. 1.2 MB, 500 B, 45 KB.
func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	sizes := []string{"KB", "MB", "GB", "TB"}
	return fmt.Sprintf("%.1f %s", float64(bytes)/float64(div), sizes[exp])
}

// evaluateBackupRule evaluates rule of kind "backup".
func evaluateBackupRule(runner sshrunner.Runner, rule *store.DesiredRule) (current, status, details, rootCause string) {
	if err := ValidateBackupPattern(rule.Target); err != nil {
		return "invalid_target", "drift", "", err.Error()
	}
	exp, err := ParseBackupExpected(rule.Expected)
	if err != nil {
		return "invalid_expected", "drift", "", err.Error()
	}

	statCmd := BuildBackupStatCommand(rule.Target)
	out, stderr, exitCode, _ := runner.Exec(statCmd)
	out = strings.TrimSpace(out)

	if exitCode != 0 || out == "" {
		current = "missing"
		status = "drift"
		rootCause = fmt.Sprintf("Backup file does not exist at path: %s", rule.Target)
		return
	}

	fields := strings.Fields(out)
	if len(fields) >= 2 {
		f0, _ := strconv.ParseInt(fields[0], 10, 64)
		f1, _ := strconv.ParseInt(fields[1], 10, 64)
		var sizeBytes, mtimeUnix int64
		if f0 > 1000000000 && (f1 < 1000000000 || f0 > f1) {
			mtimeUnix = f0
			sizeBytes = f1
		} else {
			sizeBytes = f0
			mtimeUnix = f1
		}
		actualFile := rule.Target
		if len(fields) >= 3 {
			actualFile = fields[2]
		}

		modTime := time.Unix(mtimeUnix, 0)
		ageHours := time.Since(modTime).Hours()

		details = fmt.Sprintf("File: %s, Size: %d bytes, Modified: %s (%.1fh ago)", actualFile, sizeBytes, modTime.Format(time.RFC3339), ageHours)

		if sizeBytes == 0 {
			current = "empty_file (0 bytes)"
			status = "drift"
			rootCause = fmt.Sprintf("Backup file exists but is 0 bytes empty at %s", actualFile)
			return
		}

		maxHours := exp.MaxHours
		if ageHours > maxHours {
			current = fmt.Sprintf("stale (%.1fh ago)", ageHours)
			status = "drift"
			rootCause = fmt.Sprintf("Backup file at %s is stale (age: %.1f hours, max allowed: %.1f hours)", actualFile, ageHours, maxHours)
			return
		}

		// File is present, non-empty, and fresh. Run verification if configured.
		if exp.Verify != "" {
			verifyCmd := BuildBackupVerifyCommand(actualFile, exp.Verify)
			vOut, vErr, vExit, _ := runner.Exec(verifyCmd)
			vOut = strings.TrimSpace(vOut)
			vErr = strings.TrimSpace(vErr)

			if vExit == 127 || strings.Contains(vErr, "command not found") || strings.Contains(vOut, "command not found") {
				current = fmt.Sprintf("fresh (%.1fh ago, %s), %s tool missing", ageHours, formatBytes(sizeBytes), exp.Verify)
				status = "drift"
				toolName := exp.Verify
				if toolName == "sha256" {
					toolName = "sha256sum"
				}
				rootCause = fmt.Sprintf("Verification tool %s not found on host", toolName)
				return
			}

			if strings.Contains(vErr, "no checksum file") || strings.Contains(vOut, "no checksum file") {
				current = fmt.Sprintf("fresh (%.1fh ago, %s), no checksum file", ageHours, formatBytes(sizeBytes))
				status = "drift"
				rootCause = fmt.Sprintf("no checksum file for %s", actualFile)
				return
			}

			if vExit != 0 {
				current = fmt.Sprintf("fresh (%.1fh ago, %s), %s verification failed", ageHours, formatBytes(sizeBytes), exp.Verify)
				status = "drift"
				msg := vErr
				if msg == "" {
					msg = vOut
				}
				if msg == "" {
					msg = fmt.Sprintf("verification process exited with code %d", vExit)
				}
				if len(msg) > 500 {
					msg = msg[:500] + "..."
				}
				rootCause = fmt.Sprintf("Backup verification failed (%s): %s", exp.Verify, msg)
				return
			}

			// Verification succeeded
			current = fmt.Sprintf("fresh (%.1fh ago, %s), %s ok", ageHours, formatBytes(sizeBytes), exp.Verify)
			status = "ok"
			return
		}

		// Legacy backup rule (no verify)
		current = fmt.Sprintf("fresh (%.1fh ago, %d B)", ageHours, sizeBytes)
		status = "ok"
		return
	}

	current = "stat_error"
	status = "drift"
	rootCause = "Could not parse backup file metadata"
	if stderr != "" {
		rootCause += ": " + stderr
	}
	return
}
