package model

import (
	"math"
	"strings"
	"testing"
)

func TestCSVParsesAndRecomputesProfit(t *testing.T) {
	cs, err := parseCSV(strings.NewReader(sampleCSV))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(cs) != 2 {
		t.Fatalf("got %d cycles, want 2", len(cs))
	}
	// Sorted by start ascending: JW000001 (2024-09-11) first.
	first := cs[0]
	if first.Code != "JW000001" {
		t.Fatalf("first code %q, want JW000001", first.Code)
	}
	// NetProfit must be recomputed from ZarOut-ZarIn, ignoring the deliberately
	// wrong stored "Net Profit" column (999.99).
	want := 105729.50 - 105000.00
	if math.Abs(first.NetProfit-want) > 0.001 {
		t.Errorf("NetProfit = %.2f, want %.2f (recomputed, not stored)", first.NetProfit, want)
	}
	if first.HoldDays() != 1 {
		t.Errorf("HoldDays = %d, want 1", first.HoldDays())
	}
}

// sampleCSV mirrors the real export: blank spacer columns E and H, and a stored
// Net Profit that disagrees with ZarOut-ZarIn (to prove we recompute).
const sampleCSV = `Cycle Code,Trade Type,Start Date,End Date,,ZAR in,ZAR out,,Net Profit,Net Return
JW000002,Hedged,2024-09-13,2024-09-16,,106232.77,106772.38,,999.99,0.51%
JW000001,Hedged,2024-09-11,2024-09-11,,105000.00,105729.50,,999.99,0.69%
`

func TestAuthHeaderNormalises(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"abc123", "Token abc123"},
		{"Token abc123", "Token abc123"},
		{"  abc123  ", "Token abc123"},
	} {
		if got := authHeader(tc.in); got != tc.want {
			t.Errorf("authHeader(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
