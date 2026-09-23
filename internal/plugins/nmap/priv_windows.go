//go:build windows

package nmap

// hasRawSocketPrivilege is only meaningful on the Unix runtime; on Windows (dev builds)
// we assume the privileged path so the code compiles and can be unit-tested.
func hasRawSocketPrivilege() bool { return true }
