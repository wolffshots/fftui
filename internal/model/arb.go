package model

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// This file adds the live-only arbitrage dashboard data that cycle_list doesn't
// carry: the current in-progress cycle's status ("Awaiting market conditions"),
// client funds/allowances, and the live market spread. All endpoints live on the
// same srv data host and use the same Token auth as Fetch. They are only
// implemented on LiveSource — the CSV export has none of this data.

// ClientStatus is the live snapshot from get_client/{id}/: the current trade
// cycle's status plus the client's funds and allowances.
type ClientStatus struct {
	Status         TradeStatus
	FundsAvailable float64
	FundsUpdated   string // human string, e.g. "Last updated 15:10 on 18 Jul 2026"
	TotalProfit    float64
	MinimumReturn  float64 // fractional (0.1 = 10%)
	SDAAvailable   float64 // single-discretionary allowance remaining
	AITAvailable   float64 // approval-for-international-transfer allowance remaining (ex-FIA)
	SDADetail      SDADetail
	AITDetail      AITDetail
}

// SDADetail is the sda_details breakdown beside the headline SDA balance:
// what the year has left, what is held for cycles in flight, and what is spent.
type SDADetail struct {
	Unused   float64 // sda_unused
	Reserved float64 // reserved
	Used     float64 // sda_used
}

// AITDetail is the fia_details/ait_details breakdown beside the headline AIT
// balance.
type AITDetail struct {
	Available float64
	Pending   float64
	Locked    float64
}

// TradeStatus is the trade_status_v2 object — the current-cycle state the
// dashboard (and the user's Home Assistant) surface.
type TradeStatus struct {
	Slug           string // machine slug, e.g. "trade_loaded"
	SecondaryText  string // short label, e.g. "Awaiting market conditions"
	Description    string // full sentence shown under the label
	Icon           string // "info", etc.
	AmountInvested float64
	NetProfit      *float64 // nil while the cycle is still queued (API null)
}

// MarketPoint is one market-conditions sample: the local vs offshore price gap
// that defines the arbitrage, plus the spread it yields.
type MarketPoint struct {
	Datetime      string
	Spread        float64 // percent, e.g. 0.82 means 0.82%
	LocalPrice    float64
	OffshorePrice float64
	ExchangeRate  float64
}

// MarketConditions bundles the freshest spread with a history series for a
// sparkline. Period is the history window in days (one of 1/7/30/90/365).
type MarketConditions struct {
	Current MarketPoint
	History []MarketPoint
	Period  int
}

// ---- raw JSON shapes -------------------------------------------------------

type clientResponse struct {
	FundsAvailable   flexFloat `json:"funds_available"`
	FundsLastUpdated struct {
		String string `json:"string"`
	} `json:"funds_last_updated"`
	TotalProfit        flexFloat `json:"total_profit"`
	MinimumReturn      flexFloat `json:"minimum_return"`
	AllowanceAvailable struct {
		SDA flexFloat `json:"sda"`
		FIA flexFloat `json:"fia"` // legacy key for the AIT balance
		AIT flexFloat `json:"ait"`
		// The breakdowns are held raw and decoded separately so an unexpected
		// shape degrades to zeros instead of failing the whole snapshot.
		SDADetails json.RawMessage `json:"sda_details"`
		FIADetails json.RawMessage `json:"fia_details"` // legacy key
		AITDetails json.RawMessage `json:"ait_details"`
	} `json:"allowance_available"`
	TradeStatus struct {
		Slug           string     `json:"status_slug"`
		SecondaryText  string     `json:"status_secondary_text"`
		Description    string     `json:"status_description"`
		Icon           string     `json:"status_description_icon"`
		AmountInvested flexFloat  `json:"amount_invested"`
		NetProfit      *flexFloat `json:"net_profit"`
	} `json:"trade_status_v2"`
}

// sdaDetail decodes the sda_details breakdown, or zeros if it is absent.
func (r clientResponse) sdaDetail() SDADetail {
	var d struct {
		Unused   flexFloat `json:"sda_unused"`
		Reserved flexFloat `json:"reserved"`
		Used     flexFloat `json:"sda_used"`
	}
	_ = json.Unmarshal(r.AllowanceAvailable.SDADetails, &d)
	return SDADetail{Unused: float64(d.Unused), Reserved: float64(d.Reserved), Used: float64(d.Used)}
}

// aitDetail decodes the AIT breakdown, preferring the renamed key, or zeros if
// neither is present.
func (r clientResponse) aitDetail() AITDetail {
	raw := r.AllowanceAvailable.AITDetails
	if len(raw) == 0 {
		raw = r.AllowanceAvailable.FIADetails
	}
	var d struct {
		Available flexFloat `json:"available"`
		Pending   flexFloat `json:"pending"`
		Locked    flexFloat `json:"locked"`
	}
	_ = json.Unmarshal(raw, &d)
	return AITDetail{Available: float64(d.Available), Pending: float64(d.Pending), Locked: float64(d.Locked)}
}

// aitAvailable returns the AIT balance. The API still keys it "fia"; accept
// the renamed "ait" key too in case it follows SARS.
func (r clientResponse) aitAvailable() float64 {
	if r.AllowanceAvailable.AIT != 0 {
		return float64(r.AllowanceAvailable.AIT)
	}
	return float64(r.AllowanceAvailable.FIA)
}

type marketPointJSON struct {
	Datetime      string    `json:"datetime"`
	Spread        flexFloat `json:"spread"`
	LocalPrice    flexFloat `json:"local_price"`
	OffshorePrice flexFloat `json:"offshore_price"`
	ExchangeRate  flexFloat `json:"exchange_rate"`
}

func (m marketPointJSON) toPoint() MarketPoint {
	return MarketPoint{
		Datetime:      m.Datetime,
		Spread:        float64(m.Spread),
		LocalPrice:    float64(m.LocalPrice),
		OffshorePrice: float64(m.OffshorePrice),
		ExchangeRate:  float64(m.ExchangeRate),
	}
}

// ---- fetch methods ---------------------------------------------------------

// FetchClient returns the live client snapshot (current-cycle status + funds).
func (s *LiveSource) FetchClient(ctx context.Context) (*ClientStatus, error) {
	id, err := s.resolveClientID(ctx)
	if err != nil {
		return nil, err
	}
	raw, err := s.authGet(ctx, fmt.Sprintf("/api/get_client/%s/", id))
	if err != nil {
		return nil, err
	}
	var r clientResponse
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, fmt.Errorf("decode get_client: %w", err)
	}
	cs := &ClientStatus{
		FundsAvailable: float64(r.FundsAvailable),
		FundsUpdated:   r.FundsLastUpdated.String,
		TotalProfit:    float64(r.TotalProfit),
		MinimumReturn:  float64(r.MinimumReturn),
		SDAAvailable:   float64(r.AllowanceAvailable.SDA),
		AITAvailable:   r.aitAvailable(),
		SDADetail:      r.sdaDetail(),
		AITDetail:      r.aitDetail(),
		Status: TradeStatus{
			Slug:           r.TradeStatus.Slug,
			SecondaryText:  r.TradeStatus.SecondaryText,
			Description:    r.TradeStatus.Description,
			Icon:           r.TradeStatus.Icon,
			AmountInvested: float64(r.TradeStatus.AmountInvested),
		},
	}
	if r.TradeStatus.NetProfit != nil {
		v := float64(*r.TradeStatus.NetProfit)
		cs.Status.NetProfit = &v
	}
	return cs, nil
}

// allowedPeriods are the only values market-conditions accepts (days).
var allowedPeriods = map[int]bool{1: true, 7: true, 30: true, 90: true, 365: true}

// FetchMarketConditions returns the freshest spread (from the 1-day, highest-
// resolution feed) plus a history series over historyPeriod days for a sparkline.
func (s *LiveSource) FetchMarketConditions(ctx context.Context, historyPeriod int) (*MarketConditions, error) {
	if !allowedPeriods[historyPeriod] {
		return nil, fmt.Errorf("period %d not allowed (use 1, 7, 30, 90, or 365)", historyPeriod)
	}

	// Freshest current spread: the 1-day feed has the finest resolution.
	curRaw, err := s.authGet(ctx, "/api/market-conditions/most_recent/?period=1")
	if err != nil {
		return nil, err
	}
	var cur marketPointJSON
	if err := json.Unmarshal(curRaw, &cur); err != nil {
		return nil, fmt.Errorf("decode most_recent: %w", err)
	}

	histRaw, err := s.authGet(ctx, fmt.Sprintf("/api/market-conditions/history/?period=%d", historyPeriod))
	if err != nil {
		return nil, err
	}
	var hist []marketPointJSON
	if err := json.Unmarshal(histRaw, &hist); err != nil {
		return nil, fmt.Errorf("decode history: %w", err)
	}
	points := make([]MarketPoint, len(hist))
	for i, h := range hist {
		points[i] = h.toPoint()
	}

	return &MarketConditions{Current: cur.toPoint(), History: points, Period: historyPeriod}, nil
}

// authGet does an authenticated GET against the data host, re-minting the token
// once on a 401 (tokens expire ~hourly), mirroring Fetch's recovery.
func (s *LiveSource) authGet(ctx context.Context, path string) ([]byte, error) {
	if err := s.ensureToken(ctx); err != nil {
		return nil, err
	}
	raw, err := s.rawGet(ctx, path)
	if errors.Is(err, errUnauthorized) && s.Username != "" && s.Password != "" {
		s.Token = ""
		clearToken(tokenCacheFile(s.Username)) // the cached token is stale
		if err = s.ensureToken(ctx); err != nil {
			return nil, err
		}
		raw, err = s.rawGet(ctx, path)
	}
	return raw, err
}

func (s *LiveSource) rawGet(ctx context.Context, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.BaseURL+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", authHeader(s.Token))
	req.Header.Set("Accept", "application/json")

	resp, err := s.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, errUnauthorized
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", path, resp.Status)
	}
	return raw, nil
}
