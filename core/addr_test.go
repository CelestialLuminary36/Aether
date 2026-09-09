package core

import (
	"net/netip"
	"testing"
)

func TestAddr_IPv4(t *testing.T) {
	ip := netip.MustParseAddr("192.0.2.1")
	a := AddrFromIPPort(ip, 80)

	if !a.IsIP() {
		t.Fatal("IsIP() = false, want true")
	}
	if a.IsDomain() {
		t.Fatal("IsDomain() = true, want false")
	}
	if a.IsZero() {
		t.Fatal("IsZero() = true, want false")
	}
	if a.IP() != ip {
		t.Fatalf("IP() = %v, want %v", a.IP(), ip)
	}
	if a.Port() != 80 {
		t.Fatalf("Port() = %d, want 80", a.Port())
	}
	if got := a.String(); got != "192.0.2.1:80" {
		t.Fatalf("String() = %q, want %q", got, "192.0.2.1:80")
	}
}

func TestAddr_IPv6(t *testing.T) {
	ip := netip.MustParseAddr("2001:db8::1")
	a := AddrFromIPPort(ip, 443)

	if !a.IsIP() || a.IsDomain() {
		t.Fatal("IPv6 addr must be IP-based, not domain-based")
	}
	// IPv6 string form must be bracketed so host:port stays parseable.
	if got := a.String(); got != "[2001:db8::1]:443" {
		t.Fatalf("String() = %q, want %q", got, "[2001:db8::1]:443")
	}
}

func TestAddr_Domain(t *testing.T) {
	a := AddrFromDomain("example.com", 443)

	if !a.IsDomain() {
		t.Fatal("IsDomain() = false, want true")
	}
	if a.IsIP() {
		t.Fatal("IsIP() = true, want false")
	}
	if a.IsZero() {
		t.Fatal("IsZero() = true, want false")
	}
	if got := a.Domain(); got != "example.com" {
		t.Fatalf("Domain() = %q, want example.com", got)
	}
	if got := a.String(); got != "example.com:443" {
		t.Fatalf("String() = %q, want %q", got, "example.com:443")
	}
}

func TestAddr_ZeroValue(t *testing.T) {
	var a Addr

	if !a.IsZero() {
		t.Fatal("zero Addr: IsZero() = false, want true")
	}
	if a.IsDomain() || a.IsIP() {
		t.Fatal("zero Addr must be neither domain nor IP")
	}
	if a.Port() != 0 {
		t.Fatalf("Port() = %d, want 0", a.Port())
	}
	if got := a.String(); got != "<invalid>" {
		t.Fatalf("String() = %q, want <invalid>", got)
	}
}

func TestParseAddr(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    Addr
		wantErr bool
	}{
		{
			name: "IPv4",
			in:   "192.0.2.1:80",
			want: AddrFromIPPort(netip.MustParseAddr("192.0.2.1"), 80),
		},
		{
			name: "IPv6 bracketed",
			in:   "[2001:db8::1]:443",
			want: AddrFromIPPort(netip.MustParseAddr("2001:db8::1"), 443),
		},
		{
			name: "IPv4-in-IPv6",
			in:   "[::ffff:192.0.2.1]:80",
			want: AddrFromIPPort(netip.MustParseAddr("::ffff:192.0.2.1"), 80),
		},
		{
			name: "domain",
			in:   "example.com:443",
			want: AddrFromDomain("example.com", 443),
		},
		{
			name: "localhost is a domain, not an IP",
			in:   "localhost:8080",
			want: AddrFromDomain("localhost", 8080),
		},
		{
			name:    "missing port",
			in:      "example.com",
			wantErr: true,
		},
		{
			name:    "bare IPv6 without brackets",
			in:      "2001:db8::1",
			wantErr: true,
		},
		{
			name:    "empty host",
			in:      ":80",
			wantErr: true,
		},
		{
			name:    "port zero",
			in:      "example.com:0",
			wantErr: true,
		},
		{
			name:    "port not numeric",
			in:      "example.com:http",
			wantErr: true,
		},
		{
			name:    "port out of uint16 range",
			in:      "example.com:65536",
			wantErr: true,
		},
		{
			name:    "too many colons",
			in:      "example.com:80:90",
			wantErr: true,
		},
		{
			name: "max port",
			in:   "example.com:65535",
			want: AddrFromDomain("example.com", 65535),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseAddr(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseAddr(%q) = %v, want error", tt.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseAddr(%q) error: %v", tt.in, err)
			}
			if got != tt.want {
				t.Fatalf("ParseAddr(%q) = %+v, want %+v", tt.in, got, tt.want)
			}
		})
	}
}

// TestAddr_StringRoundTrip verifies that ParseAddr(a.String()) == a for
// every address kind, including the bracketed IPv6 form.
func TestAddr_StringRoundTrip(t *testing.T) {
	for _, a := range []Addr{
		AddrFromIPPort(netip.MustParseAddr("192.0.2.1"), 80),
		AddrFromIPPort(netip.MustParseAddr("2001:db8::1"), 443),
		AddrFromIPPort(netip.MustParseAddr("::ffff:192.0.2.1"), 8080),
		AddrFromDomain("example.com", 443),
		AddrFromDomain("localhost", 1),
	} {
		parsed, err := ParseAddr(a.String())
		if err != nil {
			t.Fatalf("ParseAddr(%q) error: %v", a.String(), err)
		}
		if parsed != a {
			t.Fatalf("round trip changed address: %q -> %+v, want %+v", a.String(), parsed, a)
		}
	}
}
