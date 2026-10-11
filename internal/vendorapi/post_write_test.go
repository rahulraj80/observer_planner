package vendorapi

import (
	"errors"
	"testing"
)

// TestUserSpendLimitSetIsClosed (lane ENTAPI-L14, T-617): the one write body
// encodes only a lower-case email and a whole, non-negative, bounded dollar
// amount; it has no null form (removing a limit is a loosening and cannot be
// expressed), and only it is a write kind.
func TestUserSpendLimitSetIsClosed(t *testing.T) {
	b, err := EncodePostBody(UserSpendLimitSet{UserEmail: "dev.one+x@corp.example", LimitDollars: 0})
	if err != nil || string(b) != `{"userEmail":"dev.one+x@corp.example","spendLimitDollars":0}` {
		t.Fatalf("encode %s %v", b, err)
	}
	for _, bad := range []UserSpendLimitSet{
		{UserEmail: "", LimitDollars: 1},
		{UserEmail: "Dev@corp.example", LimitDollars: 1},
		{UserEmail: "dev@corp", LimitDollars: 1},
		{UserEmail: "dev @corp.example", LimitDollars: 1},
		{UserEmail: "dev@corp.example\n", LimitDollars: 1},
		{UserEmail: "dev@corp.example", LimitDollars: -1},
		{UserEmail: "dev@corp.example", LimitDollars: MaxSpendLimitDollars + 1},
	} {
		if _, err := EncodePostBody(bad); !errors.Is(err, ErrBadPostBody) {
			t.Errorf("%+v encoded: %v", bad, err)
		}
	}
	if !WriteKind(PostUserSpendLimitSet) {
		t.Error("the spend-limit body is not a write kind")
	}
	for _, k := range []PostKind{PostEpochRange, PostEpochRangePage, PostPage} {
		if WriteKind(k) {
			t.Errorf("read body %s is a write kind", k)
		}
	}
}
