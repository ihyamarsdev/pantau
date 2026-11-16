//go:build darwin

package vpnnet

import (
	"fmt"
	"net"
	"syscall"

	"golang.org/x/sys/unix"
)

// bindControl pins a socket to one interface via IP_BOUND_IF/IPV6_BOUND_IF.
func bindControl(iface string) func(network, address string, c syscall.RawConn) error {
	return func(network, address string, c syscall.RawConn) error {
		ifi, err := net.InterfaceByName(iface)
		if err != nil {
			return fmt.Errorf("interface %s: %w", iface, err)
		}
		var serr error
		if err := c.Control(func(fd uintptr) {
			if err := unix.SetsockoptInt(int(fd), unix.IPPROTO_IP, unix.IP_BOUND_IF, ifi.Index); err != nil {
				serr = err
				return
			}
			serr = unix.SetsockoptInt(int(fd), unix.IPPROTO_IPV6, unix.IPV6_BOUND_IF, ifi.Index)
		}); err != nil {
			return err
		}
		if serr != nil {
			return fmt.Errorf("bind to interface %s: %w", iface, serr)
		}
		return nil
	}
}
