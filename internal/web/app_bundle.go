package web

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"io/fs"
	"net/http"
	"sort"
	"strings"
	"sync"
)

// appBundle is one embedded directory of same-extension files concatenated in lexical filename order.
type appBundle struct {
	body []byte
	gz   []byte
	etag string
}

var (
	appBundleMu    sync.Mutex
	appBundleCache = map[string]*appBundle{}
)

// buildAppBundle concatenates static/app/<dir>/*<ext> (embedded and immutable, so it is built once per process).
func buildAppBundle(dir, ext string) (*appBundle, error) {
	key := dir + "|" + ext
	appBundleMu.Lock()
	defer appBundleMu.Unlock()
	if b, ok := appBundleCache[key]; ok {
		return b, nil
	}
	root := "static/app/" + dir
	entries, err := fs.ReadDir(embeddedFS, root)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ext) {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	var buf bytes.Buffer
	for _, n := range names {
		raw, err := fs.ReadFile(embeddedFS, root+"/"+n)
		if err != nil {
			return nil, err
		}
		fmt.Fprintf(&buf, "/* ---- %s ---- */\n", n)
		buf.Write(raw)
		if ext == ".js" {
			buf.WriteString("\n;\n") // a missing semicolon at the end of one module must not swallow the next
		} else {
			buf.WriteString("\n") // a stray ';' would turn the next CSS rule into a dropped qualified rule
		}
	}
	var gzBuf bytes.Buffer
	gw, _ := gzip.NewWriterLevel(&gzBuf, gzip.BestCompression)
	_, _ = gw.Write(buf.Bytes())
	_ = gw.Close()
	sum := sha256.Sum256(buf.Bytes())
	b := &appBundle{body: buf.Bytes(), gz: gzBuf.Bytes(), etag: fmt.Sprintf(`"%x"`, sum[:8])}
	appBundleCache[key] = b
	return b, nil
}

// serveAppBundle serves a bundle with ETag revalidation and pre-compressed gzip.
func serveAppBundle(dir, ext, contentType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		b, err := buildAppBundle(dir, ext)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("ETag", b.etag)
		w.Header().Set("Vary", "Accept-Encoding")
		if r.Header.Get("If-None-Match") == b.etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			w.Header().Set("Content-Encoding", "gzip")
			_, _ = w.Write(b.gz)
			return
		}
		_, _ = w.Write(b.body)
	}
}

// serveGlobeHiresChunk serves one high-resolution admin-2 province chunk. Each chunk is a standalone
// JS file (window.GLOBE_ADMIN2_HIRES['<code>'] = ...) loaded lazily by the globe. Cached and gzipped in
// memory just like the other bundles (ADR 0018); the code path is validated against the embedded dir.
func serveGlobeHiresChunk() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		code := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/app/globe-admin2-hires/"), ".js")
		if code == "" || strings.ContainsAny(code, "/\\.") || len(code) > 8 {
			http.NotFound(w, r)
			return
		}
		name := "static/app/globe-admin2-hires/" + code + ".js"
		raw, err := embeddedFS.ReadFile(name)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		var gzBuf bytes.Buffer
		gw, _ := gzip.NewWriterLevel(&gzBuf, gzip.BestCompression)
		_, _ = gw.Write(raw)
		_ = gw.Close()
		sum := sha256.Sum256(raw)
		etag := fmt.Sprintf(`"%x"`, sum[:8])
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		w.Header().Set("ETag", etag)
		w.Header().Set("Vary", "Accept-Encoding")
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			w.Header().Set("Content-Encoding", "gzip")
			_, _ = w.Write(gzBuf.Bytes())
			return
		}
		_, _ = w.Write(raw)
	}
}
