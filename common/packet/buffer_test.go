package packet

import (
	"bytes"
	"testing"
)

func TestBuffer_NewStartsEmpty(t *testing.T) {
	b := New(64)
	if b.Cap() != 64 {
		t.Fatalf("Cap() = %d, want 64", b.Cap())
	}
	if b.Len() != 0 {
		t.Fatalf("Len() = %d, want 0", b.Len())
	}
	if len(b.Bytes()) != 0 {
		t.Fatalf("len(Bytes()) = %d, want 0", len(b.Bytes()))
	}
}

func TestBuffer_AdvanceAppendsAtTail(t *testing.T) {
	b := New(64)

	first := b.Advance(4)
	copy(first, "abcd")
	second := b.Advance(2)
	copy(second, "ef")

	if got := string(b.Bytes()); got != "abcdef" {
		t.Fatalf("Bytes() = %q, want %q", got, "abcdef")
	}
	if b.Len() != 6 {
		t.Fatalf("Len() = %d, want 6", b.Len())
	}
}

// TestBuffer_AdvanceYieldsWritableRegion verifies the returned slice
// aliases the backing array, so callers can copy into it directly.
func TestBuffer_AdvanceYieldsWritableRegion(t *testing.T) {
	b := New(16)
	p := b.Advance(4)
	copy(p, "\x01\x02\x03\x04")

	got := b.Bytes()
	if !bytes.Equal(got, []byte{1, 2, 3, 4}) {
		t.Fatalf("Bytes() = %v, want [1 2 3 4]", got)
	}
}

// TestBuffer_ExtendHeaderPrependsWithoutCopy verifies the core use case:
// claiming header space in front of an existing payload.
func TestBuffer_ExtendHeaderPrependsWithoutCopy(t *testing.T) {
	b := New(64)

	payload := b.Advance(3)
	copy(payload, "def")
	header := b.ExtendHeader(3)
	copy(header, "abc")

	if got := string(b.Bytes()); got != "abcdef" {
		t.Fatalf("Bytes() = %q, want %q", got, "abcdef")
	}
	if b.Len() != 6 {
		t.Fatalf("Len() = %d, want 6", b.Len())
	}
}

func TestBuffer_Truncate(t *testing.T) {
	b := New(64)
	copy(b.Advance(8), "12345678")

	b.Truncate(5)
	if got := string(b.Bytes()); got != "12345" {
		t.Fatalf("Bytes() after Truncate(5) = %q, want %q", got, "12345")
	}
	if b.Len() != 5 {
		t.Fatalf("Len() = %d, want 5", b.Len())
	}

	// Truncating to zero empties the buffer but keeps the cursors, so
	// ExtendHeader/Advance still work afterwards.
	b.Truncate(0)
	if b.Len() != 0 || len(b.Bytes()) != 0 {
		t.Fatalf("Len() = %d after Truncate(0), want 0", b.Len())
	}
	copy(b.Advance(1), "x")
	copy(b.ExtendHeader(1), "y")
	if got := string(b.Bytes()); got != "yx" {
		t.Fatalf("Bytes() = %q, want %q", got, "yx")
	}

	// Truncate can also grow back within the claimed region.
	b.Truncate(0)
	b.Truncate(1)
	if got := string(b.Bytes()); got != "y" {
		t.Fatalf("Bytes() = %q, want %q", got, "y")
	}
}

// TestBuffer_ResetRestoresHeadroom verifies Reset returns the cursors to
// the midpoint so both directions have room again.
func TestBuffer_ResetRestoresHeadroom(t *testing.T) {
	b := New(64)

	copy(b.Advance(32), bytes.Repeat([]byte{'a'}, 32))
	copy(b.ExtendHeader(32), bytes.Repeat([]byte{'h'}, 32))
	if b.Len() != 64 {
		t.Fatalf("Len() = %d, want 64 (buffer full)", b.Len())
	}

	b.Reset()
	if b.Len() != 0 {
		t.Fatalf("Len() after Reset = %d, want 0", b.Len())
	}

	// After Reset the full half-capacity is available on each side again.
	copy(b.Advance(32), bytes.Repeat([]byte{'a'}, 32))
	copy(b.ExtendHeader(32), bytes.Repeat([]byte{'h'}, 32))
	if got := b.Len(); got != 64 {
		t.Fatalf("Len() after Reset and refill = %d, want 64", got)
	}
}

// TestBuffer_PackageFlow exercises the intended UDP sequence: write the
// payload, prepend a protocol header, then hand Bytes() to the writer.
func TestBuffer_PackageFlow(t *testing.T) {
	b := New(128)

	copy(b.Advance(4), "data")
	hdr := b.ExtendHeader(2)
	hdr[0] = 0x13
	hdr[1] = 0x37

	want := []byte{0x13, 0x37, 'd', 'a', 't', 'a'}
	if !bytes.Equal(b.Bytes(), want) {
		t.Fatalf("Bytes() = %v, want %v", b.Bytes(), want)
	}

	// A fresh packet reuses the same buffer.
	b.Reset()
	if b.Len() != 0 {
		t.Fatalf("Len() after Reset = %d, want 0", b.Len())
	}
	copy(b.Advance(2), "ok")
	if got := string(b.Bytes()); got != "ok" {
		t.Fatalf("Bytes() = %q, want %q", got, "ok")
	}
}

func TestBuffer_OddCapacityMidpoint(t *testing.T) {
	b := New(7) // midpoint is 3: 3 bytes of head room, 4 of tail room
	copy(b.Advance(4), "tail")
	copy(b.ExtendHeader(3), "hed")
	if got := string(b.Bytes()); got != "hedtail" {
		t.Fatalf("Bytes() = %q, want %q", got, "hedtail")
	}
}

func mustPanic(t *testing.T, name string, fn func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Errorf("%s: expected panic, got none", name)
		}
	}()
	fn()
}

func TestBuffer_Panics(t *testing.T) {
	t.Run("Advance beyond capacity", func(t *testing.T) {
		b := New(64) // tail room is 32
		mustPanic(t, "Advance(33)", func() { b.Advance(33) })
		b.Advance(32) // exactly full is fine
		mustPanic(t, "Advance(1) past end", func() { b.Advance(1) })
	})

	t.Run("ExtendHeader beyond head room", func(t *testing.T) {
		b := New(64) // head room is 32
		mustPanic(t, "ExtendHeader(33)", func() { b.ExtendHeader(33) })
		b.ExtendHeader(32) // exactly full is fine
		mustPanic(t, "ExtendHeader(1) below start", func() { b.ExtendHeader(1) })
	})

	t.Run("Truncate out of range", func(t *testing.T) {
		b := New(64) // start is 32, so start+n must stay <= 64
		mustPanic(t, "Truncate(-1)", func() { b.Truncate(-1) })
		mustPanic(t, "Truncate(33)", func() { b.Truncate(33) })
		b.Truncate(32) // exactly at capacity is fine
	})
}
