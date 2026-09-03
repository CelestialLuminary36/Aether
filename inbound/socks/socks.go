// Package socks implements a SOCKS5 inbound server.
package socks

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/netip"
	"strconv"

	"github.com/CelestialLuminary36/Aether/core"
)

const (
	socksVersion    = 0x05
	authNone        = 0x00
	authPassword    = 0x02
	authNoAccept    = 0xFF
	userPassVersion = 0x01
	cmdConnect      = 0x01
	addrTypeIPv4    = 0x01
	addrTypeDomain  = 0x03
	addrTypeIPv6    = 0x04
	authSuccess     = 0x00
	authFailure     = 0x01

	repSucceeded           byte = 0x00
	repGeneralFailure      byte = 0x01
	repNotAllowedByRuleset byte = 0x02
	repNetworkUnreachable  byte = 0x03
	repHostUnreachable     byte = 0x04
	repConnectionRefused   byte = 0x05
)

// AuthMethod selects the SOCKS5 authentication method.
type AuthMethod uint8

const (
	AuthNone AuthMethod = iota
	AuthPassword
)

// Server is a SOCKS5 listener.
type Server struct {
	tag        string
	listenAddr string
	listener   net.Listener

	authMethod AuthMethod
	users      map[string]string

	dispatcher core.Dispatcher
}

// New creates a SOCKS5 inbound server. dispatcher must not be nil.
func New(tag, addr string, authMethod AuthMethod, users map[string]string, disp core.Dispatcher) *Server {
	if disp == nil {
		panic("socks: dispatcher must not be nil")
	}
	return &Server{
		tag:        tag,
		listenAddr: addr,
		authMethod: authMethod,
		users:      users,
		dispatcher: disp,
	}
}

func (s *Server) Tag() string  { return s.tag }
func (s *Server) Type() string { return "socks" }

// ListenAddr returns the actual listen address after Start. Useful for tests.
func (s *Server) ListenAddr() net.Addr {
	if s.listener == nil {
		return nil
	}
	return s.listener.Addr()
}

// Start listens for TCP connections and dispatches them through the
// configured Dispatcher.
func (s *Server) Start(ctx context.Context) error {
	l, err := net.Listen("tcp", s.listenAddr)
	if err != nil {
		slog.Error("Failed to start SOCKS5 listener", "addr", s.listenAddr, "err", err)
		return err
	}
	s.listener = l

	slog.Info("SOCKS5 listener started", "addr", s.listenAddr, "auth", s.authMethod)

	go func() {
		<-ctx.Done()
		slog.Debug("Shutting down SOCKS5 listener", "addr", s.listenAddr)
		if err := s.Close(); err != nil {
			slog.Warn("Failed to close SOCKS5 listener", "addr", s.listenAddr, "err", err)
		}
	}()

	go func() {
		for {
			conn, err := s.listener.Accept()
			if err != nil {
				if errors.Is(err, net.ErrClosed) {
					slog.Debug("SOCKS5 listener closed", "addr", s.listenAddr)
					return
				}
				slog.Warn("SOCKS5 accept failed", "addr", s.listenAddr, "err", err)
				continue
			}
			slog.Debug("SOCKS5 connection accepted", "remote", conn.RemoteAddr())
			go s.handleConn(ctx, conn)
		}
	}()

	return nil
}

// Close stops the TCP listener.
func (s *Server) Close() error {
	if s.listener == nil {
		return nil
	}
	return s.listener.Close()
}

// handleConn performs the SOCKS5 handshake and dispatches to the core.Dispatcher.
//
// TODO(user): this is the main integration point. Implement the full
// handshake (method negotiation, optional auth, CONNECT request parsing),
// build a core.Metadata, call s.dispatcher.DispatchStream with an
// onDialed callback that writes the SOCKS5 success reply, and translate
// dial errors into REP codes using mapErrToRep.
//
// See Plan 1 Task 10 for the complete implementation guide.
func (s *Server) handleConn(ctx context.Context, conn net.Conn) {
	defer func() { _ = conn.Close() }()

	// TODO(user): implement SOCKS5 handshake.
	// 1. Read method negotiation and select auth.
	// 2. Optionally perform username/password auth.
	// 3. Read CONNECT request: VER CMD RSV ATYP DST.ADDR DST.PORT.
	// 4. Build core.Metadata from remote address and target.
	// 5. Call s.dispatcher.DispatchStream(ctx, md, conn, onDialed).
	//    In onDialed, write repSucceeded.
	// 6. If dispatch returns an error before onDialed ran, write the
	//    mapped REP code and close.
	_ = writeSocks5Reply(conn, repGeneralFailure)
}

// selectAuthMethod chooses the server's required method if the client offered it.
func (s *Server) selectAuthMethod(methods []byte) (byte, bool) {
	var required byte
	switch s.authMethod {
	case AuthNone:
		required = authNone
	case AuthPassword:
		required = authPassword
	default:
		return authNoAccept, false
	}
	for _, m := range methods {
		if m == required {
			return required, true
		}
	}
	return authNoAccept, false
}

// authenticate performs SOCKS5 username/password sub-negotiation.
//
// TODO(user): this function is copied from the old implementation but is
// left here as a helper. Wire it into handleConn once you implement the
// handshake.
func (s *Server) authenticate(conn net.Conn) (string, error) {
	var buf [256]byte

	if _, err := io.ReadFull(conn, buf[:1]); err != nil {
		return "", fmt.Errorf("read auth version: %w", err)
	}
	if buf[0] != userPassVersion {
		return "", fmt.Errorf("unsupported username/password version: %d", buf[0])
	}

	if _, err := io.ReadFull(conn, buf[:1]); err != nil {
		return "", fmt.Errorf("read username length: %w", err)
	}
	usernameLen := int(buf[0])
	if usernameLen == 0 {
		_, _ = conn.Write([]byte{userPassVersion, authFailure})
		return "", errors.New("empty username")
	}
	if _, err := io.ReadFull(conn, buf[:usernameLen]); err != nil {
		return "", fmt.Errorf("read username: %w", err)
	}
	username := string(buf[:usernameLen])

	if _, err := io.ReadFull(conn, buf[:1]); err != nil {
		return "", fmt.Errorf("read password length: %w", err)
	}
	passwordLen := int(buf[0])
	if passwordLen == 0 {
		_, _ = conn.Write([]byte{userPassVersion, authFailure})
		return "", errors.New("empty password")
	}
	if _, err := io.ReadFull(conn, buf[:passwordLen]); err != nil {
		return "", fmt.Errorf("read password: %w", err)
	}
	password := string(buf[:passwordLen])

	expected, ok := s.users[username]
	if !ok || expected != password {
		slog.Debug("SOCKS5 invalid credentials", "username", username)
		_, _ = conn.Write([]byte{userPassVersion, authFailure})
		return "", errors.New("invalid username or password")
	}

	if _, err := conn.Write([]byte{userPassVersion, authSuccess}); err != nil {
		return "", fmt.Errorf("write authentication success: %w", err)
	}
	return username, nil
}

// mapErrToRep translates core semantic errors into SOCKS5 REP codes.
//
// TODO(user): move this to a separate errmap.go file and add unit tests
// (Plan 1 Task 9). Keep it here for now to reduce file count while
// scaffolding.
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

func writeSocks5Reply(conn net.Conn, rep byte) error {
	_, err := conn.Write([]byte{
		socksVersion,
		rep,
		0x00,
		addrTypeIPv4,
		0x00, 0x00, 0x00, 0x00,
		0x00, 0x00,
	})
	return err
}

// addrFromNet converts a net.Addr into a core.Addr.
//
// TODO(user): use this helper in handleConn when building Metadata.
func addrFromNet(a net.Addr) core.Addr {
	tcp, ok := a.(*net.TCPAddr)
	if !ok {
		return core.Addr{}
	}
	ip, ok := netip.AddrFromSlice(tcp.IP)
	if !ok {
		return core.Addr{}
	}
	return core.AddrFromIPPort(ip.Unmap(), uint16(tcp.Port))
}

// parseTargetPort is a small helper for parsing the 2-byte port.
//
// TODO(user): use binary.BigEndian in handleConn instead; this is just a
// reminder of the shape.
func parseTargetPort(b []byte) uint16 {
	return uint16(b[0])<<8 | uint16(b[1])
}

// stringToPort validates a port string for the target.
//
// TODO(user): remove once handleConn uses core.ParseAddr.
func stringToPort(s string) (uint16, error) {
	p, err := strconv.ParseUint(s, 10, 16)
	if err != nil {
		return 0, err
	}
	return uint16(p), nil
}
