//go:build windows

package vpnnet

import (
	"encoding/binary"
	"fmt"
	"net"
	"syscall"

	"golang.org/x/sys/windows"
)

const (
	ipUnicastIf   = 31 // IP_UNICAST_IF (not exported by x/sys/windows)
	ipv6UnicastIf = 41 // IPV6_UNICAST_IF
)

// bindControl pins a socket to one interface via IP_UNICAST_IF/IPV6_UNICAST_IF.
func bindControl(iface string) func(network, address string, c syscall.RawConn) error {
	return func(network, address string, c syscall.RawConn) error {
		ifi, err := net.InterfaceByName(iface)
		if err != nil {
			return fmt.Errorf("interface %s: %w", iface, err)
		}
		// IPv4 wants the index in network byte order; IPv6 wants host order.
		var v4 [4]byte
		binary.BigEndian.PutUint32(v4[:], uint32(ifi.Index))
		idx4 := int(binary.BigEndian.Uint32(v4[:]))
		var serr error
		if err := c.Control(func(fd uintptr) {
			if err := windows.SetsockoptInt(windows.Handle(fd), windows.IPPROTO_IP, ipUnicastIf, idx4); err != nil {
				serr = err
				return
			}
			serr = windows.SetsockoptInt(windows.Handle(fd), windows.IPPROTO_IPV6, ipv6UnicastIf, ifi.Index)
		}); err != nil {
			return err
		}
		if serr != nil {
			return fmt.Errorf("bind to interface %s (needs an elevated service): %w", iface, serr)
		}
		return nil
	}
}
