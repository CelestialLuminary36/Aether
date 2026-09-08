package socks

import (
	"errors"

	"github.com/CelestialLuminary36/Aether/core"
)

// mapErrToRep translates core semantic errors into SOCKS5 REP codes
// as defined in RFC 1928 §6.
//
// Unrecognized errors map to repGeneralFailure so the client receives a
// definite failure response instead of a dropped connection.
func mapErrToRep(err error) byte {
	switch {
	case err == nil:
		return repSucceeded
	case errors.Is(err, core.ErrNetworkUnreachable):
		return repNetworkUnreachable
	case errors.Is(err, core.ErrHostUnreachable):
		return repHostUnreachable
	case errors.Is(err, core.ErrConnectionRefused):
		return repConnectionRefused
	case errors.Is(err, core.ErrBlockedByRule):
		return repNotAllowedByRuleset
	default:
		return repGeneralFailure
	}
}
