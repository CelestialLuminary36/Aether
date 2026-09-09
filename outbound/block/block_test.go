package block

import (
	"context"
	"errors"
	"testing"

	"github.com/CelestialLuminary36/Aether/core"
)

// Compile-time interface compliance check.
var _ core.Outbound = New("block")

func TestBlock_Capabilities(t *testing.T) {
	b := New("my-block")

	if got := b.Tag(); got != "my-block" {
		t.Errorf("Tag() = %q, want %q", got, "my-block")
	}
	if got := b.Type(); got != "block" {
		t.Errorf("Type() = %q, want %q", got, "block")
	}

	networks := b.Networks()
	if len(networks) != 2 || networks[0] != core.NetworkTCP || networks[1] != core.NetworkUDP {
		t.Errorf("Networks() = %v, want [tcp udp] (block supports every network)", networks)
	}
}

// TestBlock_RejectsEveryDial verifies block refuses regardless of the
// metadata it is given, including the zero value.
func TestBlock_RejectsEveryDial(t *testing.T) {
	b := New("block")
	ctx := context.Background()

	mds := []*core.Metadata{
		nil,
		{},
		{Network: core.NetworkTCP, Destination: core.AddrFromDomain("example.com", 443)},
		{Network: core.NetworkUDP, Destination: core.AddrFromDomain("example.com", 53)},
	}

	for i, md := range mds {
		if conn, err := b.DialStream(ctx, md); err == nil {
			conn.Close()
			t.Errorf("case %d: DialStream succeeded, want ErrBlockedByRule", i)
		} else if !errors.Is(err, core.ErrBlockedByRule) {
			t.Errorf("case %d: DialStream error = %v, want core.ErrBlockedByRule", i, err)
		}

		if pc, err := b.DialPacket(ctx, md); err == nil {
			pc.Close()
			t.Errorf("case %d: DialPacket succeeded, want ErrBlockedByRule", i)
		} else if !errors.Is(err, core.ErrBlockedByRule) {
			t.Errorf("case %d: DialPacket error = %v, want core.ErrBlockedByRule", i, err)
		}
	}
}
