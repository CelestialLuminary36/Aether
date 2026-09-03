// Package core defines Aether's core contracts: types (Addr, Metadata,
// Decision) and interfaces (Dispatcher, Inbound, Outbound, Router,
// PacketConn), plus cross-protocol semantic errors.
//
// core is a leaf package: it only imports the standard library and
// common/*. It must not import any concrete implementation such as
// inbound, outbound, or dispatcher packages.
package core
