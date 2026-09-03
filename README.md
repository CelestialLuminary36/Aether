# Aether

Aether is a next-generation proxy aiming to combine the maturity of xray
with the modernity of sing-box while fixing their respective pain points:
overgrown legacy code, breaking configuration changes, and confusing
mental models.

This repository is currently a skeleton. The core contracts and directory
structure are in place; the implementations follow in staged plans.

## Project Structure

```
.
├── cmd/aether           # Application entry point
├── common
│   ├── bufpool          # Reusable byte-buffer pool (for relay)
│   └── packet           # Head-reserved byte buffer (for UDP protocols)
├── core                 # Contracts: Addr, Metadata, interfaces, errors
├── dispatcher           # Traffic scheduler (StaticDispatcher in Plan 1)
├── config               # Configuration loading and validation (Plan 2)
├── dns                  # DNS subsystem (Plan 3)
├── route                # Routing engine (Plan 2)
├── sniff                # Non-destructive protocol sniffing (Plan 2)
├── stats                # Observability (Plan 10)
├── inbound
│   ├── socks            # SOCKS5 inbound
│   ├── http             # HTTP CONNECT inbound (Plan 5)
│   └── mixed            # SOCKS5 + HTTP on one port (Plan 5)
├── outbound
│   ├── direct           # Direct TCP dialer
│   ├── block            # Reject all dials
│   ├── socks            # SOCKS5 upstream client (Plan 7)
│   ├── http             # HTTP CONNECT upstream client (Plan 7)
│   ├── shadowsocks      # Shadowsocks-2022 outbound (Plan 6)
│   └── trojan           # Trojan outbound (Plan 7)
├── transport
│   ├── tcp              # Plain TCP transport (Plan 7)
│   └── ws               # WebSocket transport (Plan 7)
└── security
    ├── none             # No-op security layer (Plan 7)
    └── tls              # TLS security layer (Plan 7)
```

## Requirements

- Go 1.27.1 or later

## Build

```bash
go build -o aether ./cmd/aether
```

## Run

```bash
./aether
```

By default Aether listens on `127.0.0.1:1080` with username/password
authentication (`admin` / `123456`).

## Status

Skeleton phase: the project compiles and passes placeholder tests, but
SOCKS5 is stubbed. Follow `docs/superpowers/plans/` to implement each
plan.

## License

MIT
