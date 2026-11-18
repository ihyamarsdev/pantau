package vault

import (
	"io"
	"sort"
	"strings"
	"sync"
)

const redactedMarker = "[REDACTED]"

// Redactor holds a set of secret values and replaces them in arbitrary text.
// It is safe for concurrent use.
type Redactor struct {
	mu     sync.RWMutex
	values []string // kept sorted by descending length
}

// NewRedactor returns an empty Redactor.
func NewRedactor() *Redactor {
	return &Redactor{}
}

// SetValues replaces the current values with vals. Empty strings are ignored.
func (r *Redactor) SetValues(vals []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.values = normalizeValues(vals)
}

// AddValue adds v to the current values. Empty strings are ignored.
func (r *Redactor) AddValue(v string) {
	if v == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.values {
		if existing == v {
			return
		}
	}
	r.values = append(r.values, v)
	sortValues(r.values)
}

// Clear removes all values.
func (r *Redactor) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.values = nil
}

// Redact replaces every known value in s with "[REDACTED]". At each position
// the longest matching secret is chosen so that overlapping secrets (one a
// substring of another) are fully removed without leaking a partial match.
func (r *Redactor) Redact(s string) string {
	r.mu.RLock()
	values := r.values
	if len(values) == 0 {
		r.mu.RUnlock()
		return s
	}
	// Take a private copy so we never mutate the shared slice while other
	// readers may be iterating it. The shared slice is sorted longest-first.
	snapshot := make([]string, len(values))
	copy(snapshot, values)
	r.mu.RUnlock()

	// Single pass: at each position, replace the longest matching secret.
	// This avoids re-scanning already-inserted "[REDACTED]" markers (a later
	// shorter secret could otherwise match inside a marker and corrupt it).
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		matched := ""
		for _, v := range snapshot {
			if strings.HasPrefix(s[i:], v) {
				matched = v
				break // values are longest-first
			}
		}
		if matched != "" {
			b.WriteString(redactedMarker)
			i += len(matched)
		} else {
			b.WriteByte(s[i])
			i++
		}
	}
	return b.String()
}

// Writer returns an io.Writer that redacts each Write before passing it to w.
// On success it reports len(p), the number of original (unredacted) bytes the
// caller attempted to write.
func (r *Redactor) Writer(w io.Writer) io.Writer {
	return &redactWriter{r: r, w: w}
}

type redactWriter struct {
	r *Redactor
	w io.Writer
}

func (rw *redactWriter) Write(p []byte) (int, error) {
	redacted := rw.r.Redact(string(p))
	n, err := rw.w.Write([]byte(redacted))
	if err != nil {
		return n, err
	}
	if n != len(redacted) {
		return n, io.ErrShortWrite
	}
	return len(p), nil
}

// normalizeValues filters out empty strings, removes duplicates, and returns
// the result sorted longest-first (stable for equal lengths).
func normalizeValues(vals []string) []string {
	out := make([]string, 0, len(vals))
	seen := make(map[string]bool, len(vals))
	for _, v := range vals {
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	sortValues(out)
	return out
}

func sortValues(vals []string) {
	sort.SliceStable(vals, func(i, j int) bool {
		return len(vals[i]) > len(vals[j])
	})
}
