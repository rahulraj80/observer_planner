package sandboxvocab

import "time"

// egressrefusal.go holds the minute bucket of the sandbox egress refusal
// audit (lane N1, audit N1-A10): the one definition both the refusal flood
// bound (internal/sandboxnet.RefusalLimiter) and the store's unique key on
// sandbox_egress_refusals use, so the two can never disagree.

// EgressRefusalMinute is the minute bucket of at (Unix minutes, UTC).
func EgressRefusalMinute(at time.Time) int64 { return at.Unix() / 60 }
