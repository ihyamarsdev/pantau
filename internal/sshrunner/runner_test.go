package sshrunner

import (
	"errors"
	"net"
	"os"
	"syscall"
	"testing"
	"time"

	"pantau/internal/vpnnet"
)

type fakeConn struct{ net.Conn }

func (fakeConn) Close() error { return nil }

// A mesh VPN answers "no route to host" at once while a peer connection is still forming, so over a
// tunnel that error must earn another attempt; outside a tunnel it means the Host really is unreachable.
func TestDialRetry_RouteError(t *testing.T) {
	noRoute := func() error {
		return &net.OpError{Op: "dial", Net: "tcp", Err: os.NewSyscallError("connect", syscall.EHOSTUNREACH)}
	}

	t.Run("tunnel retries then succeeds", func(t *testing.T) {
		calls := 0
		conn, err := dialRetry(func(time.Duration) (net.Conn, error) {
			if calls++; calls < 3 {
				return nil, noRoute()
			}
			return fakeConn{}, nil
		}, 4*time.Second, true)
		if err != nil || conn == nil {
			t.Fatalf("expected success on the third attempt, got conn=%v err=%v", conn, err)
		}
		if calls != 3 {
			t.Fatalf("expected 3 attempts, got %d", calls)
		}
	})

	t.Run("tunnel gives up after 4 attempts", func(t *testing.T) {
		calls := 0
		_, err := dialRetry(func(time.Duration) (net.Conn, error) {
			calls++
			return nil, noRoute()
		}, 4*time.Second, true)
		if !errors.Is(err, syscall.EHOSTUNREACH) {
			t.Fatalf("expected the route error, got %v", err)
		}
		if calls != 4 {
			t.Fatalf("expected 4 attempts, got %d", calls)
		}
	})

	t.Run("no tunnel fails at once", func(t *testing.T) {
		calls := 0
		_, err := dialRetry(func(time.Duration) (net.Conn, error) {
			calls++
			return nil, noRoute()
		}, time.Second, false)
		if !errors.Is(err, syscall.EHOSTUNREACH) || calls != 1 {
			t.Fatalf("expected one immediate attempt, got calls=%d err=%v", calls, err)
		}
	})

	t.Run("refused is never retried", func(t *testing.T) {
		calls := 0
		_, err := dialRetry(func(time.Duration) (net.Conn, error) {
			calls++
			return nil, syscall.ECONNREFUSED
		}, time.Second, true)
		if !errors.Is(err, syscall.ECONNREFUSED) || calls != 1 {
			t.Fatalf("expected one immediate refusal, got calls=%d err=%v", calls, err)
		}
	})
}

// A missing tunnel interface (the VPN went down mid-plan) is a path failure: the Host keeps its last
// state and is reported stale, not down.
func TestDialTCPMissingTunnelInterfaceIsNetworkUnavailable(t *testing.T) {
	_, err := dialTCP("10.0.0.1:22", &vpnnet.Path{Name: "gone", Iface: "pantau-no-such-iface0"}, 200*time.Millisecond)
	if !errors.Is(err, ErrNetworkUnavailable) {
		t.Fatalf("expected ErrNetworkUnavailable, got %v", err)
	}
}
