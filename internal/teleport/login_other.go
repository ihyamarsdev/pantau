//go:build !unix

package teleport

// ptyRun is a placeholder on platforms without a Unix PTY. Interactive login is unavailable there.
type ptyRun struct{}

func startPTYRun(string, []string, []string, uint16, uint16) (*ptyRun, error) {
	return nil, ErrPTYUnsupported
}

func (*ptyRun) Read([]byte) (int, error)    { return 0, ErrPTYUnsupported }
func (*ptyRun) Write([]byte) (int, error)   { return 0, ErrPTYUnsupported }
func (*ptyRun) Close() error                { return nil }
func (*ptyRun) Resize(uint16, uint16) error { return ErrPTYUnsupported }
func (*ptyRun) Wait() (int, error)          { return -1, ErrPTYUnsupported }
func (*ptyRun) Kill()                       {}
