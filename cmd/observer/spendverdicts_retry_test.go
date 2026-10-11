package main

import (
	"context"
	"errors"
	"testing"
	"time"
)

var errBusy = errors.New("database is locked (5) (SQLITE_BUSY)")

func isBusyForTest(err error) bool { return errors.Is(err, errBusy) }

// TestRefreshRetryingTransient is U6: a refresh that hits lock contention is
// retried with backoff; a busy error that outlasts the backoff is reported
// as transient (logged quietly), and a real error is never retried.
func TestRefreshRetryingTransient(t *testing.T) {
	short := []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond}
	cases := []struct {
		name      string
		errs      []error
		calls     int
		wantErr   bool
		transient bool
	}{
		{"busy then ok", []error{errBusy, errBusy, nil}, 3, false, false},
		{"busy throughout", []error{errBusy, errBusy, errBusy, errBusy}, 4, true, true},
		{"real error is not retried", []error{errors.New("disk I/O error")}, 1, true, false},
		{"ok first time", []error{nil}, 1, false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			calls := 0
			_, transient, err := refreshRetryingTransient(context.Background(), func() (int, error) {
				e := c.errs[calls]
				calls++
				return 0, e
			}, short, isBusyForTest)
			if calls != c.calls || (err != nil) != c.wantErr || transient != c.transient {
				t.Fatalf("calls=%d err=%v transient=%v, want calls=%d err=%v transient=%v", calls, err, transient, c.calls, c.wantErr, c.transient)
			}
		})
	}
}
