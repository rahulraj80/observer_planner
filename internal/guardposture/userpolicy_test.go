package guardposture

import (
	"bytes"
	"encoding/json"
	"strconv"
	"testing"
	"time"
)

// legacyCanonical is the MAC input exactly as the pre-D-221 binary computed
// it (frozen copy of canonical at 7d0c35e2d).
func legacyCanonical(p Posture) []byte {
	p = normalize(p)
	var b bytes.Buffer
	b.WriteString(macDomain)
	b.WriteByte('\n')
	b.WriteString(strconv.Itoa(p.Schema))
	b.WriteByte('\n')
	b.WriteString(strconv.FormatBool(p.Enforced))
	b.WriteByte('\n')
	b.WriteString(strconv.Quote(p.Source))
	b.WriteByte('\n')
	b.WriteString(strconv.Quote(p.Generation))
	b.WriteByte('\n')
	for _, c := range p.GateCaps {
		b.WriteString(strconv.Quote(c))
		b.WriteByte(',')
	}
	b.WriteByte('\n')
	b.WriteString(p.WrittenAt.UTC().Format(time.RFC3339Nano))
	return b.Bytes()
}

// TestUserPolicyModeKeepsLegacyMAC pins the mixed-version promise: a posture
// that does not use the new field MACs exactly as the older binary's, so an
// upgraded hook and a not-yet-restarted daemon (or the reverse) still verify
// each other; a posture that uses it is covered by the MAC, so flipping the
// flag in the file is detected as tampering.
func TestUserPolicyModeKeepsLegacyMAC(t *testing.T) {
	at := time.Date(2026, 10, 4, 9, 0, 0, 0, time.UTC)
	p := Posture{Enforced: true, Source: SourceOrg, Generation: "12", GateCaps: []string{"tool_gate"}, WrittenAt: at}
	if !bytes.Equal(canonical(p), legacyCanonical(p)) {
		t.Fatalf("canonical changed for a posture without the new field:\n%s\nvs\n%s", canonical(p), legacyCanonical(p))
	}
	key := bytes.Repeat([]byte{7}, 32)
	signed := Sign(Posture{Enforced: true, Source: SourceOrg, GateCaps: []string{"tool_gate"}, UserPolicyFailClosed: true, WrittenAt: at}, key)
	if !Verify(&signed, key) {
		t.Fatal("a signed posture with the mode on does not verify")
	}
	forged := signed
	forged.UserPolicyFailClosed = false
	if Verify(&forged, key) {
		t.Fatal("clearing the mode in the file was not detected")
	}
	// An older binary's decoder ignores the unknown JSON field, and its
	// legacy MAC over the remaining fields does NOT match (the mode is
	// covered), so it treats the file as tampered and falls back to its own
	// config: never a silent misread of the new mode.
	raw, err := Encode(signed)
	if err != nil {
		t.Fatal(err)
	}
	var asOld Posture
	if err := json.Unmarshal(raw, &asOld); err != nil {
		t.Fatal(err)
	}
	if !asOld.UserPolicyFailClosed {
		t.Fatal("the mode did not round-trip through JSON")
	}
}

func TestUserPolicyClosesGate(t *testing.T) {
	on := &Posture{Enforced: true, GateCaps: []string{"tool_gate"}, UserPolicyFailClosed: true}
	cases := []struct {
		name string
		p    *Posture
		cap  string
		want bool
	}{
		{"on, enforced gate", on, "tool_gate", true},
		{"on, gate not enforced", on, "prompt_gate", false},
		{"mode off", &Posture{Enforced: true, GateCaps: []string{"tool_gate"}}, "tool_gate", false},
		{"not enforced", &Posture{UserPolicyFailClosed: true}, "tool_gate", false},
		{"nil", nil, "tool_gate", false},
	}
	for _, c := range cases {
		if got := c.p.UserPolicyClosesGate(c.cap); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
	if n := normalize(Posture{UserPolicyFailClosed: true}); n.UserPolicyFailClosed {
		t.Error("normalize must drop the mode from a non-enforced posture")
	}
}
