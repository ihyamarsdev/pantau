package teleport

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

const (
	logoutTimeout = 30 * time.Second
	// defaultCols and defaultRows size the PTY until the browser reports its real terminal size.
	defaultCols = 80
	defaultRows = 24
)

// ErrLoginInProgress means a tsh login for this profile is still running. Only one may run per profile.
var ErrLoginInProgress = errors.New("a login for this profile is already running")

// ErrPTYUnsupported means this build has no PTY support. Interactive login needs a Unix system.
var ErrPTYUnsupported = errors.New("interactive teleport login needs a Unix PTY, which this platform lacks")

// LoginArgs builds the argv of `tsh login` from a profile. The client sends no arguments: every value comes from
// the stored profile after ValidateProfile. --browser=none keeps tsh from trying to open a browser on the server.
func LoginArgs(p Profile) ([]string, error) {
	if err := ValidateProfile(p); err != nil {
		return nil, err
	}
	args := []string{"login", "--proxy=" + p.Proxy, "--browser=none"}
	if p.AuthConnector != "" {
		args = append(args, "--auth="+p.AuthConnector)
	}
	if p.Username != "" {
		args = append(args, "--user="+p.Username)
	}
	if p.TeleportCluster != "" {
		args = append(args, p.TeleportCluster)
	}
	return args, nil
}

// Logout runs `tsh logout` for a profile that has its own TELEPORT_HOME. Without one, tsh would log out every
// profile in the shared default home, so Logout skips and returns false. Pantau never ends sessions it does not own.
func (m *Manager) Logout(ctx context.Context, p Profile) (bool, error) {
	if err := ValidateProfile(p); err != nil {
		return false, err
	}
	if p.TeleportHome == "" {
		return false, nil
	}
	kubeconfig, err := KubeconfigPath(m.dataDir, p.ID)
	if err != nil {
		return false, err
	}
	res, err := m.run(ctx, logoutTimeout, p, kubeconfig, "logout")
	if err != nil {
		return false, err
	}
	if res.ExitCode != 0 {
		return false, failure("tsh logout", res)
	}
	return true, nil
}

// RemoveKubeconfig deletes the isolated kubeconfig of a profile. Its contexts hold client certificates, so removing
// the file ends Pantau's use of them. A missing file is not an error.
func (m *Manager) RemoveKubeconfig(profileID int64) error {
	path, err := KubeconfigPath(m.dataDir, profileID)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove isolated kubeconfig: %w", err)
	}
	return nil
}

// tryLockProfile takes the profile lock without waiting. It reports false when the lock is held.
func (m *Manager) tryLockProfile(id int64) (func(), bool) {
	m.mu.Lock()
	l, ok := m.locks[id]
	if !ok {
		l = &sync.Mutex{}
		m.locks[id] = l
	}
	m.mu.Unlock()
	if !l.TryLock() {
		return nil, false
	}
	return l.Unlock, true
}

// LoginSession is one interactive `tsh login` attached to a PTY. Read gives the terminal output, Write sends the
// user's keystrokes, and Resize follows the browser window. The session holds the profile lock until the process
// exits or Close runs. Nothing it carries is logged or stored.
type LoginSession struct {
	run       *ptyRun
	unlock    func()
	done      chan struct{}
	code      int
	waitErr   error
	closeOnce sync.Once
}

// StartLogin starts `tsh login` for a profile in a PTY of cols x rows. A zero size gets the default. The profile
// lock is held for the whole login, so a concurrent EnsureKubeContext waits for it instead of racing the file.
func (m *Manager) StartLogin(p Profile, cols, rows uint16) (*LoginSession, error) {
	args, err := LoginArgs(p)
	if err != nil {
		return nil, err
	}
	bin, err := m.tsh()
	if err != nil {
		return nil, err
	}
	kubeconfig, err := KubeconfigPath(m.dataDir, p.ID)
	if err != nil {
		return nil, err
	}
	if err := prepareKubeconfigDir(m.dataDir); err != nil {
		return nil, err
	}
	env, err := m.env(p, kubeconfig)
	if err != nil {
		return nil, err
	}
	env = append(env, "TERM=xterm-256color")
	if cols == 0 {
		cols = defaultCols
	}
	if rows == 0 {
		rows = defaultRows
	}

	unlock, ok := m.tryLockProfile(p.ID)
	if !ok {
		return nil, ErrLoginInProgress
	}
	run, err := startPTYRun(bin, args, env, cols, rows)
	if err != nil {
		unlock()
		return nil, fmt.Errorf("start tsh login: %w", err)
	}
	s := &LoginSession{run: run, unlock: unlock, done: make(chan struct{})}
	go func() {
		code, err := run.Wait()
		s.code, s.waitErr = code, err
		s.unlock()
		close(s.done)
	}()
	return s, nil
}

// Read returns terminal output. It returns an error once the process has exited and the PTY is drained.
func (s *LoginSession) Read(b []byte) (int, error) {
	return s.run.Read(b)
}

// Write sends keystrokes to tsh.
func (s *LoginSession) Write(b []byte) (int, error) {
	return s.run.Write(b)
}

// Resize sets the PTY window size.
func (s *LoginSession) Resize(cols, rows uint16) error {
	if cols == 0 || rows == 0 {
		return nil
	}
	return s.run.Resize(cols, rows)
}

// Done is closed once the process has exited and the profile lock is released.
func (s *LoginSession) Done() <-chan struct{} {
	return s.done
}

// Result returns the exit code. It is valid only after Done is closed.
func (s *LoginSession) Result() (int, error) {
	return s.code, s.waitErr
}

// Close stops the login: it kills the tsh process group, waits for the process to be reaped, and closes the PTY.
// The profile lock is released by then. Close is safe to call more than once.
func (s *LoginSession) Close() error {
	s.closeOnce.Do(func() {
		select {
		case <-s.done:
		default:
			s.run.Kill()
		}
		<-s.done
		_ = s.run.Close()
	})
	return nil
}

var _ io.ReadWriteCloser = (*LoginSession)(nil)
