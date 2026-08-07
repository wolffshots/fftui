package data

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/wolffshots/fftui/internal/model"
)

func testService() *Service {
	return NewService(model.NewCSVSource("../../testdata/cycles.csv"))
}

// TestServiceLifecycle covers the not-fetched-yet state, a refresh, and that
// Latest republishes the same snapshot.
func TestServiceLifecycle(t *testing.T) {
	svc := testService()

	if snap, err := svc.Latest(); snap != nil || err != nil {
		t.Fatalf("Latest before Refresh = %v, %v; want nil, nil", snap, err)
	}

	snap, err := svc.Refresh(context.Background())
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if len(snap.Cycles) != 43 {
		t.Fatalf("Refresh returned %d cycles, want 43", len(snap.Cycles))
	}
	if snap.Now.IsZero() || snap.FetchedAt.IsZero() {
		t.Errorf("Now/FetchedAt not set: %v / %v", snap.Now, snap.FetchedAt)
	}

	got, err := svc.Latest()
	if err != nil {
		t.Fatalf("Latest after Refresh: %v", err)
	}
	if got != snap {
		t.Fatalf("Latest = %p, want the snapshot Refresh returned (%p)", got, snap)
	}
}

// TestServiceDateRange: SetDateRange trims snapshots to cycles overlapping the
// window — a cycle straddling a bound stays in — and zero bounds are open.
func TestServiceDateRange(t *testing.T) {
	day := func(s string) time.Time {
		d, err := time.Parse("2006-01-02", s)
		if err != nil {
			t.Fatalf("bad date %q: %v", s, err)
		}
		return d
	}
	cases := []struct {
		name     string
		from, to string
		want     int
	}{
		{"no window", "", "", 43},
		{"both bounds", "2026-01-01", "2026-06-30", 12},
		{"open to", "2026-01-01", "", 14},
		{"open from", "", "2025-12-31", 29},
		// FX0004 runs 2024-10-29 → 2024-10-31: still in when the window starts
		// on its last day, out one day later.
		{"straddles from", "2024-10-31", "", 40},
		{"past straddle", "2024-11-01", "", 39},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := testService()
			var from, to time.Time
			if tc.from != "" {
				from = day(tc.from)
			}
			if tc.to != "" {
				to = day(tc.to)
			}
			svc.SetDateRange(from, to)
			snap, err := svc.Refresh(context.Background())
			if err != nil {
				t.Fatalf("Refresh: %v", err)
			}
			if len(snap.Cycles) != tc.want {
				t.Fatalf("got %d cycles, want %d", len(snap.Cycles), tc.want)
			}
		})
	}
}

// countingSource counts Fetch calls so tests can assert a window change costs
// no source round trip.
type countingSource struct {
	inner model.CycleSource
	n     int
}

func (c *countingSource) Fetch(ctx context.Context) ([]model.Cycle, error) {
	c.n++
	return c.inner.Fetch(ctx)
}

// TestServiceSetDateRangeRepublishes: changing the window after a refresh
// re-filters the cached fetch and publishes it immediately — no refetch —
// and clearing it restores the full set.
func TestServiceSetDateRangeRepublishes(t *testing.T) {
	src := &countingSource{inner: model.NewCSVSource("../../testdata/cycles.csv")}
	svc := NewService(src)
	if _, err := svc.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	svc.SetDateRange(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), time.Time{})
	if snap, _ := svc.Latest(); len(snap.Cycles) != 14 {
		t.Fatalf("windowed Latest has %d cycles, want 14", len(snap.Cycles))
	}

	svc.SetDateRange(time.Time{}, time.Time{})
	if snap, _ := svc.Latest(); len(snap.Cycles) != 43 {
		t.Fatalf("cleared Latest has %d cycles, want 43", len(snap.Cycles))
	}

	if src.n != 1 {
		t.Fatalf("window changes hit the source: %d fetches, want 1", src.n)
	}
}

// TestServiceRefreshError: a source failure is returned and published, with no
// snapshot to hand back.
func TestServiceRefreshError(t *testing.T) {
	svc := NewService(model.NewCSVSource("../../testdata/does-not-exist.csv"))
	if _, err := svc.Refresh(context.Background()); err == nil {
		t.Fatal("Refresh on a missing file should error")
	}
	if snap, err := svc.Latest(); snap != nil || err == nil {
		t.Fatalf("Latest after failed Refresh = %v, %v; want nil, error", snap, err)
	}
}

// TestServiceConcurrent hammers Refresh and Latest in parallel; the mutexes
// make this meaningful under -race.
func TestServiceConcurrent(t *testing.T) {
	svc := testService()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			if _, err := svc.Refresh(context.Background()); err != nil {
				t.Errorf("Refresh: %v", err)
			}
		}()
		go func() {
			defer wg.Done()
			_, _ = svc.Latest()
		}()
	}
	wg.Wait()

	snap, err := svc.Latest()
	if err != nil || snap == nil || len(snap.Cycles) != 43 {
		t.Fatalf("after concurrent refreshes: snap=%v err=%v", snap, err)
	}
}

// TestRefreshKeepsExtras: the live extras are best-effort, so a refresh whose
// extras pulls fail (or, as here, can't run at all) keeps the previous values
// instead of blanking the live panels mid-session — nil only ever means
// "never fetched".
func TestRefreshKeepsExtras(t *testing.T) {
	svc := testService()
	if _, err := svc.Refresh(context.Background()); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	svc.mu.Lock()
	svc.raw.Client = &model.ClientStatus{FundsAvailable: 42}
	svc.raw.Market = &model.MarketConditions{Period: 7}
	svc.mu.Unlock()

	snap, err := svc.Refresh(context.Background())
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if snap.Client == nil || snap.Client.FundsAvailable != 42 {
		t.Fatalf("client status not carried forward: %+v", snap.Client)
	}
	if snap.Market == nil || snap.Market.Period != 7 {
		t.Fatalf("market conditions not carried forward: %+v", snap.Market)
	}
}
