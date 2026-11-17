package netdiag

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"time"
)

// streamer is the part of a command runner a probe needs: both the SSH runner and the local one satisfy it.
type streamer interface {
	Stream(cmd string, out io.Writer) error
	Close() error
}

// localRunner runs probe commands on the machine (or container) Pantau itself runs on: the "Pantau" source host.
// Close kills the running command, like closing an SSH runner does.
type localRunner struct {
	ctx    context.Context
	cancel context.CancelFunc
}

func newLocalRunner() (*localRunner, error) {
	if _, err := exec.LookPath("sh"); err != nil {
		return nil, errors.New("probing from Pantau itself needs a POSIX sh, which this system does not have (Windows is not supported)")
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &localRunner{ctx: ctx, cancel: cancel}, nil
}

func (l *localRunner) Stream(cmd string, out io.Writer) error {
	c := exec.CommandContext(l.ctx, "sh", "-c", cmd)
	c.Stdout, c.Stderr = out, out
	// A grandchild (ping, traceroute) that outlives the killed shell must not keep the output pipe open forever.
	c.WaitDelay = 2 * time.Second
	return c.Run()
}

func (l *localRunner) Close() error {
	l.cancel()
	return nil
}
