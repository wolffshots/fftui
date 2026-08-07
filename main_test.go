package main

import (
	"testing"
	"time"
)

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

// TestEnvDuration: unset falls back to the default, a Go duration parses, and
// a unitless or garbage value falls back (with a stderr warning) rather than
// aborting — the flag still overrides, but the typo can't go unannounced.
func TestEnvDuration(t *testing.T) {
	if d := envDuration("FF_TEST_INTERVAL", 0); d != 0 {
		t.Fatalf("unset = %v, want 0", d)
	}
	t.Setenv("FF_TEST_INTERVAL", "5m")
	if d := envDuration("FF_TEST_INTERVAL", 0); d != 5*time.Minute {
		t.Fatalf("5m = %v, want 5m0s", d)
	}
	t.Setenv("FF_TEST_INTERVAL", "30") // no unit — not a valid Go duration
	if d := envDuration("FF_TEST_INTERVAL", time.Minute); d != time.Minute {
		t.Fatalf("unitless 30 = %v, want the 1m0s default", d)
	}
}
