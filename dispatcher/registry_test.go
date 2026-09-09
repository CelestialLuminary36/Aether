package dispatcher

import (
	"errors"
	"testing"

	"github.com/CelestialLuminary36/Aether/core"
)

func TestOutboundRegistry_New(t *testing.T) {
	t.Run("accepts empty registry", func(t *testing.T) {
		if _, err := NewOutboundRegistry(); err != nil {
			t.Fatalf("NewOutboundRegistry() error = %v, want nil", err)
		}
	})

	t.Run("rejects empty tag", func(t *testing.T) {
		_, err := NewOutboundRegistry(&stubOutbound{tag: "", networks: []core.Network{core.NetworkTCP}})
		if err == nil {
			t.Fatal("expected error for empty tag, got nil")
		}
	})

	t.Run("rejects duplicate tag", func(t *testing.T) {
		_, err := NewOutboundRegistry(
			&stubOutbound{tag: "proxy", networks: []core.Network{core.NetworkTCP}},
			&stubOutbound{tag: "proxy", networks: []core.Network{core.NetworkTCP}},
		)
		if err == nil {
			t.Fatal("expected error for duplicate tag, got nil")
		}
	})
}

func TestOutboundRegistry_Get(t *testing.T) {
	tcpOnly := &stubOutbound{tag: "proxy", networks: []core.Network{core.NetworkTCP}}
	reg, err := NewOutboundRegistry(tcpOnly)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("known tag and supported network", func(t *testing.T) {
		ob, err := reg.Get("proxy", core.NetworkTCP)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if ob != core.Outbound(tcpOnly) {
			t.Fatal("Get returned a different outbound instance")
		}
	})

	t.Run("unknown tag", func(t *testing.T) {
		_, err := reg.Get("nope", core.NetworkTCP)
		if !errors.Is(err, core.ErrUnknownOutbound) {
			t.Fatalf("Get error = %v, want core.ErrUnknownOutbound", err)
		}
	})

	t.Run("network not supported", func(t *testing.T) {
		_, err := reg.Get("proxy", core.NetworkUDP)
		if !errors.Is(err, core.ErrNetworkNotSupported) {
			t.Fatalf("Get error = %v, want core.ErrNetworkNotSupported", err)
		}
	})
}
