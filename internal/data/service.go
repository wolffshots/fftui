// Package data owns fetching and caching of cycle data. Service wraps a
// model.CycleSource behind a mutex so concurrent front ends (TUI, web) can
// share one source safely.
package data

import (
	"context"
	"sync"
	"time"

	"github.com/wolffshots/fftui/internal/model"
)

// liveSpreadPeriod is the history window (days) for the Live view spread
// sparkline; must be one of the API's allowed periods (1/7/30/90/365).
const liveSpreadPeriod = 7

// trendSpreadPeriod is the long history window (days) the analytics trend
// strip compares recent spreads against; also an API-allowed period.
const trendSpreadPeriod = 365

// Today returns the current local calendar date as a UTC-midnight time — the
// same convention cycle dates are parsed with. Deriving it from the local date
// (rather than truncating UTC time) avoids the early-morning off-by-one for
// timezones ahead of UTC (e.g. SAST).
func Today() time.Time {
	y, m, d := time.Now().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// Snapshot is one fetched view of the data: the cycle history plus the
// live-only extras (nil in CSV mode or if their pull failed).
type Snapshot struct {
	Cycles     []model.Cycle
	Client     *model.ClientStatus
	Market     *model.MarketConditions
	MarketYear *model.MarketConditions // year-long history for the trend strip
	Now        time.Time               // fetch-time "today"
	FetchedAt  time.Time               // wall-clock time the fetch completed
}

// Service serialises fetches against a single CycleSource and publishes the
// latest snapshot. src is unexported on purpose: ALL fetches must go through
// Service, because LiveSource mutates its token on a 401 re-mint and is not
// safe for concurrent fetches.
type Service struct {
	src model.CycleSource
	now func() time.Time // Snapshot.Now source; Today unless overridden

	fetchMu sync.Mutex // serialises whole fetches against src

	mu   sync.RWMutex // guards snap/err publication
	snap *Snapshot
	err  error
}

// NewService wraps src in a Service.
func NewService(src model.CycleSource) *Service {
	return &Service{src: src, now: Today}
}

// SetNow overrides the clock used for Snapshot.Now. For tests, so
// date-relative figures (trend windows, in-progress buckets) are
// deterministic. Call before any Refresh; not safe concurrently with one.
func (s *Service) SetNow(now func() time.Time) {
	s.now = now
}

// Refresh runs a full fetch: the cycle history, then (for the live source) the
// best-effort extras — current-cycle status and market spread. If the extras
// fail, the cycles still load and those fields just stay nil rather than
// failing the whole refresh.
func (s *Service) Refresh(ctx context.Context) (*Snapshot, error) {
	s.fetchMu.Lock()
	defer s.fetchMu.Unlock()

	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	cs, err := s.src.Fetch(ctx)
	if err != nil {
		s.mu.Lock()
		s.err = err
		s.mu.Unlock()
		return nil, err
	}
	snap := &Snapshot{Cycles: cs, Now: s.now()}
	if ff, ok := s.src.(*model.LiveSource); ok {
		if st, err := ff.FetchClient(ctx); err == nil {
			snap.Client = st
		}
		if mc, err := ff.FetchMarketConditions(ctx, liveSpreadPeriod); err == nil {
			snap.Market = mc
		}
		if mc, err := ff.FetchMarketConditions(ctx, trendSpreadPeriod); err == nil {
			snap.MarketYear = mc
		}
	}
	snap.FetchedAt = time.Now()

	s.mu.Lock()
	s.snap, s.err = snap, nil
	s.mu.Unlock()
	return snap, nil
}

// Latest returns the most recent snapshot and the error from the most recent
// refresh. nil, nil means nothing has been fetched yet. A failed refresh keeps
// the last good snapshot and reports the error alongside it.
func (s *Service) Latest() (*Snapshot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.snap, s.err
}
