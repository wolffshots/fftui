package model

import (
	"encoding/json"
	"math"
	"testing"
)

// sampleClientJSON mirrors the get_client response shape with fictional values,
// including the null net_profit that appears while a cycle is still queued.
const sampleClientJSON = `{
  "id": 1234,
  "funds_available": 119422.50,
  "funds_last_updated": {"string": "Last updated 12:00 on 07 Jul 2026"},
  "minimum_return": 0.1,
  "allowance_available": {"sda": 500000.0, "fia": 4200000.0},
  "total_profit": 19422.50,
  "trade_status_v2": {
    "status_slug": "trade_loaded",
    "status_secondary_text": "Awaiting market conditions",
    "status_description": "Your funds are currently queued to trade.",
    "status_description_icon": "info",
    "amount_invested": 119422.50,
    "net_profit": null
  }
}`

func TestClientResponseParses(t *testing.T) {
	var r clientResponse
	if err := json.Unmarshal([]byte(sampleClientJSON), &r); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if r.TradeStatus.SecondaryText != "Awaiting market conditions" {
		t.Errorf("secondary text = %q", r.TradeStatus.SecondaryText)
	}
	if r.TradeStatus.NetProfit != nil {
		t.Errorf("net_profit should be nil (JSON null), got %v", float64(*r.TradeStatus.NetProfit))
	}
	if math.Abs(float64(r.FundsAvailable)-119422.50) > 0.001 {
		t.Errorf("funds_available = %v", float64(r.FundsAvailable))
	}
	if math.Abs(float64(r.MinimumReturn)-0.1) > 1e-9 {
		t.Errorf("minimum_return = %v", float64(r.MinimumReturn))
	}
}

// TestFlexFloatHandlesStringOrNumber guards the market fields, which arrive as
// bare JSON numbers here but as strings elsewhere in the API.
func TestFlexFloatHandlesStringOrNumber(t *testing.T) {
	raw := `{"datetime":"t","spread":0.82,"local_price":"16.59","offshore_price":0.999,"exchange_rate":"16.47"}`
	var m marketPointJSON
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	p := m.toPoint()
	if math.Abs(p.Spread-0.82) > 1e-9 || math.Abs(p.LocalPrice-16.59) > 1e-9 ||
		math.Abs(p.OffshorePrice-0.999) > 1e-9 || math.Abs(p.ExchangeRate-16.47) > 1e-9 {
		t.Errorf("point = %+v", p)
	}
}

// TestFetchMarketConditionsRejectsBadPeriod avoids a doomed network round-trip.
func TestFetchMarketConditionsRejectsBadPeriod(t *testing.T) {
	s := NewLiveSource()
	if _, err := s.FetchMarketConditions(nil, 3); err == nil {
		t.Fatal("expected error for disallowed period 3")
	}
}
