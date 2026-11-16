//go:build !linux && !windows && !darwin

package vpnnet

import "syscall"

// bindControl is unsupported here: drivers still work, they just cannot pick an interface.
func bindControl(iface string) func(network, address string, c syscall.RawConn) error {
	return func(network, address string, c syscall.RawConn) error { return ErrBindUnsupported }
}
