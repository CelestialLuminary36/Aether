package core

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"testing"
	"time"
)

// relayLegs builds the two TCP legs Relay sits between. left and right are
// handed to Relay; the test speaks on their peers, client and server:
//
//	client --- left <==Relay==> right --- server
//
// TCP loopback is used instead of net.Pipe so CloseWrite (half-close) is
// available and behaves like production sockets.
func relayLegs(t *testing.T) (left, right, client, server net.Conn) {
	t.Helper()

	l1, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l1.Close() })

	l2, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l2.Close() })

	if client, err = net.DialTimeout("tcp", l1.Addr().String(), 2*time.Second); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })

	if left, err = l1.Accept(); err != nil {
		t.Fatal(err)
	}

	if server, err = net.DialTimeout("tcp", l2.Addr().String(), 2*time.Second); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })

	if right, err = l2.Accept(); err != nil {
		t.Fatal(err)
	}
	return left, right, client, server
}

// assertClosed fails unless conn ends up fully closed (reads yield
// net.ErrClosed). Relay performs its final Close calls from background
// goroutines, and a half-closed conn briefly reports io.EOF, so poll
// until the close lands instead of requiring it on first read.
func assertClosed(t *testing.T, name string, conn net.Conn) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		_ = conn.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
		_, err := conn.Read(make([]byte, 1))
		switch {
		case errors.Is(err, net.ErrClosed):
			return
		case err == nil:
			t.Fatalf("%s still readable after Relay returned", name)
		case time.Now().After(deadline):
			t.Fatalf("%s not closed within 2s of Relay returning (last error: %v)", name, err)
		}
	}
}

// closeWrite shuts down one write direction of a TCP conn.
func closeWrite(t *testing.T, conn net.Conn) {
	t.Helper()
	hc, ok := conn.(interface{ CloseWrite() error })
	if !ok {
		t.Fatalf("%T does not support CloseWrite", conn)
	}
	if err := hc.CloseWrite(); err != nil {
		t.Fatalf("CloseWrite: %v", err)
	}
}

func TestRelay_BidirectionalCopyAndCounts(t *testing.T) {
	left, right, client, server := relayLegs(t)

	upPayload := bytes.Repeat([]byte{'u'}, 1000)
	downPayload := bytes.Repeat([]byte{'d'}, 700)

	type result struct {
		up, down uint64
		err      error
	}
	done := make(chan result, 1)
	go func() {
		up, down, err := Relay(context.Background(), left, right)
		done <- result{up, down, err}
	}()

	// Uplink: two writes to exercise accumulation.
	if _, err := client.Write(upPayload[:600]); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Write(upPayload[600:]); err != nil {
		t.Fatal(err)
	}
	if _, err := server.Write(downPayload); err != nil {
		t.Fatal(err)
	}

	upRead := make([]byte, len(upPayload))
	if _, err := io.ReadFull(server, upRead); err != nil {
		t.Fatalf("server read uplink: %v", err)
	}
	if !bytes.Equal(upRead, upPayload) {
		t.Fatal("uplink payload corrupted")
	}

	downRead := make([]byte, len(downPayload))
	if _, err := io.ReadFull(client, downRead); err != nil {
		t.Fatalf("client read downlink: %v", err)
	}
	if !bytes.Equal(downRead, downPayload) {
		t.Fatal("downlink payload corrupted")
	}

	// Shut both directions down cleanly and wait for Relay to finish.
	closeWrite(t, client)
	closeWrite(t, server)

	select {
	case r := <-done:
		if r.err != nil {
			t.Fatalf("Relay error = %v, want nil", r.err)
		}
		if r.up != uint64(len(upPayload)) {
			t.Errorf("uplink = %d, want %d", r.up, len(upPayload))
		}
		if r.down != uint64(len(downPayload)) {
			t.Errorf("downlink = %d, want %d", r.down, len(downPayload))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Relay did not return after both sides finished")
	}

	// Relay owns both conns: they must be closed once it returns.
	assertClosed(t, "left", left)
	assertClosed(t, "right", right)
}

// TestRelay_HalfClose verifies that after one side shuts down its write
// direction, data can still flow back through Relay.
func TestRelay_HalfClose(t *testing.T) {
	left, right, client, server := relayLegs(t)

	done := make(chan error, 1)
	go func() {
		_, _, err := Relay(context.Background(), left, right)
		done <- err
	}()

	if _, err := client.Write([]byte("request")); err != nil {
		t.Fatal(err)
	}
	closeWrite(t, client)

	// Server sees EOF after the request, yet can still reply.
	req, err := io.ReadAll(server)
	if err != nil {
		t.Fatalf("server read: %v", err)
	}
	if string(req) != "request" {
		t.Fatalf("server got %q, want %q", req, "request")
	}
	if _, err := server.Write([]byte("response")); err != nil {
		t.Fatalf("server write after client half-close: %v", err)
	}
	closeWrite(t, server)

	resp, err := io.ReadAll(client)
	if err != nil {
		t.Fatalf("client read: %v", err)
	}
	if string(resp) != "response" {
		t.Fatalf("client got %q, want %q", resp, "response")
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Relay error = %v, want nil after clean half-closes", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Relay did not return after half-closes")
	}
}

func TestRelay_ContextCancel(t *testing.T) {
	left, right, client, _ := relayLegs(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		_, _, err := Relay(ctx, left, right)
		done <- err
	}()

	// Push some traffic, then cancel mid-stream.
	if _, err := client.Write([]byte("partial")); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		// Cancellation-induced closes are benign: no error is surfaced.
		if err != nil {
			t.Fatalf("Relay error after cancel = %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Relay did not return after ctx cancel")
	}

	assertClosed(t, "left", left)
	assertClosed(t, "right", right)
}

// readErrConn fails every Read with a fixed non-benign error.
type readErrConn struct {
	net.Conn
	err error
}

func (c *readErrConn) Read([]byte) (int, error) { return 0, c.err }

func TestRelay_ReturnsFirstError(t *testing.T) {
	p1a, p1b := net.Pipe()
	defer p1b.Close()
	p2a, p2b := net.Pipe()
	defer p2b.Close()

	boom := errors.New("boom: tunnel collapsed")
	left := &readErrConn{Conn: p1a, err: boom}
	right := &readErrConn{Conn: p2a, err: boom}

	done := make(chan error, 1)
	go func() {
		_, _, err := Relay(context.Background(), left, right)
		done <- err
	}()

	select {
	case err := <-done:
		if !errors.Is(err, boom) {
			t.Fatalf("Relay error = %v, want %v", err, boom)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Relay did not return after read errors")
	}
}
