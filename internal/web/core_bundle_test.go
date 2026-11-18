package web

import (
	"io/fs"
	"sort"
	"strings"
	"testing"
)

// coreJSSource concatenates every native ES module under static/app/core so tests that
// historically inspected the monolithic inline <script> can keep asserting on the same
// function definitions after the modularisation.
func coreJSSource(t *testing.T) string {
	t.Helper()
	entries, err := fs.ReadDir(embeddedFS, "static/app/core")
	if err != nil {
		t.Fatalf("read core modules: %v", err)
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".js") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	var b strings.Builder
	for _, n := range names {
		raw, err := fs.ReadFile(embeddedFS, "static/app/core/"+n)
		if err != nil {
			t.Fatalf("read core module %s: %v", n, err)
		}
		b.Write(raw)
		b.WriteString("\n")
	}
	return b.String()
}

// uiSourceForTest returns the HTML plus all modularised core JS, so existing assertions
// that searched index.html for a function body keep working after the split.
func uiSourceForTest(t *testing.T) string {
	t.Helper()
	return string(embeddedHTML) + "\n" + coreJSSource(t)
}
