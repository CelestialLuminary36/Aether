package direct

import (
	"context"
	"errors"
	"io"
	"net"
	"net/netip"
	"testing"

	"github.com/CelestialLuminary36/Aether/core"
)

// Compile-time interface compliance checks.
var (
	_ core.Outbound = New("direct")
)

func TestDirect_Capabilities(t *testing.T) {
	d := New("my-direct")

	if got := d.Tag(); got != "my-direct" {
		t.Errorf("Tag() = %q, want %q", got, "my-direct")
	}
	if got := d.Type(); got != "direct" {
		t.Errorf("Type() = %q, want %q", got, "direct")
	}
	networks := d.Networks()
	if len(networks) != 1 || networks[0] != core.NetworkTCP {
		t.Errorf("Networks() = %v, want [tcp] only in this plan", networks)
	}
}

// TestDirect_DialStream_IP dials an IP destination and verifies traffic
// actually flows to the target.
func TestDirect_DialStream_IP(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	echoDone := make(chan struct{})
	defer func() { <-echoDone }()
	go func() {
		defer close(echoDone)
		c, err := listener.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		_, _ = io.Copy(c, c)
	}()

	ap := listener.Addr().(*net.TCPAddr).AddrPort()
	d := New("direct")
	md := &core.Metadata{
		Network:     core.NetworkTCP,
		Destination: core.AddrFromIPPort(netip.MustParseAddr(ap.Addr().String()), uint16(ap.Port())),
	}

	conn, err := d.DialStream(context.Background(), md)
	if err != nil {
		t.Fatalf("DialStream: %v", err)
	}
	defer conn.Close()

	if _, err := conn.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	conn.(interface{ CloseWrite() error }).CloseWrite()

	data, err := io.ReadAll(conn)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(data) != "ping" {
		t.Fatalf("echo = %q, want %q", data, "ping")
	}
}

// TestDirect_DialStream_Domain verifies domain destinations are passed
// through to the system resolver.
func TestDirect_DialStream_Domain(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	accepted := make(chan net.Conn, 1)
	go func() {
		c, err := listener.Accept()
		if err != nil {
			close(accepted)
			return
		}
		accepted <- c
	}()

	port := listener.Addr().(*net.TCPAddr).Port
	d := New("direct")
	md := &core.Metadata{
		Network:     core.NetworkTCP,
		Destination: core.AddrFromDomain("localhost", uint16(port)),
	}

	conn, err := d.DialStream(context.Background(), md)
	if err != nil {
		t.Fatalf("DialStream via domain: %v", err)
	}
	defer conn.Close()

	peer := <-accepted
	if peer == nil {
		t.Fatal("listener never saw the connection")
	}
	peer.Close()
}

// TestDirect_DialStream_Refused verifies a real connection refusal is
// mapped to the core.ErrConnectionRefused semantic error on every
// platform (ECONNREFUSED on Unix, WSAECONNREFUSED on Windows).
func TestDirect_DialStream_Refused(t *testing.T) {
	// Grab a port that is guaranteed closed.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	refusedPort := listener.Addr().(*net.TCPAddr).Port
	listener.Close()

	d := New("direct")
	md := &core.Metadata{
		Network:     core.NetworkTCP,
		Destination: core.AddrFromIPPort(netip.MustParseAddr("127.0.0.1"), uint16(refusedPort)),
	}

	conn, err := d.DialStream(context.Background(), md)
	if conn != nil {
		conn.Close()
		t.Fatal("DialStream returned a conn alongside an error")
	}
	if !errors.Is(err, core.ErrConnectionRefused) {
		t.Fatalf("DialStream error = %v, want core.ErrConnectionRefused", err)
	}
}

// TestDirect_DialStream_CanceledContext verifies ctx is honored.
func TestDirect_DialStream_CanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	d := New("direct")
	md := &core.Metadata{
		Network:     core.NetworkTCP,
		Destination: core.AddrFromDomain("example.com", 443),
	}

	if _, err := d.DialStream(ctx, md); !errors.Is(err, context.Canceled) {
		t.Fatalf("DialStream error = %v, want context.Canceled", err)
	}
}

func TestDirect_DialPacket_NotSupported(t *testing.T) {
	d := New("direct")
	md := &core.Metadata{Network: core.NetworkUDP}

	if _, err := d.DialPacket(context.Background(), md); !errors.Is(err, core.ErrNetworkNotSupported) {
		t.Fatalf("DialPacket error = %v, want core.ErrNetworkNotSupported", err)
	}
}
