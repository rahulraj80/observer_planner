package store

import (
	"context"
	"testing"
	"time"
)

func TestBriefingSnapshot_RoundTrip(t *testing.T) {
	t.Parallel()
	s, _ := newTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 1, 9, 30, 0, 0, time.UTC)

	if _, ok, err := s.LoadBriefingSnapshot(ctx, "2026-07-31"); err != nil {
		t.Fatal(err)
	} else if ok {
		t.Fatal("expected no snapshot before any save")
	}

	const json = `{"date":"2026-07-31","generated_at":"2026-08-01T09:30:00Z"}`

	if err := s.SaveBriefingSnapshot(ctx, "2026-07-31", json, now); err != nil {
		t.Fatal(err)
	}

	got, ok, err := s.LoadBriefingSnapshot(ctx, "2026-07-31")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || got != json {
		t.Errorf("round trip mismatch: ok=%v got=%q", ok, got)
	}
}

func TestBriefingSnapshot_OverwriteIdempotent(t *testing.T) {
	t.Parallel()
	s, _ := newTestStore(t)
	ctx := context.Background()

	first := `{"date":"2026-07-31","generated_at":"2026-08-01T09:30:00Z"}`
	second := `{"date":"2026-07-31","generated_at":"2026-08-01T10:00:00Z"}`

	for _, j := range []string{first, second} {
		if err := s.SaveBriefingSnapshot(ctx, "2026-07-31", j, time.Now()); err != nil {
			t.Fatal(err)
		}
	}

	got, ok, err := s.LoadBriefingSnapshot(ctx, "2026-07-31")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || got != second {
		t.Errorf("newest composition should win: ok=%v got=%q", ok, got)
	}
}

func TestBriefingSnapshot_LatestDate(t *testing.T) {
	t.Parallel()
	s, _ := newTestStore(t)
	ctx := context.Background()

	if _, ok, err := s.LatestBriefingDate(ctx); err != nil {
		t.Fatal(err)
	} else if ok {
		t.Fatal("expected no latest date before any save")
	}

	for _, d := range []string{"2026-07-29", "2026-07-31", "2026-07-30"} {
		if err := s.SaveBriefingSnapshot(ctx, d, `{"date":"`+d+`"}`, time.Now()); err != nil {
			t.Fatal(err)
		}
	}

	got, ok, err := s.LatestBriefingDate(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || got != "2026-07-31" {
		t.Errorf("expected 2026-07-31, got ok=%v date=%q", ok, got)
	}
}
