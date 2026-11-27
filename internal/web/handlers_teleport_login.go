package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"pantau/internal/logger"
	"pantau/internal/teleport"
)

const (
	// teleportWSReadLimit bounds one client frame. Keystrokes are small; a larger frame is refused by the library.
	teleportWSReadLimit = 64 * 1024
	teleportWSWait      = 10 * time.Second
	// teleportExitGrace is how long the terminal output may still drain after tsh exits.
	teleportExitGrace = 2 * time.Second
)

// teleportWSConn serializes writes: the PTY pump and the exit event both write to the socket.
type teleportWSConn struct {
	ws *websocket.Conn
	mu sync.Mutex
}

func (c *teleportWSConn) write(messageType int, data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	_ = c.ws.SetWriteDeadline(time.Now().Add(teleportWSWait))
	return c.ws.WriteMessage(messageType, data)
}

func (c *teleportWSConn) writeJSON(v interface{}) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return c.write(websocket.TextMessage, b)
}

// handleWSTeleportLogin serves GET /api/teleport/profiles/{id}/login/ws, an interactive `tsh login` in a PTY.
//
// Protocol. The client sends keystrokes as binary frames, and control messages as text frames. The only control
// message is {"type":"resize","cols":N,"rows":N}. The client never sends a command or argument: tsh's argv is built
// from the stored profile. The server sends terminal output as binary frames, and one final text frame
// {"type":"exit","code":N,"success":bool} when tsh exits.
//
// The terminal stream is neither logged nor stored. Closing the socket kills the whole tsh process group.
func (s *Server) handleWSTeleportLogin(w http.ResponseWriter, r *http.Request, profileID int64) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	mgr := s.teleportManager(w)
	if mgr == nil {
		return
	}
	p, err := s.db.GetTeleportProfile(profileID)
	if err != nil {
		http.Error(w, "load profile failed", http.StatusInternalServerError)
		return
	}
	if p == nil {
		http.NotFound(w, r)
		return
	}
	cols, rows := teleportTermSize(r.URL.Query().Get("cols"), r.URL.Query().Get("rows"))

	// The Origin rule is the one the Web Terminal uses: a browser page from another origin cannot open this socket.
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	conn := &teleportWSConn{ws: ws}
	defer func() {
		_ = ws.Close()
		if s.teleportLoginDone != nil {
			s.teleportLoginDone(profileID)
		}
	}()
	ws.SetReadLimit(teleportWSReadLimit)

	session, err := mgr.StartLogin(teleportProfileOf(p), cols, rows)
	if err != nil {
		_ = conn.writeJSON(map[string]interface{}{"type": "error", "code": teleportLoginErrorCode(err), "error": err.Error()})
		return
	}
	logger.Info("teleport", p.Name, "tsh login started")

	// PTY output to the socket. The reader ends when the PTY reaches end of file, or when Close shuts it.
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		buf := make([]byte, 4096)
		for {
			n, rerr := session.Read(buf)
			if n > 0 && conn.write(websocket.BinaryMessage, buf[:n]) != nil {
				return
			}
			if rerr != nil {
				return
			}
		}
	}()

	// When tsh exits: let the output drain, send the result, and close the socket so the read loop below ends.
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		<-session.Done()
		select {
		case <-readerDone:
		case <-time.After(teleportExitGrace):
		}
		code, _ := session.Result()
		_ = conn.writeJSON(map[string]interface{}{"type": "exit", "code": code, "success": code == 0})
		logger.Info("teleport", p.Name, fmt.Sprintf("tsh login finished (exit %d)", code))
		_ = ws.Close()
	}()

readLoop:
	for {
		mt, msg, rerr := ws.ReadMessage()
		if rerr != nil {
			break readLoop
		}
		switch mt {
		case websocket.BinaryMessage:
			if _, werr := session.Write(msg); werr != nil {
				break readLoop
			}
		case websocket.TextMessage:
			if cols, rows, ok := parseTeleportResize(msg); ok {
				_ = session.Resize(cols, rows)
			}
		}
	}

	// Closing the session kills tsh if it still runs, waits for it to be reaped, and releases the profile lock.
	_ = session.Close()
	<-readerDone
	<-finished
}

// teleportTermSize reads the initial terminal size from the query. Anything out of range gets the default.
func teleportTermSize(colsStr, rowsStr string) (uint16, uint16) {
	cols, err := strconv.Atoi(colsStr)
	if err != nil || cols < 20 || cols > 500 {
		cols = 80
	}
	rows, err := strconv.Atoi(rowsStr)
	if err != nil || rows < 5 || rows > 200 {
		rows = 24
	}
	return uint16(cols), uint16(rows)
}

// parseTeleportResize reads a resize control frame. Sizes out of range are ignored.
func parseTeleportResize(msg []byte) (uint16, uint16, bool) {
	var m struct {
		Type string `json:"type"`
		Cols int    `json:"cols"`
		Rows int    `json:"rows"`
	}
	if json.Unmarshal(msg, &m) != nil || m.Type != "resize" {
		return 0, 0, false
	}
	if m.Cols < 20 || m.Cols > 500 || m.Rows < 5 || m.Rows > 200 {
		return 0, 0, false
	}
	return uint16(m.Cols), uint16(m.Rows), true
}

// teleportLoginErrorCode names a login start failure for the client.
func teleportLoginErrorCode(err error) string {
	switch {
	case errors.Is(err, teleport.ErrLoginInProgress):
		return "TELEPORT_LOGIN_IN_PROGRESS"
	case errors.Is(err, teleport.ErrPTYUnsupported):
		return "PTY_UNSUPPORTED"
	case errors.Is(err, teleport.ErrTSHNotInstalled):
		return "TSH_NOT_INSTALLED"
	case errors.Is(err, teleport.ErrInvalidInput):
		return "INVALID_INPUT"
	default:
		return "LOGIN_FAILED"
	}
}
