package sshrunner

import (
	"bytes"
	"fmt"
	"io"
	"sync"

	"golang.org/x/crypto/ssh"
)

// Command is a long-running command on a Host. Its standard output is read while it runs, and Stop ends it.
type Command interface {
	Stdout() io.Reader
	// StderrTail returns the last part of the command's standard error.
	StderrTail() string
	// Stop asks the command to end and closes its session. It is safe to call more than once.
	Stop()
}

// CommandStarter is implemented by runners that can start a Command (the SSH runner, not the mock).
type CommandStarter interface {
	StartCommand(cmd string) (Command, error)
}

// maxCommandStderr bounds the stderr kept for error reports from a long-running command.
const maxCommandStderr = 4096

// remoteCommand is an SSH session that runs one command for as long as it is needed.
type remoteCommand struct {
	session  *ssh.Session
	stdout   io.Reader
	stopOnce sync.Once

	mu     sync.Mutex
	stderr bytes.Buffer
}

// StartCommand runs cmd on the Host in its own session and returns at once. Output is read through Stdout.
func (r *LiveSSHRunner) StartCommand(cmd string) (Command, error) {
	if r.client == nil {
		return nil, fmt.Errorf("ssh runner client is closed or not initialized")
	}
	session, err := r.client.NewSession()
	if err != nil {
		return nil, err
	}
	rc := &remoteCommand{session: session}
	stdout, err := session.StdoutPipe()
	if err != nil {
		_ = session.Close()
		return nil, err
	}
	rc.stdout = stdout
	session.Stderr = rc
	if err := session.Start(cmd); err != nil {
		_ = session.Close()
		return nil, err
	}
	return rc, nil
}

// Write keeps only the newest bytes of stderr, so a chatty command cannot grow memory.
func (c *remoteCommand) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stderr.Write(p)
	if extra := c.stderr.Len() - maxCommandStderr; extra > 0 {
		c.stderr.Next(extra)
	}
	return len(p), nil
}

func (c *remoteCommand) Stdout() io.Reader { return c.stdout }

func (c *remoteCommand) StderrTail() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stderr.String()
}

// Stop sends SIGTERM first so the remote shell can stop its children, then closes the session.
func (c *remoteCommand) Stop() {
	c.stopOnce.Do(func() {
		_ = c.session.Signal(ssh.SIGTERM)
		_ = c.session.Close()
	})
}
