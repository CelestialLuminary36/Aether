package core

import "errors"

// Semantic errors produced by outbounds and consumed by inbounds, which
// translate them into protocol-specific error codes (e.g. SOCKS5 REP).
var (
	ErrNetworkUnreachable  = errors.New("network is unreachable")
	ErrHostUnreachable     = errors.New("host is unreachable")
	ErrConnectionRefused   = errors.New("connection refused")
	ErrBlockedByRule       = errors.New("blocked by rule")
	ErrNetworkNotSupported = errors.New("network not supported")
	ErrNoRouteMatched      = errors.New("no route matched")

	// ErrNeedMoreData is returned by Sniffers when more bytes are needed
	// to make a determination. Used in Plan 2.
	ErrNeedMoreData = errors.New("need more data")
)
