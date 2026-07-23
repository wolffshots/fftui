package data

import (
	"context"
	"sync"
	"testing"

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
