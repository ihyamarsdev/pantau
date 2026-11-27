package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"pantau/internal/teleport"
)

// teleportLoginDoneHook records each finished login WebSocket, so a test can wait for the handler to clean up.
func teleportLoginDoneHook(srv *Server) chan int64 {
	done := make(chan int64, 8)
	srv.teleportLoginDone = func(profileID int64) { done <- profileID }
	return done
}

func setupTeleportLogin(t *testing.T, tshBody string) (*Server, *httptest.Server, int64, func(*http.Request)) {
	t.Helper()
	srv, auth, _ := setupTeleportAPI(t, tshBody)
	id := createProfile(t, srv, auth)
	ts := httptest.NewServer(srv.mux)
	t.Cleanup(ts.Close)
	return srv, ts, id, auth
}

// dialTeleportLogin opens the login socket with the session cookie. Browsers send their Origin, which the shared
// upgrader checks against the Host. A Go client sends none, which the upgrader accepts.
func dialTeleportLogin(t *testing.T, ts *httptest.Server, auth func(*http.Request), id int64) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/teleport/profiles/" + strconv.FormatInt(id, 10) + "/login/ws?cols=100&rows=30"
	req, err := http.NewRequest(http.MethodGet, wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	auth(req)
	return websocket.DefaultDialer.Dial(wsURL, http.Header{"Cookie": req.Header["Cookie"]})
}

// readUntilTerminal reads binary frames until the terminal output contains want, failing the test on timeout.
func readUntilTerminal(t *testing.T, c *websocket.Conn, want string) string {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(10 * time.Second))
	var out strings.Builder
	for !strings.Contains(out.String(), want) {
		mt, msg, err := c.ReadMessage()
		if err != nil {
			t.Fatalf("socket ended before %q appeared: %v (output %q)", want, err, out.String())
		}
		if mt == websocket.BinaryMessage {
			out.Write(msg)
		}
	}
	return out.String()
}

// readControl reads frames until a text control message arrives, and returns it decoded.
func readControl(t *testing.T, c *websocket.Conn) map[string]interface{} {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(10 * time.Second))
	for {
		mt, msg, err := c.ReadMessage()
		if err != nil {
			t.Fatalf("no control message before the socket closed: %v", err)
		}
		if mt != websocket.TextMessage {
			continue
		}
		var m map[string]interface{}
		if err := json.Unmarshal(msg, &m); err != nil {
			t.Fatalf("control message is not JSON: %q", msg)
		}
		return m
	}
}

func waitHook(t *testing.T, done chan int64) int64 {
	t.Helper()
	select {
	case id := <-done:
		return id
	case <-time.After(10 * time.Second):
		t.Fatal("login socket handler did not finish")
	}
	return 0
}

func TestTeleportLoginWebSocketCompletesAndReportsExit(t *testing.T) {
	srv, ts, id, auth := setupTeleportLogin(t, `printf 'Enter password: '
read pw
if [ "$pw" = "letmein" ]; then printf 'logged in\n'; exit 0; fi
printf 'denied\n'
exit 1
`)
	done := teleportLoginDoneHook(srv)

	c, _, err := dialTeleportLogin(t, ts, auth, id)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()

	readUntilTerminal(t, c, "Enter password:")
	if err := c.WriteMessage(websocket.BinaryMessage, []byte("letmein\n")); err != nil {
		t.Fatal(err)
	}
	output := readUntilTerminal(t, c, "logged in")
	if strings.Contains(output, "denied") {
		t.Fatalf("login was denied: %q", output)
	}

	exit := readControl(t, c)
	if exit["type"] != "exit" || exit["success"] != true || exit["code"] != float64(0) {
		t.Fatalf("exit event = %v, want success exit 0", exit)
	}
	if got := waitHook(t, done); got != id {
		t.Fatalf("handler finished for profile %d, want %d", got, id)
	}
}

// Closing the socket mid-login must kill tsh and release the profile lock, so the next login can start.
func TestTeleportLoginWebSocketDisconnectKillsTSH(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pid")
	srv, ts, id, auth := setupTeleportLogin(t, "echo $$ > '"+pidFile+"'\nprintf 'waiting\\n'\nexec sleep 600\n")
	done := teleportLoginDoneHook(srv)

	c, _, err := dialTeleportLogin(t, ts, auth, id)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	readUntilTerminal(t, c, "waiting")
	raw, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatal(err)
	}

	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	waitHook(t, done)

	if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("tsh pid %d still alive after the socket closed (kill 0 = %v)", pid, err)
	}
	p, err := srv.db.GetTeleportProfile(id)
	if err != nil || p == nil {
		t.Fatalf("profile: %v", err)
	}
	again, err := srv.teleportMgr.StartLogin(teleportProfileOf(p), 80, 24)
	if err != nil {
		t.Fatalf("profile lock still held after disconnect: %v", err)
	}
	if err := again.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestTeleportLoginWebSocketRefusesSecondLogin(t *testing.T) {
	srv, ts, id, auth := setupTeleportLogin(t, "printf 'waiting\\n'\nexec sleep 600\n")
	done := teleportLoginDoneHook(srv)

	first, _, err := dialTeleportLogin(t, ts, auth, id)
	if err != nil {
		t.Fatalf("first dial: %v", err)
	}
	readUntilTerminal(t, first, "waiting")

	second, _, err := dialTeleportLogin(t, ts, auth, id)
	if err != nil {
		t.Fatalf("second dial: %v", err)
	}
	defer second.Close()
	if ev := readControl(t, second); ev["type"] != "error" || ev["code"] != "TELEPORT_LOGIN_IN_PROGRESS" {
		t.Fatalf("second login event = %v, want TELEPORT_LOGIN_IN_PROGRESS", ev)
	}

	first.Close()
	waitHook(t, done)
}

// Text frames are control messages only. A text frame that is not a resize is ignored, so it never reaches tsh as a
// keystroke.
func TestTeleportLoginWebSocketIgnoresTextInputAndAcceptsResize(t *testing.T) {
	srv, ts, id, auth := setupTeleportLogin(t, "read pw\nprintf 'got:%s\\n' \"$pw\"\nexit 0\n")
	done := teleportLoginDoneHook(srv)

	c, _, err := dialTeleportLogin(t, ts, auth, id)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()
	if err := c.WriteMessage(websocket.TextMessage, []byte(`{"type":"resize","cols":120,"rows":40}`)); err != nil {
		t.Fatal(err)
	}
	if err := c.WriteMessage(websocket.TextMessage, []byte("ignored")); err != nil {
		t.Fatal(err)
	}
	if err := c.WriteMessage(websocket.BinaryMessage, []byte("real\n")); err != nil {
		t.Fatal(err)
	}
	output := readUntilTerminal(t, c, "got:real")
	if strings.Contains(output, "got:ignored") {
		t.Fatalf("a text frame reached tsh as input: %q", output)
	}
	readControl(t, c)
	waitHook(t, done)
}

func TestTeleportLoginWebSocketReportsMissingTSH(t *testing.T) {
	srv, ts, id, auth := setupTeleportLogin(t, "exit 0\n")
	done := teleportLoginDoneHook(srv)
	m, err := teleport.NewManager(t.TempDir(), filepath.Join(t.TempDir(), "absent-tsh"), nil)
	if err != nil {
		t.Fatal(err)
	}
	srv.teleportMgr = m

	c, _, err := dialTeleportLogin(t, ts, auth, id)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()
	if ev := readControl(t, c); ev["code"] != "TSH_NOT_INSTALLED" {
		t.Fatalf("event = %v, want TSH_NOT_INSTALLED", ev)
	}
	waitHook(t, done)
}

func TestTeleportLoginRejectsUnknownProfileBeforeUpgrade(t *testing.T) {
	srv, auth, _ := setupTeleportAPI(t, "exit 0\n")
	rec := teleportCall(t, srv, auth, http.MethodGet, "/api/teleport/profiles/999/login/ws", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown profile: status = %d, want 404", rec.Code)
	}
}
