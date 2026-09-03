// Package sniff implements non-destructive protocol sniffing.
//
// TODO(user): implement in Plan 2. Provide Sniffer implementations for:
//   - TLS ClientHello (extract SNI)
//   - HTTP (extract Host)
//   - DNS (identify DNS queries for hijacking)
//
// Use a CachedConn wrapper so the sniffed bytes are still available to
// the relay stage.
package sniff
