package web

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"

	"pantau/internal/logger"
)

// Pantau as a local, read-only MCP server (Streamable HTTP transport, JSON-RPC 2.0).
//
// Off by default. Enabled with the -mcp flag or the mcp_enabled setting. Only loopback clients get in, every request
// needs the MCP token (stored as a SHA-256 hash, shown once when generated), and the registry holds no mutating tool.

const (
	mcpProtocolVersion = "2025-11-25"
	mcpMaxBody         = 1 << 20
	mcpSettingEnabled  = "mcp_enabled"
	mcpSettingTokenH   = "mcp_token_hash"
	mcpTokenPrefix     = "pmcp_"
)

// JSON-RPC 2.0 error codes.
const (
	rpcParseError     = -32700
	rpcInvalidRequest = -32600
	rpcMethodNotFound = -32601
	rpcInvalidParams  = -32602
)

type mcpRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type mcpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type mcpResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *mcpError       `json:"error,omitempty"`
}

// SetMCPEnabled turns the MCP server on for this process (the -mcp flag). The setting can still enable it.
func (s *Server) SetMCPEnabled(on bool) { s.mcpForced = on }

// mcpEnabled reports whether /mcp serves requests: the flag, or the stored setting.
func (s *Server) mcpEnabled() bool {
	if s.mcpForced {
		return true
	}
	if s.db == nil {
		return false
	}
	v, _ := s.db.GetSetting(mcpSettingEnabled)
	return v == "1"
}

// loopbackAddr reports whether a remote "host:port" address is a loopback IP.
func loopbackAddr(remote string) bool {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		host = remote
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// loopbackOrigin accepts an Origin whose host is localhost or a loopback IP (DNS rebinding guard).
func loopbackOrigin(origin string) bool {
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	h := u.Hostname()
	if h == "localhost" {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

// mcpTokenHash is the stored form of a token; the token itself is never persisted.
func mcpTokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// mcpAuthorized checks the Bearer token in constant time against the stored hash.
func (s *Server) mcpAuthorized(r *http.Request) bool {
	h := r.Header.Get("Authorization")
	if len(h) < len("Bearer ") || !strings.EqualFold(h[:len("Bearer ")], "Bearer ") {
		return false
	}
	token := strings.TrimSpace(h[len("Bearer "):])
	if token == "" || s.db == nil {
		return false
	}
	stored, _ := s.db.GetSetting(mcpSettingTokenH)
	if stored == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(mcpTokenHash(token)), []byte(stored)) == 1
}

// generateMCPToken makes a new random token, stores its hash (replacing any previous token) and returns it once.
func (s *Server) generateMCPToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	token := mcpTokenPrefix + base64.RawURLEncoding.EncodeToString(buf)
	if err := s.db.SetSetting(mcpSettingTokenH, mcpTokenHash(token)); err != nil {
		return "", err
	}
	return token, nil
}

// handleMCP serves the MCP endpoint. Checks run in order: enabled, loopback, Origin, token, method, body.
func (s *Server) handleMCP(w http.ResponseWriter, r *http.Request) {
	if !s.mcpEnabled() {
		http.NotFound(w, r)
		return
	}
	if !loopbackAddr(r.RemoteAddr) {
		http.Error(w, "forbidden: MCP accepts loopback clients only", http.StatusForbidden)
		return
	}
	if o := r.Header.Get("Origin"); o != "" && !loopbackOrigin(o) {
		http.Error(w, "forbidden: origin is not loopback", http.StatusForbidden)
		return
	}
	if !s.mcpAuthorized(r) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="pantau-mcp"`)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, mcpMaxBody+1))
	if err != nil {
		http.Error(w, "read error", http.StatusBadRequest)
		return
	}
	if len(body) > mcpMaxBody {
		http.Error(w, "request too large", http.StatusRequestEntityTooLarge)
		return
	}
	resp, status := s.mcpDispatch(r.Context(), body)
	if resp == nil {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(resp)
}

// mcpDispatch handles one JSON-RPC message. It returns nil for a notification (answered with 202).
func (s *Server) mcpDispatch(ctx context.Context, body []byte) (*mcpResponse, int) {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return mcpFail(nil, rpcParseError, "empty body"), http.StatusBadRequest
	}
	if trimmed[0] == '[' {
		return mcpFail(nil, rpcInvalidRequest, "batch requests are not supported"), http.StatusBadRequest
	}
	var req mcpRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return mcpFail(nil, rpcParseError, "invalid JSON"), http.StatusBadRequest
	}
	if req.JSONRPC != "2.0" || req.Method == "" {
		return mcpFail(req.ID, rpcInvalidRequest, "expected a JSON-RPC 2.0 request"), http.StatusBadRequest
	}
	if len(req.ID) == 0 {
		// Notifications (for example notifications/initialized) get no response body.
		return nil, http.StatusAccepted
	}

	switch req.Method {
	case "initialize":
		return mcpOK(req.ID, map[string]any{
			"protocolVersion": mcpProtocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "pantau", "version": "1"},
		}), http.StatusOK
	case "ping":
		return mcpOK(req.ID, map[string]any{}), http.StatusOK
	case "tools/list":
		return mcpOK(req.ID, map[string]any{"tools": mcpToolList()}), http.StatusOK
	case "tools/call":
		return s.mcpCall(ctx, req), http.StatusOK
	default:
		return mcpFail(req.ID, rpcMethodNotFound, "method not found: "+req.Method), http.StatusOK
	}
}

// mcpCall runs a registered read-only tool. A tool failure is reported inside the result (isError), not as a
// protocol error, as the MCP tools specification asks. An unknown name, mutation names included, is a protocol error.
func (s *Server) mcpCall(ctx context.Context, req mcpRequest) *mcpResponse {
	var p struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil || p.Name == "" {
		return mcpFail(req.ID, rpcInvalidParams, "tools/call needs a tool name")
	}
	tool, ok := mcpToolByName(p.Name)
	if !ok {
		return mcpFail(req.ID, rpcInvalidParams, "unknown tool: "+p.Name)
	}
	logger.Info("mcp", "", "tool call: "+p.Name)
	data, err := tool.run(s, ctx, p.Arguments)
	if err != nil {
		return mcpOK(req.ID, mcpToolResult(err.Error(), true))
	}
	raw, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return mcpOK(req.ID, mcpToolResult("result could not be encoded", true))
	}
	return mcpOK(req.ID, mcpToolResult(string(raw), false))
}

// mcpToolResult wraps text in the MCP content envelope. Every tool result goes through the redaction filter here.
func mcpToolResult(text string, isError bool) map[string]any {
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": mcpRedact(text)}},
		"isError": isError,
	}
}

func mcpOK(id json.RawMessage, result any) *mcpResponse {
	return &mcpResponse{JSONRPC: "2.0", ID: mcpID(id), Result: result}
}

func mcpFail(id json.RawMessage, code int, msg string) *mcpResponse {
	return &mcpResponse{JSONRPC: "2.0", ID: mcpID(id), Error: &mcpError{Code: code, Message: msg}}
}

// mcpID keeps the request id as sent (a string, number or null) and uses null when there was none.
func mcpID(id json.RawMessage) json.RawMessage {
	if len(id) == 0 {
		return json.RawMessage("null")
	}
	return id
}

// registerMCPRoutes wires /mcp (bearer token, outside the session login) and its settings API (session login).
func (s *Server) registerMCPRoutes() {
	s.mux.HandleFunc("/mcp", s.handleMCP)
	s.mux.HandleFunc("/api/mcp", s.authMiddleware(s.handleMCPSettings))
	s.mux.HandleFunc("/api/mcp/token", s.authMiddleware(s.handleMCPToken))
}

// handleMCPSettings reports the state (GET) or enables and disables the server (POST {enabled}). The token is never
// returned here; only whether one exists.
func (s *Server) handleMCPSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		stored, _ := s.db.GetSetting(mcpSettingTokenH)
		writeJSON(w, http.StatusOK, map[string]any{
			"enabled":   s.mcpEnabled(),
			"forced":    s.mcpForced,
			"token_set": stored != "",
		})
	case http.MethodPost:
		var req struct {
			Enabled bool `json:"enabled"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		v := "0"
		if req.Enabled {
			v = "1"
		}
		if err := s.db.SetSetting(mcpSettingEnabled, v); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"enabled": s.mcpEnabled(), "forced": s.mcpForced})
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

// handleMCPToken generates or rotates the MCP token (POST). The new token is returned once, and the old one stops working.
func (s *Server) handleMCPToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	token, err := s.generateMCPToken()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	logger.Info("mcp", "", "MCP token generated")
	writeJSON(w, http.StatusOK, map[string]string{"token": token})
}
