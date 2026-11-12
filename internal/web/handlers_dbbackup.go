package web

import (
	"compress/gzip"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"pantau/internal/inspector"
	"pantau/internal/logger"
)

// Database backup of a host (MySQL/MariaDB, PostgreSQL, MongoDB; installed on
// the host or running in Docker containers):
//
//	GET /api/databases?host_id=N                                  the database servers found on the host
//	GET /api/databases/backup?host_id=N&where=host|container
//	                         &engine=mysql|postgres|mongo&container=NAME
//	                                                              one gzip-compressed dump, streamed (POST also accepted: a MongoDB
//	                                                              login goes in the body as user, password, auth_db)
//
// Nothing is stored on the Pantau server: the dump flows from the host through
// Pantau to the browser.

func (s *Server) registerDatabaseRoutes() {
	s.mux.HandleFunc("/api/databases", s.authMiddleware(s.handleDatabases))
	s.mux.HandleFunc("/api/databases/backup", s.authMiddleware(s.handleDatabaseBackup))
}

// slugName keeps only characters that are safe in a download file name.
func slugName(s string) string {
	return strings.Map(func(c rune) rune {
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '-' {
			return c
		}
		return '_'
	}, s)
}

func (s *Server) handleDatabases(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id, err := strconv.ParseInt(r.URL.Query().Get("host_id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "host_id must be a number"})
		return
	}
	host, err := s.db.GetHost(id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	if host == nil {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "host not found"})
		return
	}
	if s.inspector == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "inspector not available"})
		return
	}
	sources, err := s.inspector.ScanDatabases(id)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]interface{}{"error": err.Error()})
		return
	}
	if sources == nil {
		sources = []inspector.DBSource{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"sources": sources})
}

// dbDumpWriter starts the gzip download on the first byte of the dump, so a dump
// that fails right away (bad credentials, missing tool) can still answer with a
// plain error instead of an empty download.
type dbDumpWriter struct {
	w        http.ResponseWriter
	filename string
	gz       *gzip.Writer
}

func (d *dbDumpWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if d.gz == nil {
		d.w.Header().Set("Content-Type", "application/gzip")
		d.w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", d.filename))
		d.gz, _ = gzip.NewWriterLevel(d.w, gzip.BestSpeed)
	}
	return d.gz.Write(p)
}

func (s *Server) handleDatabaseBackup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	_ = r.ParseForm() // POST body (the MongoDB login must not travel in the URL) and query
	id, err := strconv.ParseInt(r.Form.Get("host_id"), 10, 64)
	if err != nil {
		http.Error(w, "host_id must be a number", http.StatusBadRequest)
		return
	}
	engine, where, container := r.Form.Get("engine"), r.Form.Get("where"), r.Form.Get("container")
	// Only the POST body can carry a login: a URL ends up in logs and history.
	stdin := ""
	if engine == "mongo" {
		if stdin, err = inspector.DBMongoLogin(r.PostForm.Get("user"), r.PostForm.Get("password"), r.PostForm.Get("auth_db")); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	if _, err := inspector.DBDumpCommand(engine, where, container); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	host, err := s.db.GetHost(id)
	if err != nil || host == nil {
		http.Error(w, "host not found", http.StatusNotFound)
		return
	}
	if s.inspector == nil {
		http.Error(w, "inspector not available", http.StatusInternalServerError)
		return
	}
	// One dump per host at a time: a dump loads the database and holds an SSH session.
	if _, busy := s.dbBackupActive.LoadOrStore(id, struct{}{}); busy {
		http.Error(w, "a database backup is already running for this host", http.StatusTooManyRequests)
		return
	}
	defer s.dbBackupActive.Delete(id)

	label := engine
	if where == "container" {
		label = container + "-" + engine
	}
	dw := &dbDumpWriter{w: w, filename: slugName(host.Name) + "-" + slugName(label) + "-" + time.Now().Format("20060102-150405") + inspector.DBDumpExt(engine)}
	start := time.Now()
	err = s.inspector.DumpDatabase(r.Context(), id, engine, where, container, stdin, dw)
	if dw.gz == nil {
		msg := s.inspector.DiagnoseDatabaseDump(id, engine, where, container, stdin)
		if msg == "" && err != nil {
			msg = err.Error()
		}
		if msg == "" {
			msg = "the dump produced no output"
		}
		logger.Warn("databases", host.Name, fmt.Sprintf("%s backup (%s %s) failed: %s", engine, where, container, msg))
		http.Error(w, "Database backup failed: "+msg, http.StatusBadGateway)
		return
	}
	if err != nil {
		// The gzip stream is left unfinished on purpose: a truncated dump must not look complete.
		logger.Warn("databases", host.Name, fmt.Sprintf("%s backup (%s %s) stopped midway: %v", engine, where, container, err))
		return
	}
	_ = dw.gz.Close()
	logger.Info("databases", host.Name, fmt.Sprintf("%s backup streamed (%s %s) in %s", engine, where, container, time.Since(start).Round(time.Second)))
}
