package route

import (
	"net/netip"
	"testing"

	"github.com/CelestialLuminary36/Aether/core"
)

func mdDomain(domain string, port uint16) *core.Metadata {
	return &core.Metadata{
		Network:     core.NetworkTCP,
		Destination: core.AddrFromDomain(domain, port),
		InboundTag:  "in-socks",
	}
}

func mdIP(ip string, port uint16) *core.Metadata {
	return &core.Metadata{
		Network:     core.NetworkTCP,
		Destination: core.AddrFromIPPort(netip.MustParseAddr(ip), port),
		InboundTag:  "in-socks",
	}
}

func TestRouter_DomainMatch(t *testing.T) {
	r := New([]Rule{
		{Domain: []string{"example.com"}, Action: core.ActionDirect},
		{DomainSuffix: []string{".google.com"}, Action: core.ActionProxy, Outbound: "proxy"},
		{DomainKeyword: []string{"youtube"}, Action: core.ActionBlock},
	}, core.Decision{Action: core.ActionProxy, Outbound: "default-proxy"}, nil)

	cases := []struct {
		name string
		md   *core.Metadata
		want core.Decision
	}{
		{
			name: "exact domain direct",
			md:   mdDomain("example.com", 443),
			want: core.Decision{Action: core.ActionDirect},
		},
		{
			name: "suffix proxy",
			md:   mdDomain("www.google.com", 443),
			want: core.Decision{Action: core.ActionProxy, Outbound: "proxy"},
		},
		{
			name: "keyword block",
			md:   mdDomain("youtube.com", 443),
			want: core.Decision{Action: core.ActionBlock},
		},
		{
			name: "default",
			md:   mdDomain("unknown.com", 443),
			want: core.Decision{Action: core.ActionProxy, Outbound: "default-proxy"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := r.Route(c.md, core.StageInitial)
			if err != nil {
				t.Fatalf("Route: %v", err)
			}
			if got != c.want {
				t.Errorf("got %+v want %+v", got, c.want)
			}
		})
	}
}

func TestRouter_NetworkFilter(t *testing.T) {
	r := New([]Rule{
		{Network: []core.Network{core.NetworkUDP}, Action: core.ActionBlock},
	}, core.Decision{Action: core.ActionDirect}, nil)

	tcp := mdDomain("x.com", 443)
	tcp.Network = core.NetworkTCP

	udp := mdDomain("x.com", 443)
	udp.Network = core.NetworkUDP

	got, err := r.Route(tcp, core.StageInitial)
	if err != nil {
		t.Fatal(err)
	}
	if got.Action != core.ActionDirect {
		t.Errorf("tcp got %v want direct", got)
	}

	got, err = r.Route(udp, core.StageInitial)
	if err != nil {
		t.Fatal(err)
	}
	if got.Action != core.ActionBlock {
		t.Errorf("udp got %v want block", got)
	}
}

func TestRouter_PortFilter(t *testing.T) {
	r := New([]Rule{
		{Ports: []uint16{53}, Action: core.ActionDirect},
	}, core.Decision{Action: core.ActionProxy, Outbound: "p"}, nil)

	dns := mdDomain("x.com", 53)
	https := mdDomain("x.com", 443)

	got, _ := r.Route(dns, core.StageInitial)
	if got.Action != core.ActionDirect {
		t.Errorf("port 53 got %v want direct", got)
	}
	got, _ = r.Route(https, core.StageInitial)
	if got.Action != core.ActionProxy {
		t.Errorf("port 443 got %v want proxy", got)
	}
}

func TestRouter_ResolveTwoStage(t *testing.T) {
	// First pass: domain matches .internal and triggers resolve.
	// Second pass: resolved IP is in 10.0.0.0/8, so direct.
	r := New([]Rule{
		{DomainSuffix: []string{".internal"}, Action: core.ActionResolve},
		{CIDR: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}, Action: core.ActionDirect},
	}, core.Decision{Action: core.ActionProxy, Outbound: "p"}, nil)

	md := mdDomain("foo.internal", 443)

	// First pass should ask for resolution.
	got, err := r.Route(md, core.StageInitial)
	if err != nil {
		t.Fatal(err)
	}
	if got.Action != core.ActionResolve {
		t.Fatalf("first pass got %v want resolve", got)
	}

	// Simulate DNS resolution.
	md.ResolvedIPs = []netip.Addr{netip.MustParseAddr("10.1.2.3")}

	// Second pass should match CIDR and return direct.
	got, err = r.Route(md, core.StagePostResolve)
	if err != nil {
		t.Fatal(err)
	}
	if got.Action != core.ActionDirect {
		t.Errorf("second pass got %v want direct", got)
	}
}

func TestRouter_ResolveFallsBackToDefault(t *testing.T) {
	// Domain triggers resolve, but resolved IP does not match any CIDR rule.
	r := New([]Rule{
		{DomainSuffix: []string{".resolveme"}, Action: core.ActionResolve},
		{CIDR: []netip.Prefix{netip.MustParsePrefix("192.168.0.0/16")}, Action: core.ActionDirect},
	}, core.Decision{Action: core.ActionProxy, Outbound: "p"}, nil)

	md := mdDomain("x.resolveme", 443)
	md.ResolvedIPs = []netip.Addr{netip.MustParseAddr("1.2.3.4")}

	got, err := r.Route(md, core.StagePostResolve)
	if err != nil {
		t.Fatal(err)
	}
	if got.Action != core.ActionProxy || got.Outbound != "p" {
		t.Errorf("got %v want proxy/p", got)
	}
}

func TestRouter_SniffedDomainPreferred(t *testing.T) {
	// Destination is an IP, but SNI is present. Routing by SNI should work.
	r := New([]Rule{
		{DomainSuffix: []string{".netflix.com"}, Action: core.ActionProxy, Outbound: "us"},
	}, core.Decision{Action: core.ActionDirect}, nil)

	md := mdIP("1.2.3.4", 443)
	md.SniffedDomain = "www.netflix.com"

	got, err := r.Route(md, core.StageInitial)
	if err != nil {
		t.Fatal(err)
	}
	if got.Action != core.ActionProxy || got.Outbound != "us" {
		t.Errorf("got %v want proxy/us", got)
	}
}

func TestRouter_PrivateGeoIP(t *testing.T) {
	r := New([]Rule{
		{GeoIP: []string{"private"}, Action: core.ActionDirect},
	}, core.Decision{Action: core.ActionProxy, Outbound: "p"}, nil)

	md := mdIP("192.168.1.1", 443)
	md.ResolvedIPs = []netip.Addr{netip.MustParseAddr("192.168.1.1")}

	got, err := r.Route(md, core.StagePostResolve)
	if err != nil {
		t.Fatal(err)
	}
	if got.Action != core.ActionDirect {
		t.Errorf("got %v want direct", got)
	}
}
