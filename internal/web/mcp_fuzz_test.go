package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"pantau/internal/inspector"
	"pantau/internal/sshrunner"
	"pantau/internal/store"
)

// FuzzMCPRequestHandler feeds arbitrary bytes to the MCP endpoint. Malformed JSON-RPC must never panic and must
// always get a well-formed answer: a JSON-RPC response, an empty 202, or a plain HTTP error.
//
// Smoke run: go test -run '^$' -fuzz FuzzMCPRequestHandler -fuzztime=5s ./internal/web
func FuzzMCPRequestHandler(f *testing.F) {
	seeds := []string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize"}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"pantau_list_hosts","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"pantau_get_host_detail","arguments":{"host_id":"x"}}}`,
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"exec_command","arguments":{"cmd":"rm -rf /"}}}`,
		`[`,
		`{"jsonrpc":"2.0","id":null,"method":null}`,
		"",
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}

	db, err := store.Open(filepath.Join(f.TempDir(), "pantau.db"))
	if err != nil {
		f.Fatal(err)
	}
	f.Cleanup(func() { _ = db.Close() })
	factory := func(sshrunner.Target) (sshrunner.Runner, error) { return sshrunner.NewMockRunner(), nil }
	srv := NewServer(db, inspector.New(db, factory, nil), nil)
	srv.SetMCPEnabled(true)
	token, err := srv.generateMCPToken()
	if err != nil {
		f.Fatal(err)
	}

	f.Fuzz(func(t *testing.T, body []byte) {
		req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
		req.RemoteAddr = mcpLoopback
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)

		switch w.Code {
		case http.StatusAccepted:
			if w.Body.Len() != 0 {
				t.Fatalf("202 carries a body: %q", w.Body.String())
			}
		case http.StatusOK, http.StatusBadRequest:
			if !json.Valid(w.Body.Bytes()) {
				t.Fatalf("status %d with non-JSON body: %q", w.Code, w.Body.String())
			}
		default:
			t.Fatalf("unexpected status %d for input %q", w.Code, body)
		}
	})
}
