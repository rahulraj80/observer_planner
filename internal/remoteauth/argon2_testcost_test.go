package remoteauth

import "testing"

// Lower the Argon2id cost for this test binary only (D-609); a stored hash
// carries its own parameters, so every verify path is exercised unchanged.
// TestProductionArgonCost keeps the production values pinned.
func init() {
	argonMemoryKiB, argonTime = 64, 1
}

// TestProductionArgonCost pins the production Argon2id parameters (OWASP
// m=19MiB, t=2, p=1), which the init above lowers for the rest of this test
// binary.
func TestProductionArgonCost(t *testing.T) {
	if prodArgonMemoryKiB != 19*1024 || prodArgonTime != 2 || argonThreads != 1 || argonKeyLen != 32 || argonSaltLen != 16 {
		t.Fatalf("production argon2id cost changed: m=%d t=%d p=%d", prodArgonMemoryKiB, prodArgonTime, argonThreads)
	}
}
