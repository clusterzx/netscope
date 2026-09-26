//go:build linux

package main

import "syscall"

// statfs returns size and usage of a file system like df (size = used + available to
// unprivileged users).
func statfs(path string) (total, used uint64, ok bool) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil || st.Blocks == 0 {
		return 0, 0, false
	}
	bs := uint64(st.Frsize) //nolint:gosec,unconvert // int32 or int64 depending on the platform
	if bs == 0 {
		bs = uint64(st.Bsize) //nolint:gosec,unconvert // see above
	}
	used = (st.Blocks - st.Bfree) * bs
	return used + st.Bavail*bs, used, true
}
