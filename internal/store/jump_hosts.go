package store

import (
	"errors"
	"fmt"
)

// MaxJumpHops bounds how many bastion hosts may sit between Pantau and a Host. Longer chains are refused.
const MaxJumpHops = 4

// ErrJumpHostInvalid marks a jump host that would make the chain loop, point at itself, exceed MaxJumpHops or
// refer to a Host that does not exist. API handlers map it to HTTP 400.
var ErrJumpHostInvalid = errors.New("invalid jump host")

// ValidateJumpHost checks that jumpID can serve as the bastion of hostID (0 for a Host not stored yet).
// It rejects self-jumps, loops of any length and chains longer than MaxJumpHops.
func (d *DB) ValidateJumpHost(hostID, jumpID int64) error {
	if jumpID == 0 {
		return nil
	}
	if hostID != 0 && jumpID == hostID {
		return fmt.Errorf("%w: a host cannot jump through itself", ErrJumpHostInvalid)
	}
	_, err := d.walkJumpChain(hostID, jumpID)
	return err
}

// JumpChain returns the bastions of h in dial order: the first entry is reached directly, each later one through
// the previous. It is empty for a Host without a bastion. Stored data is re-checked, so a corrupt chain fails here.
func (d *DB) JumpChain(h *Host) ([]*Host, error) {
	if h == nil || h.JumpHostID == 0 {
		return nil, nil
	}
	chain, err := d.walkJumpChain(h.ID, h.JumpHostID)
	if err != nil {
		return nil, err
	}
	// walkJumpChain walks from the nearest bastion outwards; the dial order is the reverse.
	for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
		chain[i], chain[j] = chain[j], chain[i]
	}
	return chain, nil
}

// walkJumpChain follows the bastion references from jumpID, nearest first, and fails on a loop back to hostID,
// a repeated Host, a missing Host or a chain over MaxJumpHops.
func (d *DB) walkJumpChain(hostID, jumpID int64) ([]*Host, error) {
	seen := map[int64]bool{}
	if hostID != 0 {
		seen[hostID] = true
	}
	var chain []*Host
	for cur := jumpID; cur != 0; {
		if seen[cur] {
			return nil, fmt.Errorf("%w: the bastion chain loops back on host %d", ErrJumpHostInvalid, cur)
		}
		if len(chain) >= MaxJumpHops {
			return nil, fmt.Errorf("%w: more than %d bastion hosts in the chain", ErrJumpHostInvalid, MaxJumpHops)
		}
		seen[cur] = true
		next, err := d.GetHost(cur)
		if err != nil {
			return nil, err
		}
		if next == nil {
			return nil, fmt.Errorf("%w: bastion host %d does not exist", ErrJumpHostInvalid, cur)
		}
		chain = append(chain, next)
		cur = next.JumpHostID
	}
	return chain, nil
}
