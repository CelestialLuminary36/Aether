package direct

import (
	"errors"
	"fmt"
	"net"
	"os"
	"syscall"
	"testing"

	"github.com/CelestialLuminary36/Aether/core"
)

// dialErr builds the error shape net.Dialer produces on dial failure:
// *net.OpError → *os.SyscallError → platform errno. The errno constants
// come from errno_unix.go / errno_windows.go, so these tests exercise the
// real per-platform values.
func dialErr(errno error) error {
	return &net.OpError{
		Op:   "dial",
		Net:  "tcp",
		Addr: &net.TCPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 443},
		Err:  &os.SyscallError{Syscall: "connect", Err: errno},
	}
}

func TestMapDialError(t *testing.T) {
	tests := []struct {
		name string
		in   error
		want error // nil means the error must pass through unchanged
	}{
		{
			name: "connection refused",
			in:   dialErr(errConnRefused),
			want: core.ErrConnectionRefused,
		},
		{
			name: "host unreachable",
			in:   dialErr(errHostUnreachable),
			want: core.ErrHostUnreachable,
		},
		{
			name: "network unreachable",
			in:   dialErr(errNetUnreachable),
			want: core.ErrNetworkUnreachable,
		},
		{
			name: "unmapped errno passes through",
			in:   dialErr(syscall.EINVAL),
			want: nil,
		},
		{
			name: "non-syscall error passes through",
			in:   errors.New("dns lookup failed"),
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mapDialError(tt.in)
			if tt.want == nil {
				if got != tt.in {
					t.Fatalf("mapDialError(%v) = %v, want the original error unchanged", tt.in, got)
				}
				return
			}
			if got != tt.want {
				t.Fatalf("mapDialError(%v) = %v, want the %v sentinel", tt.in, got, tt.want)
			}
			// The sentinel must also answer errors.Is, which is how
			// inbounds (SOCKS5 REP mapping) consume it.
			if !errors.Is(got, tt.want) {
				t.Fatalf("errors.Is(%v, %v) = false", got, tt.want)
			}
		})
	}
}

// TestMapDialError_WrappedChain verifies that extra error wrapping does
// not hide the errno from errors.Is.
func TestMapDialError_WrappedChain(t *testing.T) {
	for _, tt := range []struct {
		name  string
		errno error
		want  error
	}{
		{"refused", errConnRefused, core.ErrConnectionRefused},
		{"host unreachable", errHostUnreachable, core.ErrHostUnreachable},
		{"network unreachable", errNetUnreachable, core.ErrNetworkUnreachable},
	} {
		t.Run(tt.name, func(t *testing.T) {
			wrapped := fmt.Errorf("dial tcp 192.0.2.1:443: connect: %w",
				fmt.Errorf("operation failed: %w", dialErr(tt.errno)))

			got := mapDialError(wrapped)
			if got != tt.want {
				t.Fatalf("mapDialError through %d wrap layers = %v, want %v", 2, got, tt.want)
			}
		})
	}
}
