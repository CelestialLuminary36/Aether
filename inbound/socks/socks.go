// Package socks implements a SOCKS5 inbound server.
package socks

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/netip"
	"slices"

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

	repSucceeded               byte = 0x00
	repGeneralFailure          byte = 0x01
	repNotAllowedByRuleset     byte = 0x02
	repNetworkUnreachable      byte = 0x03
	repHostUnreachable         byte = 0x04
	repConnectionRefused       byte = 0x05
	repCommandNotSupported     byte = 0x07
	repAddressTypeNotSupported byte = 0x08
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
// Ownership of conn: this function owns conn until onDialed succeeds. At
// that point ownership transfers to core.Relay (via the Dispatcher), which
// will close conn on return. If onDialed never succeeds, this function
// closes conn itself.
func (s *Server) handleConn(ctx context.Context, conn net.Conn) {
	connectionSuccess := false
	defer func() {
		if !connectionSuccess {
			_ = conn.Close()
		}
	}()

	buf := make([]byte, 512)
	// 1. Read method negotiation and select auth.
	nmethods, err := s.readMethodNegotiation(conn, buf)
	if err != nil {
		slog.Debug("SOCKS5 method negotiation failed", "remote", conn.RemoteAddr(), "err", err)
		return
	}
	selected, ok := s.selectAuthMethod(buf[:nmethods])
	if !ok {
		_, _ = conn.Write([]byte{socksVersion, authNoAccept})
		return
	}
	if _, err := conn.Write([]byte{socksVersion, selected}); err != nil {
		slog.Debug("SOCKS5 failed to write method selection", "remote", conn.RemoteAddr(), "err", err)
		return
	}
	// 2. Optionally perform username/password auth.
	var user string
	if selected == authPassword {
		user, err = s.authenticate(conn)
		if err != nil {
			slog.Debug("SOCKS5 authentication failed", "remote", conn.RemoteAddr(), "err", err)
			return
		}
	}
	// 3. Read CONNECT request: VER CMD RSV ATYP DST.ADDR DST.PORT.
	target, rep, err := s.readConnectRequest(conn, buf)
	if err != nil {
		slog.Debug("SOCKS5 failed to read CONNECT request", "remote", conn.RemoteAddr(), "err", err)
		_ = writeSocks5Reply(conn, rep)
		return
	}
	// 4. Build core.Metadata from remote address and target.
	md := &core.Metadata{
		Network:     core.NetworkTCP,
		Source:      addrFromNet(conn.RemoteAddr()),
		Destination: target,
		InboundTag:  s.tag,
		User:        user,
	}
	// 5. Call s.dispatcher.DispatchStream(ctx, md, conn, onDialed).
	//    In onDialed, write repSucceeded. If onDialed succeeds, ownership
	//    of conn transfers to the Dispatcher/Relay.
	onDialed := func() error {
		if err := writeSocks5Reply(conn, repSucceeded); err != nil {
			return err
		}
		connectionSuccess = true
		return nil
	}
	// 6. If dispatch returns an error before onDialed ran, write the
	//    mapped REP code and close.
	if err := s.dispatcher.DispatchStream(ctx, md, conn, onDialed); err != nil && !connectionSuccess {
		_ = writeSocks5Reply(conn, mapErrToRep(err))
	}
}

func (s *Server) readMethodNegotiation(conn net.Conn, buf []byte) (int, error) {
	// VER + METHODS
	if _, err := io.ReadFull(conn, buf[:2]); err != nil {
		return -1, err
	}
	if buf[0] != 0x05 {
		return -1, fmt.Errorf("unsupported SOCKS version %d", buf[0])
	}
	nmethods := int(buf[1])

	// METHODS
	if nmethods == 0 {
		return -1, fmt.Errorf("no authentication methods")
	}

	if _, err := io.ReadFull(conn, buf[:nmethods]); err != nil {
		return -1, err
	}

	return nmethods, nil
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
	return required, slices.Contains(methods, required)
}

func (s *Server) readConnectRequest(conn net.Conn, buf []byte) (core.Addr, byte, error) {
	// VER + CMD + RSV + ATYP
	if _, err := io.ReadFull(conn, buf[:4]); err != nil {
		return core.Addr{}, repGeneralFailure, err
	}
	if buf[0] != socksVersion {
		return core.Addr{}, repGeneralFailure, fmt.Errorf("unsupported SOCKS version %d", buf[0])
	}
	if buf[1] != cmdConnect {
		return core.Addr{}, repCommandNotSupported, fmt.Errorf("unsupported command %d", buf[1])
	}
	if buf[2] != 0x00 {
		return core.Addr{}, repGeneralFailure, fmt.Errorf("non-zero reserved byte")
	}

	addrType := buf[3]
	var host string
	switch addrType {
	case addrTypeIPv4:
		if _, err := io.ReadFull(conn, buf[:4]); err != nil {
			return core.Addr{}, repGeneralFailure, err
		}
		host = net.IP(buf[:4]).String()
	case addrTypeDomain:
		if _, err := io.ReadFull(conn, buf[:1]); err != nil {
			return core.Addr{}, repGeneralFailure, err
		}
		domainLen := int(buf[0])
		if domainLen == 0 {
			return core.Addr{}, repGeneralFailure, errors.New("empty domain")
		}
		if _, err := io.ReadFull(conn, buf[:domainLen]); err != nil {
			return core.Addr{}, repGeneralFailure, err
		}
		host = string(buf[:domainLen])
	case addrTypeIPv6:
		if _, err := io.ReadFull(conn, buf[:16]); err != nil {
			return core.Addr{}, repGeneralFailure, err
		}
		host = net.IP(buf[:16]).String()
	default:
		return core.Addr{}, repAddressTypeNotSupported, fmt.Errorf("unsupported address type %d", addrType)
	}

	// DST.PORT
	if _, err := io.ReadFull(conn, buf[:2]); err != nil {
		return core.Addr{}, repGeneralFailure, err
	}
	port := binary.BigEndian.Uint16(buf[:2])

	// Build core.Addr
	if addrType == addrTypeDomain {
		return core.AddrFromDomain(host, port), repSucceeded, nil
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return core.Addr{}, repGeneralFailure, err
	}
	return core.AddrFromIPPort(ip, port), repSucceeded, nil
}

// authenticate performs SOCKS5 username/password sub-negotiation.
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
