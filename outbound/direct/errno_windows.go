//go:build windows

package direct

import "golang.org/x/sys/windows"

// On Windows, syscall.ECONNREFUSED etc. are invented values that never
// appear in real socket errors; the runtime produces WSA* errnos instead.
var (
	errConnRefused     = windows.WSAECONNREFUSED // 10061
	errHostUnreachable = windows.WSAEHOSTUNREACH // 10065
	errNetUnreachable  = windows.WSAENETUNREACH  // 10051
)
