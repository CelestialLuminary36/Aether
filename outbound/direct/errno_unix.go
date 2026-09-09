//go:build !windows

package direct

import "syscall"

// Unix errnos as returned by net.OpError on non-Windows platforms.
var (
	errConnRefused     = syscall.ECONNREFUSED
	errHostUnreachable = syscall.EHOSTUNREACH
	errNetUnreachable  = syscall.ENETUNREACH
)
