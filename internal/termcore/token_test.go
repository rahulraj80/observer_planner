package termcore

import (
	"encoding/base64"
	"testing"
)

// TestNewTokenNeverStartsWithAFlagCharacter: about one draw in 32 starts
// with '-' or '_'; no minted token does, every one decodes to n bytes.
func TestNewTokenNeverStartsWithAFlagCharacter(t *testing.T) {
	for i := 0; i < 5000; i++ {
		id, err := NewToken(32)
		if err != nil {
			t.Fatal(err)
		}
		if !TokenStartOK(id) {
			t.Fatalf("token %q starts with %q", id, id[0])
		}
		if b, err := base64.RawURLEncoding.DecodeString(id); err != nil || len(b) != 32 {
			t.Fatalf("token %q decodes to %d bytes, %v", id, len(b), err)
		}
	}
	if _, err := NewToken(0); err == nil {
		t.Fatal("a zero-byte token was minted")
	}
	for _, id := range []string{"", "-a", "_a"} {
		if TokenStartOK(id) {
			t.Fatalf("%q accepted", id)
		}
	}
}
