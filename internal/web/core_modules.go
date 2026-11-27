package web

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"sync"
)

// coreModuleCache holds pre-compressed native ES modules. Each module is served as its own
// file so relative `import` specifiers resolve; the gzip buffer and ETag are built once per
// process, mirroring the in-memory strategy of ADR-0018 / serveAppBundle.
var coreModuleCache = struct {
	sync.Mutex
	m map[string]*appBundle
}{m: map[string]*appBundle{}}

// serveCoreModules serves one embedded static/app/core/<name>.js module, with ETag
// revalidation and pre-compressed gzip. No bundler, no external dependencies.
func serveCoreModules(w http.ResponseWriter, r *http.Request) {
	name := path.Base(r.URL.Path)
	if !strings.HasSuffix(name, ".js") {
		http.NotFound(w, r)
		return
	}

	coreModuleCache.Lock()
	b, ok := coreModuleCache.m[name]
	coreModuleCache.Unlock()
	if !ok {
		raw, err := fs.ReadFile(embeddedFS, "static/app/core/"+name)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		var gzBuf bytes.Buffer
		gw, _ := gzip.NewWriterLevel(&gzBuf, gzip.BestCompression)
		_, _ = gw.Write(raw)
		_ = gw.Close()
		sum := sha256.Sum256(raw)
		b = &appBundle{body: raw, gz: gzBuf.Bytes(), etag: fmt.Sprintf(`"%x"`, sum[:8])}
		coreModuleCache.Lock()
		coreModuleCache.m[name] = b
		coreModuleCache.Unlock()
	}

	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("ETag", b.etag)
	w.Header().Set("Vary", "Accept-Encoding")
	if r.Header.Get("If-None-Match") == b.etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
		w.Header().Set("Content-Encoding", "gzip")
		_, _ = w.Write(b.gz) // #nosec G705 -- static bundled module asset, content type set by the caller
		return
	}
	_, _ = w.Write(b.body) // #nosec G705 -- static bundled module asset, content type set by the caller
}
