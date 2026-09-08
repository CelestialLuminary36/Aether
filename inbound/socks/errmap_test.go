package socks

import (
	"errors"
	"fmt"
	"testing"

	"github.com/CelestialLuminary36/Aether/core"
)

func TestMapErrToRep(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want byte
	}{
		{"nil is succeeded", nil, 0x00},
		{"network unreachable", core.ErrNetworkUnreachable, 0x03},
		{"host unreachable", core.ErrHostUnreachable, 0x04},
		{"connection refused", core.ErrConnectionRefused, 0x05},
		{"blocked by rule", core.ErrBlockedByRule, 0x02},
		{"wrapped blocked", fmt.Errorf("dial primary: %w", core.ErrBlockedByRule), 0x02},
		{"unknown error", errors.New("boom"), 0x01},
		{"wrapped unknown", fmt.Errorf("wrap: %w", errors.New("boom")), 0x01},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := mapErrToRep(c.err)
			if got != c.want {
				t.Errorf("mapErrToRep(%v)=0x%02x want 0x%02x", c.err, got, c.want)
			}
		})
	}
}
