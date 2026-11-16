//go:build linux

package vpnnet

import (
	"fmt"
	"syscall"

	"golang.org/x/sys/unix"
)

// bindControl pins a socket to one interface, so packets leave through that tunnel even when the
// main routing table prefers another VPN with the same subnet. Needs CAP_NET_RAW (or root).
func bindControl(iface string) func(network, address string, c syscall.RawConn) error {
	return func(network, address string, c syscall.RawConn) error {
		var serr error
		if err := c.Control(func(fd uintptr) {
			serr = unix.SetsockoptString(int(fd), unix.SOL_SOCKET, unix.SO_BINDTODEVICE, iface)
		}); err != nil {
			return err
		}
		if serr != nil {
			return fmt.Errorf("bind to interface %s (needs CAP_NET_RAW/root): %w", iface, serr)
		}
		return nil
	}
}
