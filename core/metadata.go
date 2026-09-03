package core

import "net/netip"

// Network identifies the transport type of a request.
type Network uint8

const (
	NetworkTCP Network = iota
	NetworkUDP
)

func (n Network) String() string {
	switch n {
	case NetworkTCP:
		return "tcp"
	case NetworkUDP:
		return "udp"
	default:
		return "unknown"
	}
}

// Metadata is the set of all known facts about a single proxied request.
// It is pure data, copyable, and behavior-free.
//
// Inbound implementations fill Network, Source, Destination, InboundTag,
// and User. The remaining fields are filled by the Dispatcher pipeline.
//
// TODO(user): add tests for Network/String, Action/String, and Metadata
// copyability. See Plan 1 Task 3.
type Metadata struct {
	Network Network

	Source      Addr
	Destination Addr

	InboundTag string
	User       string

	// Filled by the Dispatcher pipeline.

	SniffedProtocol string
	SniffedDomain   string
	ResolvedIPs     []netip.Addr
}

// Action is the decision produced by a Router.
type Action uint8

const (
	ActionProxy Action = iota
	ActionDirect
	ActionBlock
	ActionResolve
)

func (a Action) String() string {
	switch a {
	case ActionProxy:
		return "proxy"
	case ActionDirect:
		return "direct"
	case ActionBlock:
		return "block"
	case ActionResolve:
		return "resolve"
	default:
		return "unknown"
	}
}

// Decision is the output of Router.Route. Outbound is only meaningful
// when Action == ActionProxy.
type Decision struct {
	Action   Action
	Outbound string
}
