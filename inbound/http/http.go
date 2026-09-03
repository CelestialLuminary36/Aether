// Package http implements an HTTP CONNECT inbound server.
//
// TODO(user): implement in Plan 5. It should implement core.Inbound,
// parse "CONNECT host:port HTTP/1.1", build core.Metadata, and dispatch
// through core.Dispatcher. On success return "HTTP/1.1 200 Connection
// Established"; on failure map core errors to appropriate HTTP status
// codes (e.g. ErrBlockedByRule → 403, connection errors → 502).
package http
