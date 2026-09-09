package core

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
)

// Addr expresses a destination as either a domain name or an IP plus port.
//
// It replaces the string-based target handling used by the old Session.
// A valid Addr is either domain-based (IsDomain) or IP-based (IsIP),
// never both, never neither (except the zero value, which IsZero reports).
type Addr struct {
	ip     netip.Addr
	domain string
	port   uint16
}

// AddrFromIPPort constructs an IP destination.
func AddrFromIPPort(ip netip.Addr, port uint16) Addr {
	return Addr{ip: ip, port: port}
}

// AddrFromDomain constructs a domain destination.
func AddrFromDomain(domain string, port uint16) Addr {
	return Addr{domain: domain, port: port}
}

// ParseAddr parses a "host:port" string. Numeric IPs become IP-based
// addresses; everything else becomes domain-based.
func ParseAddr(s string) (Addr, error) {
	host, portStr, err := net.SplitHostPort(s)
	if err != nil {
		return Addr{}, fmt.Errorf("parse addr %q: %w", s, err)
	}
	if host == "" {
		return Addr{}, fmt.Errorf("parse addr %q: empty host", s)
	}
	p64, err := strconv.ParseUint(portStr, 10, 16)
	if err != nil {
		return Addr{}, fmt.Errorf("parse addr %q: bad port: %w", s, err)
	}
	if p64 == 0 {
		return Addr{}, errors.New("parse addr: port must be > 0")
	}
	port := uint16(p64)
	if ip, err := netip.ParseAddr(host); err == nil {
		return AddrFromIPPort(ip, port), nil
	}
	return AddrFromDomain(host, port), nil
}

func (a Addr) IsDomain() bool { return a.domain != "" }
func (a Addr) IsIP() bool     { return a.ip.IsValid() }
func (a Addr) IsZero() bool   { return a.domain == "" && !a.ip.IsValid() && a.port == 0 }
func (a Addr) IP() netip.Addr { return a.ip }
func (a Addr) Domain() string { return a.domain }
func (a Addr) Port() uint16   { return a.port }

// String returns "host:port" form. IPv6 addresses are bracketed.
func (a Addr) String() string {
	if a.IsDomain() {
		return net.JoinHostPort(a.domain, strconv.Itoa(int(a.port)))
	}
	if a.IsIP() {
		return netip.AddrPortFrom(a.ip, a.port).String()
	}
	return "<invalid>"
}
