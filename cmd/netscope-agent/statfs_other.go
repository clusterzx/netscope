//go:build !linux

package main

// statfs is only available on Linux, the agent's platform.
func statfs(string) (total, used uint64, ok bool) { return 0, 0, false }
