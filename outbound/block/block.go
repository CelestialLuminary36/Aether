// Package block implements an outbound that rejects every dial with
// core.ErrBlockedByRule. It is the runtime target of route decisions
// whose Action is ActionBlock.
package block

import (
	"context"
	"net"

	"github.com/CelestialLuminary36/Aether/core"
)

// Block is an outbound that always rejects.
type Block struct {
	tag string
}

// New creates a new block outbound with the given tag.
func New(tag string) *Block {
	return &Block{tag: tag}
}

func (b *Block) Tag() string             { return b.tag }
func (b *Block) Type() string            { return "block" }
func (b *Block) Networks() []core.Network { return []core.Network{core.NetworkTCP, core.NetworkUDP} }

func (b *Block) DialStream(ctx context.Context, md *core.Metadata) (net.Conn, error) {
	return nil, core.ErrBlockedByRule
}

func (b *Block) DialPacket(ctx context.Context, md *core.Metadata) (core.PacketConn, error) {
	return nil, core.ErrBlockedByRule
}
