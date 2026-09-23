//go:build !windows

package nmap

import "os"

// hasRawSocketPrivilege reports whether nmap can use raw sockets (SYN scan, OS detection),
// which requires effective root on Unix.
func hasRawSocketPrivilege() bool { return os.Geteuid() == 0 }
