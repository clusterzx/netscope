//go:build !linux

package tunnel

import "errors"

// errUnsupported is returned where WireGuard cannot be managed (non-Linux builds).
var errUnsupported = errors.New("WireGuard-Tunnel werden nur unter Linux unterstützt")

func newNetOps() (netOps, error) { return nil, errUnsupported }
