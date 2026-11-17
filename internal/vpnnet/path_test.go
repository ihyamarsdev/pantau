package vpnnet

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"
)

// TestPathDialBindsToInterface proves the bound dial leaves through the named interface:
// the listener accepts a loopback connection that was pinned to "lo".
func TestPathDialBindsToInterface(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	accepted := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err == nil {
			_ = conn.Close()
		}
		accepted <- err
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	p := &Path{Name: "test", Iface: "lo"}
	conn, err := p.DialContext(ctx, "tcp", ln.Addr().String())
	if err != nil {
		if errors.Is(err, ErrBindUnsupported) || strings.Contains(err.Error(), "CAP_NET_RAW") || strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("binding needs CAP_NET_RAW on this machine: %v", err)
		}
		t.Fatalf("bound dial: %v", err)
	}
	_ = conn.Close()
	if err := <-accepted; err != nil {
		t.Fatalf("accept: %v", err)
	}
}

// TestPathDialFailsClosed proves a bad interface never silently falls back to the OS routing table.
func TestPathDialFailsClosed(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	p := &Path{Name: "missing", Iface: "pantau-no-such-iface"}
	if _, err := p.DialContext(ctx, "tcp", "127.0.0.1:1"); err == nil {
		t.Fatal("expected an error for an unknown interface, got a connection")
	}
}

// TestNilPathDialsDirectly keeps Hosts without a network on the historical dial path.
func TestNilPathDialsDirectly(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var p *Path
	conn, err := p.DialContext(ctx, "tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("direct dial: %v", err)
	}
	_ = conn.Close()
}

// A tunnel that went down leaves no interface: the dial must say so as a path failure (ErrInterfaceNotExists)
// instead of surfacing a bare ENODEV from the bind, which reached the UI as the Host being down.
func TestPathDialMissingInterface(t *testing.T) {
	p := &Path{Name: "gone", Iface: "pantau-no-such-iface0"}
	conn, err := p.DialContext(context.Background(), "tcp", "10.0.0.1:22")
	if conn != nil {
		conn.Close()
		t.Fatal("expected no connection over a missing interface")
	}
	if !errors.Is(err, ErrInterfaceNotExists) {
		t.Fatalf("expected ErrInterfaceNotExists, got %v", err)
	}
}
