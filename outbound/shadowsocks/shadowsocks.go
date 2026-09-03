// Package shadowsocks implements the Shadowsocks-2022 outbound.
//
// TODO(user): implement in Plan 6. It should implement core.Outbound.
// Stream: derive session key with 2022-blake3 KDF, AEAD encrypt/decrypt
// payload length + payload. Packet: prepend header (salt + encrypted
// address) and use PacketConn with the shared context.
package shadowsocks
