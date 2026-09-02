# Aether

Aether is a lightweight TCP proxy written in Go. It exposes a SOCKS5 inbound listener and forwards accepted connections to their requested destinations through a direct (unproxied) outbound dialer.

## Features

- **SOCKS5 inbound** — minimal, no-authentication SOCKS5 handshake.
- **Direct outbound** — connects targets straight over TCP.
- **Bidirectional relay** — copies traffic between client and target using a reusable buffer pool.
- **Graceful shutdown** — listens for `SIGINT`/`SIGTERM` and stops cleanly.

## Project Structure

```
.
├── cmd/aether        # Application entry point
├── common/bufpool    # Reusable byte-buffer pool
├── core              # Session state and traffic relay
├── inbound           # Inbound listener interface
│   └── socks         # SOCKS5 server implementation
└── outbound          # Outbound dialer interface
    └── direct        # Direct TCP dialer
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

By default, Aether listens on `127.0.0.1:1080`. Configure your client to use this SOCKS5 proxy, then press `Ctrl+C` to stop.

## Example Output

```text
time=... level=INFO msg="Aether Starting..." version=1.27.1
time=... level=INFO msg="Aether listening on 127.0.0.1:1080, press Ctrl+C to exit"
```

## License

MIT
