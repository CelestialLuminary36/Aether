// Package socks implements a minimal SOCKS5 inbound server.
package socks

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"strconv"

	"github.com/CelestialLuminary36/Aether/core"
	"github.com/CelestialLuminary36/Aether/inbound"
)

// Server is a SOCKS5 listener that accepts inbound connections and hands them
// off to the configured inbound.Handler.
type Server struct {
	listenAddr string
	listener   net.Listener
}

// New creates a SOCKS5 inbound server bound to the supplied address.
func New(addr string) inbound.Inbound {
	return &Server{listenAddr: addr}
}

// Name returns the protocol identifier for this inbound.
func (s *Server) Name() string {
	return "socks5"
}

// Start listens for TCP connections on the configured address. Accepted
// connections are processed concurrently. The server shuts down when ctx is
// canceled.
func (s *Server) Start(ctx context.Context, handler inbound.Handler) error {
	l, err := net.Listen("tcp", s.listenAddr)
	if err != nil {
		return err
	}
	s.listener = l

	// Close the listener when the application context is canceled.
	go func() {
		<-ctx.Done()
		_ = s.Close()
	}()

	// Accept loop.
	go func() {
		for {
			conn, err := s.listener.Accept()
			if err != nil {
				if errors.Is(err, net.ErrClosed) {
					return
				}
				continue
			}
			go s.handleConn(ctx, conn, handler)
		}
	}()

	return nil
}

// Close stops the TCP listener.
func (s *Server) Close() error {
	if s.listener != nil {
		return s.listener.Close()
	}
	return nil
}

// handleConn performs the SOCKS5 handshake and then invokes handler.
// It closes conn on handshake failure.
func (s *Server) handleConn(ctx context.Context, conn net.Conn, handler inbound.Handler) {
	handshakeSuccess := false
	defer func() {
		if !handshakeSuccess {
			conn.Close()
		}
	}()

	buf := make([]byte, 260)

	// Read and acknowledge the authentication negotiation.
	if _, err := io.ReadFull(conn, buf[:2]); err != nil {
		return
	}
	if _, err := conn.Write([]byte{0x05, 0x00}); err != nil {
		return
	}

	// Read the connection request header: version, command, reserved byte, and address type.
	if _, err := io.ReadFull(conn, buf[:4]); err != nil {
		return
	}
	if buf[0] != 0x05 || buf[1] != 0x01 {
		return
	}

	var host string
	addrType := buf[3]

	// Resolve the destination address based on the SOCKS5 address type.
	switch addrType {
	case 0x01: // IPv4
		if _, err := io.ReadFull(conn, buf[:4]); err != nil {
			return
		}
		host = net.IP(buf[:4]).String()
	case 0x03: // Domain name
		if _, err := io.ReadFull(conn, buf[:1]); err != nil {
			return
		}
		domainLen := int(buf[0])
		if _, err := io.ReadFull(conn, buf[:domainLen]); err != nil {
			return
		}
		host = string(buf[:domainLen])
	case 0x04: // IPv6
		if _, err := io.ReadFull(conn, buf[:16]); err != nil {
			return
		}
		host = net.IP(buf[:16]).String()
	default:
		return
	}

	// Read the destination port.
	if _, err := io.ReadFull(conn, buf[:2]); err != nil {
		return
	}
	port := binary.BigEndian.Uint16(buf[:2])

	targetAddr := net.JoinHostPort(host, strconv.Itoa(int(port)))

	// Respond with a SOCKS5 success reply using a placeholder bind address.
	if _, err := conn.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0}); err != nil {
		return
	}

	handshakeSuccess = true
	sess := core.NewSession(ctx, targetAddr, conn.RemoteAddr())

	handler(sess, conn)
}
