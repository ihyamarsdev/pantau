package sshrunner

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"testing"

	"golang.org/x/crypto/ssh"
)

// startTestSSHServer serves an in-process SSH server on 127.0.0.1 that accepts publickey auth only for accepted.
// With forward set it also relays direct-tcpip channels, which makes it a bastion. It returns the listening port
// and a channel that receives one value each time a client connection ends, so tests can assert teardown.
func startTestSSHServer(t *testing.T, accepted ssh.PublicKey, forward bool) (int, <-chan struct{}) {
	t.Helper()
	_, hostPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	hostSigner, err := ssh.NewSignerFromKey(hostPriv)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &ssh.ServerConfig{
		PublicKeyCallback: func(_ ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			if bytes.Equal(key.Marshal(), accepted.Marshal()) {
				return nil, nil
			}
			return nil, fmt.Errorf("key rejected")
		},
	}
	cfg.AddHostKey(hostSigner)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ended := make(chan struct{}, 16)
	serveListener(t, ln, func(c net.Conn) {
		defer func() { ended <- struct{}{} }()
		sc, chans, reqs, err := ssh.NewServerConn(c, cfg)
		if err != nil {
			_ = c.Close()
			return
		}
		go ssh.DiscardRequests(reqs)
		var relays sync.WaitGroup
		for nc := range chans {
			switch {
			case nc.ChannelType() == "session":
				relays.Add(1)
				go func() {
					defer relays.Done()
					serveSession(nc)
				}()
			case forward && nc.ChannelType() == "direct-tcpip":
				relays.Add(1)
				go func() {
					defer relays.Done()
					relayChannel(nc)
				}()
			default:
				_ = nc.Reject(ssh.Prohibited, "channel type not served in test")
			}
		}
		relays.Wait()
		_ = sc.Close()
	})
	return ln.Addr().(*net.TCPAddr).Port, ended
}

// testForwardLine is what a test session's exec prints: the readiness line kubectl port-forward prints.
const testForwardLine = "Forwarding from 127.0.0.1:4242 -> 80\n"

// serveSession answers exec with testForwardLine and then holds the channel until the client closes it.
func serveSession(nc ssh.NewChannel) {
	ch, reqs, err := nc.Accept()
	if err != nil {
		return
	}
	var drain sync.WaitGroup
	for req := range reqs {
		if req.Type != "exec" {
			if req.WantReply {
				_ = req.Reply(false, nil)
			}
			continue
		}
		if req.WantReply {
			_ = req.Reply(true, nil)
		}
		_, _ = io.WriteString(ch, testForwardLine)
		drain.Add(1)
		go func() {
			defer drain.Done()
			_, _ = io.Copy(io.Discard, ch)
		}()
	}
	_ = ch.Close()
	drain.Wait()
}

// relayChannel connects a direct-tcpip channel to its destination and copies bytes both ways until one side ends.
func relayChannel(nc ssh.NewChannel) {
	var req struct {
		DestAddr string
		DestPort uint32
		OrigAddr string
		OrigPort uint32
	}
	if err := ssh.Unmarshal(nc.ExtraData(), &req); err != nil {
		_ = nc.Reject(ssh.ConnectionFailed, "bad direct-tcpip payload")
		return
	}
	up, err := net.Dial("tcp", net.JoinHostPort(req.DestAddr, strconv.Itoa(int(req.DestPort))))
	if err != nil {
		_ = nc.Reject(ssh.ConnectionFailed, err.Error())
		return
	}
	ch, reqs, err := nc.Accept()
	if err != nil {
		_ = up.Close()
		return
	}
	go ssh.DiscardRequests(reqs)
	var copies sync.WaitGroup
	copies.Add(2)
	go func() {
		defer copies.Done()
		_, _ = io.Copy(up, ch)
		_ = up.Close()
	}()
	go func() {
		defer copies.Done()
		_, _ = io.Copy(ch, up)
		_ = ch.Close()
	}()
	copies.Wait()
}
