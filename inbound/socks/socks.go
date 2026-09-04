// Package socks implements a minimal SOCKS5 inbound server.
package socks

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"log/slog"
	"net"
	"strconv"

	"github.com/CelestialLuminary36/Aether/core"
	"github.com/CelestialLuminary36/Aether/inbound"
)

type AuthMethod uint8

const (
	AuthNone AuthMethod = iota
	AuthPassword
)

// Server is a SOCKS5 listener that accepts inbound connections and hands them
// off to the configured inbound.Handler.
type Server struct {
	listenAddr string
	listener   net.Listener
	users      map[string]string
	authMethod AuthMethod
}

// New creates a SOCKS5 inbound server bound to the supplied address.
func New(addr string, authMethod AuthMethod, users map[string]string) inbound.Inbound {
	return &Server{
		listenAddr: addr,
		authMethod: authMethod,
		users:      users,
	}
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
		slog.Error("Failed to start SOCKS5 listener", "addr", s.listenAddr, "err", err)
		return err
	}
	s.listener = l
	slog.Info("SOCKS5 listener started", "addr", s.listenAddr)

	// Close the listener when the application context is canceled.
	go func() {
		<-ctx.Done()
		slog.Debug("Shutting down SOCKS5 listener", "addr", s.listenAddr)
		_ = s.Close()
	}()

	// Accept loop.
	go func() {
		for {
			conn, err := s.listener.Accept()
			if err != nil {
				if errors.Is(err, net.ErrClosed) {
					slog.Debug("SOCKS5 listener closed", "addr", s.listenAddr)
					return
				}
				slog.Warn("SOCKS5 accept failed", "err", err)
				continue
			}
			slog.Info("SOCKS5 connection accepted", "remote", conn.RemoteAddr())
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
		slog.Warn("SOCKS5 auth negotiation read failed", "remote", conn.RemoteAddr(), "err", err)
		return
	}
	if buf[0] != 0x05 {
		slog.Warn("SOCKS5 unsupported version", "remote", conn.RemoteAddr(), "version", buf[0])
		return
	}
	nMethods := int(buf[1])
	if nMethods == 0 || nMethods > len(buf) {
		slog.Warn("SOCKS5 invalid method count", "remote", conn.RemoteAddr(), "nMethods", nMethods)
		return
	}
	if _, err := io.ReadFull(conn, buf[:nMethods]); err != nil {
		slog.Warn("SOCKS5 methods read failed", "remote", conn.RemoteAddr(), "nMethods", nMethods, "err", err)
		return
	}
	// Select no-authentication (0x00) if the client offers it.
	selected := byte(0xFF)
	for i := range nMethods {
		switch s.authMethod {
		case AuthNone:
			if buf[i] == 0x00 {
				selected = 0x00
			}
		case AuthPassword:
			if buf[i] == 0x02 {
				selected = 0x02
			}
		}
		if selected != 0xFF {
			break
		}
	}
	if selected == 0xFF {
		slog.Warn("SOCKS5 no acceptable auth method", "remote", conn.RemoteAddr(), "methods", buf[:nMethods])
		_, _ = conn.Write([]byte{0x05, 0xFF})
		return
	}

	// Tell the client which authentication method was selected.
	if _, err := conn.Write([]byte{0x05, selected}); err != nil {
		slog.Warn("SOCKS5 auth negotiation write failed", "remote", conn.RemoteAddr(), "err", err)
		return
	}

	if selected == 0x02 {
		if err := s.authenticate(conn); err != nil {
			slog.Warn("SOCKS5 authentication failed",
				"remote", conn.RemoteAddr(),
				"err", err,
			)
			return
		}
	}

	// Read the connection request header: version, command, reserved byte, and address type.
	if _, err := io.ReadFull(conn, buf[:4]); err != nil {
		slog.Warn("SOCKS5 request header read failed", "remote", conn.RemoteAddr(), "err", err)
		return
	}
	if buf[0] != 0x05 || buf[1] != 0x01 {
		slog.Warn("SOCKS5 unsupported request", "remote", conn.RemoteAddr(), "version", buf[0], "command", buf[1])
		return
	}

	var host string
	addrType := buf[3]

	// Resolve the destination address based on the SOCKS5 address type.
	switch addrType {
	case 0x01: // IPv4
		if _, err := io.ReadFull(conn, buf[:4]); err != nil {
			slog.Warn("SOCKS5 IPv4 read failed", "remote", conn.RemoteAddr(), "err", err)
			return
		}
		host = net.IP(buf[:4]).String()
	case 0x03: // Domain name
		if _, err := io.ReadFull(conn, buf[:1]); err != nil {
			slog.Warn("SOCKS5 domain length read failed", "remote", conn.RemoteAddr(), "err", err)
			return
		}
		domainLen := int(buf[0])
		if _, err := io.ReadFull(conn, buf[:domainLen]); err != nil {
			slog.Warn("SOCKS5 domain read failed", "remote", conn.RemoteAddr(), "len", domainLen, "err", err)
			return
		}
		host = string(buf[:domainLen])
	case 0x04: // IPv6
		if _, err := io.ReadFull(conn, buf[:16]); err != nil {
			slog.Warn("SOCKS5 IPv6 read failed", "remote", conn.RemoteAddr(), "err", err)
			return
		}
		host = net.IP(buf[:16]).String()
	default:
		slog.Warn("SOCKS5 unsupported address type", "remote", conn.RemoteAddr(), "addrType", addrType)
		return
	}

	// Read the destination port.
	if _, err := io.ReadFull(conn, buf[:2]); err != nil {
		slog.Warn("SOCKS5 port read failed", "remote", conn.RemoteAddr(), "err", err)
		return
	}
	port := binary.BigEndian.Uint16(buf[:2])

	targetAddr := net.JoinHostPort(host, strconv.Itoa(int(port)))
	slog.Debug("SOCKS5 target resolved", "remote", conn.RemoteAddr(), "target", targetAddr)

	// Respond with a SOCKS5 success reply using a placeholder bind address.
	if _, err := conn.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0}); err != nil {
		slog.Warn("SOCKS5 success reply write failed", "remote", conn.RemoteAddr(), "target", targetAddr, "err", err)
		return
	}

	handshakeSuccess = true
	slog.Debug("SOCKS5 handshake completed", "remote", conn.RemoteAddr(), "target", targetAddr)
	sess := core.NewSession(ctx, targetAddr, conn.RemoteAddr())

	handler(sess, conn)
}

func (s *Server) authenticate(conn net.Conn) error {
	var buf [256]byte

	// Authentication version
	if _, err := io.ReadFull(conn, buf[:1]); err != nil {
		return err
	}

	if buf[0] != 0x01 {
		return errors.New("unsupported username/password version")
	}

	// Username Length
	if _, err := io.ReadFull(conn, buf[:1]); err != nil {
		return err
	}

	usernameLen := int(buf[0])

	// Username
	if _, err := io.ReadFull(conn, buf[:usernameLen]); err != nil {
		return err
	}

	username := string(buf[:usernameLen])

	// Password length
	if _, err := io.ReadFull(conn, buf[:1]); err != nil {
		return err
	}

	passwordLen := int(buf[0])

	// Password
	if _, err := io.ReadFull(conn, buf[:passwordLen]); err != nil {
		return err
	}

	password := string(buf[:passwordLen])

	// Validate
	expectedPassword, ok := s.users[username]

	if !ok || expectedPassword != password {
		_, _ = conn.Write([]byte{0x01, 0x01})
		return errors.New("invalid username or password")
	}

	// Authentication success
	_, err := conn.Write([]byte{0x01, 0x00})
	return err
}
