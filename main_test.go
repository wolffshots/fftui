package main

import "testing"

// TestParseDate: empty is an open bound (zero time), full ISO dates parse, and
// anything else errors mentioning the offending flag.
func TestParseDate(t *testing.T) {
	if d, err := parseDate("--from", ""); err != nil || !d.IsZero() {
		t.Fatalf("empty = %v, %v; want zero, nil", d, err)
	}
	d, err := parseDate("--from", "2026-03-01")
	if err != nil || d.Format("2006-01-02") != "2026-03-01" {
		t.Fatalf("2026-03-01 = %v, %v", d, err)
	}
	if _, err := parseDate("--to", "01/03/2026"); err == nil {
		t.Fatal("non-ISO date should error")
	}
}
