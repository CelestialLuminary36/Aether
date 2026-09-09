package dispatcher

import (
	"fmt"
	"slices"

	"github.com/CelestialLuminary36/Aether/core"
)

type OutboundRegistry struct {
	byTag map[string]core.Outbound
}

func NewOutboundRegistry(outbounds ...core.Outbound) (*OutboundRegistry, error) {
	m := make(map[string]core.Outbound, len(outbounds))
	for _, ob := range outbounds {
		tag := ob.Tag()
		if tag == "" {
			return nil, fmt.Errorf("outbound %T has empty tag", ob)
		}
		if _, dup := m[tag]; dup {
			return nil, fmt.Errorf("duplicate outbound tag %q", tag)
		}
		m[tag] = ob
	}
	return &OutboundRegistry{byTag: m}, nil
}

func (r *OutboundRegistry) Get(tag string, network core.Network) (core.Outbound, error) {
	ob, ok := r.byTag[tag]
	if !ok {
		return nil, fmt.Errorf("%w: %q", core.ErrUnknownOutbound, tag)
	}
	if !slices.Contains(ob.Networks(), network) {
		return nil, fmt.Errorf("%w: outbound %q does not support %s", core.ErrNetworkNotSupported, tag, network)
	}
	return ob, nil
}
