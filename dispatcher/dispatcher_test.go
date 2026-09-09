package dispatcher

import (
	"context"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"github.com/CelestialLuminary36/Aether/core"
)

// stubOutbound is a controllable core.Outbound: it records the dial and
// returns either a canned conn or a canned error.
type stubOutbound struct {
	tag      string
	networks []core.Network
	onDial   func()
	dialErr  error
	conn     net.Conn
	dials    int
}

func (o *stubOutbound) Tag() string              { return o.tag }
func (o *stubOutbound) Type() string             { return "stub" }
func (o *stubOutbound) Networks() []core.Network { return o.networks }

func (o *stubOutbound) DialStream(ctx context.Context, md *core.Metadata) (net.Conn, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if o.onDial != nil {
		o.onDial()
	}
	o.dials++
	if o.dialErr != nil {
		return nil, o.dialErr
	}
	return o.conn, nil
}

func (o *stubOutbound) DialPacket(ctx context.Context, md *core.Metadata) (core.PacketConn, error) {
	return nil, core.ErrNetworkNotSupported
}

// readErrConn fails every Read with a fixed error.
type readErrConn struct {
	net.Conn
	err error
}

func (c *readErrConn) Read([]byte) (int, error) { return 0, c.err }

func testMetadata() *core.Metadata {
	return &core.Metadata{
		Network:     core.NetworkTCP,
		Destination: core.AddrFromDomain("example.com", 443),
		InboundTag:  "test-in",
	}
}

// TestDispatchStream_DialOnDialedRelayOrder verifies the full success
// path: dial happens first, onDialed second, and only then does traffic
// flow through the relay.
func TestDispatchStream_DialOnDialedRelayOrder(t *testing.T) {
	// Echo backend that the stub outbound "dials".
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

	outConn, err := net.Dial("tcp", backend.Addr().String())
	if err != nil {
		t.Fatal(err)
	}

	// Inbound leg the dispatcher relays from.
	inListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer inListener.Close()
	client, err := net.Dial("tcp", inListener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	inConn, err := inListener.Accept()
	if err != nil {
		t.Fatal(err)
	}

	var events []string
	stub := &stubOutbound{
		tag:      "stub",
		networks: []core.Network{core.NetworkTCP},
		conn:     outConn,
		onDial:   func() { events = append(events, "dial") },
	}
	disp := New(stub, nil)

	dialed := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		err := disp.DispatchStream(context.Background(), testMetadata(), inConn, func() error {
			events = append(events, "onDialed")
			close(dialed)
			return nil
		})
		done <- err
	}()

	// Only exchange traffic once onDialed has run.
	<-dialed
	if _, err := client.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	client.(interface{ CloseWrite() error }).CloseWrite()

	echo, err := io.ReadAll(client)
	if err != nil {
		t.Fatalf("client read: %v", err)
	}
	if string(echo) != "ping" {
		t.Fatalf("client got %q, want %q", echo, "ping")
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("DispatchStream error = %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("DispatchStream did not return after relay finished")
	}

	if len(events) != 2 || events[0] != "dial" || events[1] != "onDialed" {
		t.Fatalf("events = %v, want [dial onDialed]", events)
	}
	if stub.dials != 1 {
		t.Errorf("dials = %d, want 1", stub.dials)
	}
}

func TestDispatchStream_DialError(t *testing.T) {
	boom := errors.New("dial boom")
	stub := &stubOutbound{
		tag:      "stub",
		networks: []core.Network{core.NetworkTCP},
		dialErr:  boom,
	}
	disp := New(stub, nil)

	inA, inB := net.Pipe()
	defer inB.Close()

	onDialedCalled := false
	err := disp.DispatchStream(context.Background(), testMetadata(), inA, func() error {
		onDialedCalled = true
		return nil
	})
	if !errors.Is(err, boom) {
		t.Fatalf("DispatchStream error = %v, want %v", err, boom)
	}
	if onDialedCalled {
		t.Error("onDialed was called despite dial failure")
	}
	if stub.dials != 1 {
		t.Errorf("dials = %d, want 1", stub.dials)
	}
}

// TestDispatchStream_OnDialedError verifies that a failing onDialed
// aborts the dispatch, closes the outbound conn, and never relays.
func TestDispatchStream_OnDialedError(t *testing.T) {
	outA, outB := net.Pipe()
	defer outB.Close()

	stub := &stubOutbound{
		tag:      "stub",
		networks: []core.Network{core.NetworkTCP},
		conn:     outA,
	}
	disp := New(stub, nil)

	inA, inB := net.Pipe()
	defer inB.Close()

	oops := errors.New("reply failed")
	err := disp.DispatchStream(context.Background(), testMetadata(), inA, func() error {
		return oops
	})
	if !errors.Is(err, oops) {
		t.Fatalf("DispatchStream error = %v, want %v", err, oops)
	}

	// The dialed outbound conn must be closed; its peer sees EOF.
	_ = outB.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := outB.Read(make([]byte, 1)); err != io.EOF {
		t.Fatalf("outbound peer read = %v, want io.EOF (conn must be closed)", err)
	}
}

func TestDispatchStream_RelayError(t *testing.T) {
	outA, outB := net.Pipe()
	defer outB.Close()

	stub := &stubOutbound{
		tag:      "stub",
		networks: []core.Network{core.NetworkTCP},
		conn:     outA,
	}
	disp := New(stub, nil)

	inA, inB := net.Pipe()
	defer inB.Close()

	boom := errors.New("client vanished")
	err := disp.DispatchStream(context.Background(), testMetadata(), &readErrConn{Conn: inA, err: boom}, nil)
	if !errors.Is(err, boom) {
		t.Fatalf("DispatchStream error = %v, want %v", err, boom)
	}
}

// ctxBlockOutbound blocks in DialStream until ctx is canceled.
type ctxBlockOutbound struct {
	started chan struct{}
}

func (o *ctxBlockOutbound) Tag() string              { return "ctx-stub" }
func (o *ctxBlockOutbound) Type() string             { return "stub" }
func (o *ctxBlockOutbound) Networks() []core.Network { return []core.Network{core.NetworkTCP} }

func (o *ctxBlockOutbound) DialStream(ctx context.Context, md *core.Metadata) (net.Conn, error) {
	close(o.started)
	<-ctx.Done()
	return nil, ctx.Err()
}

func (o *ctxBlockOutbound) DialPacket(ctx context.Context, md *core.Metadata) (core.PacketConn, error) {
	return nil, core.ErrNetworkNotSupported
}

func TestDispatchStream_ContextCanceled(t *testing.T) {
	stub := &ctxBlockOutbound{started: make(chan struct{})}
	disp := New(stub, nil)

	ctx, cancel := context.WithCancel(context.Background())
	inA, inB := net.Pipe()
	defer inB.Close()

	done := make(chan error, 1)
	go func() {
		done <- disp.DispatchStream(ctx, testMetadata(), inA, nil)
	}()

	<-stub.started
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("DispatchStream error = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("DispatchStream did not return after ctx cancel")
	}
}

func TestDispatchPacket_NotSupported(t *testing.T) {
	stub := &stubOutbound{tag: "stub", networks: []core.Network{core.NetworkTCP}}
	disp := New(stub, nil)

	err := disp.DispatchPacket(context.Background(), testMetadata(), nil)
	if err != core.ErrNetworkNotSupported {
		t.Fatalf("DispatchPacket error = %v, want core.ErrNetworkNotSupported", err)
	}
}
