package packet

import "testing"

// TODO(user): replace these placeholder tests with real Buffer tests.
// See Plan 1 Task 1 for the full test suite.

func TestBuffer_NewIsEmpty(t *testing.T) {
	b := New(64)
	if b.Len() != 0 {
		t.Fatalf("Len()=%d want 0", b.Len())
	}
}
