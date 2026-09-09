package socks_test

import (
	"context"
	"io"
	"net"
	"net/netip"
	"testing"
	"time"

	"github.com/CelestialLuminary36/Aether/dispatcher"
	"github.com/CelestialLuminary36/Aether/inbound/socks"
	"github.com/CelestialLuminary36/Aether/outbound/direct"
)

// TestSOCKS5_EndToEnd runs a full SOCKS5 CONNECT through the server,
// dispatcher, and direct outbound to a real TCP echo backend.
func TestSOCKS5_EndToEnd(t *testing.T) {
	// Backend echo server.
	backend, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()

	backendDone := make(chan struct{})
	defer func() { <-backendDone }()
	go func() {
		defer close(backendDone)
		c, err := backend.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		_, _ = io.Copy(c, c)
	}()

	// SOCKS5 server with direct outbound.
	out := direct.New("out")
	disp := dispatcher.NewStaticDispatcher(out, nil)
	server := socks.New("in", "127.0.0.1:0", socks.AuthNone, nil, disp)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := server.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer server.Close()

	// Wait for listener to be ready.
	time.Sleep(50 * time.Millisecond)
	socksAddr := server.ListenAddr().String()

	backendAP := backend.Addr().(*net.TCPAddr)
	client, err := net.Dial("tcp", socksAddr)
	if err != nil {
		t.Fatalf("Dial socks: %v", err)
	}
	defer client.Close()

	// Method negotiation: VER=5, NMETHODS=1, NO AUTH.
	if _, err := client.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 2)
	if _, err := io.ReadFull(client, buf); err != nil {
		t.Fatal(err)
	}
	if buf[0] != 0x05 || buf[1] != 0x00 {
		t.Fatalf("bad method reply: %x", buf)
	}

	// CONNECT request with IPv4 address type.
	req := []byte{0x05, 0x01, 0x00, 0x01}
	ip4 := netip.MustParseAddr(backendAP.IP.String()).As4()
	req = append(req, ip4[:]...)
	req = append(req, byte(backendAP.Port>>8), byte(backendAP.Port&0xff))
	if _, err := client.Write(req); err != nil {
		t.Fatal(err)
	}

	// Reply: VER, REP, RSV, ATYP, BND.ADDR(4), BND.PORT(2)
	reply := make([]byte, 10)
	if _, err := io.ReadFull(client, reply); err != nil {
		t.Fatal(err)
	}
	if reply[1] != 0x00 {
		t.Fatalf("CONNECT REP=0x%02x want 0x00", reply[1])
	}

	// Send data through the tunnel and read it back from the echo server.
	if _, err := client.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	client.(interface{ CloseWrite() error }).CloseWrite()

	data, err := io.ReadAll(client)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello" {
		t.Errorf("got %q want hello", data)
	}
}

// TestSOCKS5_ConnectToRefusedPort returns a sensible REP when the target
// is unreachable.
func TestSOCKS5_ConnectRefused(t *testing.T) {
	out := direct.New("out")
	disp := dispatcher.NewStaticDispatcher(out, nil)
	server := socks.New("in", "127.0.0.1:0", socks.AuthNone, nil, disp)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := server.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	time.Sleep(50 * time.Millisecond)
	client, err := net.Dial("tcp", server.ListenAddr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	// Method negotiation.
	if _, err := client.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 2)
	if _, err := io.ReadFull(client, buf); err != nil {
		t.Fatal(err)
	}

	// CONNECT to port 1 (should be refused on most systems).
	req := []byte{0x05, 0x01, 0x00, 0x01, 127, 0, 0, 1, 0, 1}
	if _, err := client.Write(req); err != nil {
		t.Fatal(err)
	}
	reply := make([]byte, 10)
	if _, err := io.ReadFull(client, reply); err != nil {
		t.Fatal(err)
	}
	// REP should be 0x05 (connection refused) or 0x04 (host unreachable)
	// depending on OS; either is acceptable. 0x00 or 0x01 would be wrong.
	if reply[1] != 0x05 && reply[1] != 0x04 {
		t.Errorf("refused connect REP=0x%02x, want 0x04 or 0x05", reply[1])
	}
}
