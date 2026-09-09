package core

import (
	"net/netip"
	"testing"
)

func TestNetwork_String(t *testing.T) {
	tests := []struct {
		n    Network
		want string
	}{
		{NetworkTCP, "tcp"},
		{NetworkUDP, "udp"},
		{Network(2), "unknown"},
		{Network(255), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.n.String(); got != tt.want {
			t.Errorf("Network(%d).String() = %q, want %q", uint8(tt.n), got, tt.want)
		}
	}
}

func TestAction_String(t *testing.T) {
	tests := []struct {
		a    Action
		want string
	}{
		{ActionProxy, "proxy"},
		{ActionDirect, "direct"},
		{ActionBlock, "block"},
		{ActionResolve, "resolve"},
		{Action(4), "unknown"},
		{Action(255), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.a.String(); got != tt.want {
			t.Errorf("Action(%d).String() = %q, want %q", uint8(tt.a), got, tt.want)
		}
	}
}

func TestMetadata_ZeroValue(t *testing.T) {
	var md Metadata

	// The zero Network is TCP; the plan treats TCP as the default so a
	// hand-constructed Metadata for the only supported network needs no
	// explicit initialization.
	if md.Network != NetworkTCP {
		t.Errorf("zero Metadata Network = %v, want NetworkTCP", md.Network)
	}
	if !md.Source.IsZero() {
		t.Errorf("zero Metadata Source = %v, want zero Addr", md.Source)
	}
	if !md.Destination.IsZero() {
		t.Errorf("zero Metadata Destination = %v, want zero Addr", md.Destination)
	}
	if md.InboundTag != "" || md.User != "" {
		t.Errorf("zero Metadata has non-empty InboundTag/User: %q/%q", md.InboundTag, md.User)
	}
	if md.SniffedProtocol != "" || md.SniffedDomain != "" {
		t.Errorf("zero Metadata has sniff fields set: %q/%q", md.SniffedProtocol, md.SniffedDomain)
	}
	if md.ResolvedIPs != nil {
		t.Errorf("zero Metadata ResolvedIPs = %v, want nil", md.ResolvedIPs)
	}
}

func TestMetadata_CopySemantics(t *testing.T) {
	md := Metadata{
		Network:     NetworkTCP,
		Source:      AddrFromIPPort(netip.MustParseAddr("198.51.100.1"), 54321),
		Destination: AddrFromDomain("example.com", 443),
		InboundTag:  "socks-in",
		ResolvedIPs: []netip.Addr{netip.MustParseAddr("192.0.2.10")},
	}

	cp := md
	cp.Network = NetworkUDP
	cp.Destination = AddrFromDomain("other.example", 80)
	cp.InboundTag = "http-in"
	cp.SniffedDomain = "sniffed.example"

	if cp.Network != NetworkUDP ||
		cp.Destination.Domain() != "other.example" ||
		cp.InboundTag != "http-in" ||
		cp.SniffedDomain != "sniffed.example" {
		t.Fatalf("copy does not hold the mutated values: %+v", cp)
	}

	// A struct copy isolates every scalar and struct field.
	if md.Network != NetworkTCP {
		t.Errorf("original Network changed to %v", md.Network)
	}
	if md.Destination.Domain() != "example.com" || md.Destination.Port() != 443 {
		t.Errorf("original Destination changed to %v", md.Destination)
	}
	if md.InboundTag != "socks-in" {
		t.Errorf("original InboundTag changed to %q", md.InboundTag)
	}
	if md.SniffedDomain != "" {
		t.Errorf("original SniffedDomain changed to %q", md.SniffedDomain)
	}

	// The copied slice header still points at the original backing array
	// (shallow copy); callers must not mutate shared elements in place.
	if len(cp.ResolvedIPs) != 1 || cp.ResolvedIPs[0] != md.ResolvedIPs[0] {
		t.Errorf("copy lost ResolvedIPs: %v", cp.ResolvedIPs)
	}
}

func TestDecision_ZeroValue(t *testing.T) {
	var d Decision

	// The zero Action is ActionProxy with an empty outbound tag; the
	// dispatcher must treat an empty tag as an error, not a default.
	if d.Action != ActionProxy {
		t.Errorf("zero Decision Action = %v, want ActionProxy", d.Action)
	}
	if d.Outbound != "" {
		t.Errorf("zero Decision Outbound = %q, want empty", d.Outbound)
	}
}
