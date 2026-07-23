package format

import "testing"

func TestMoney(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{0, "R0.00"},
		{0.5, "R0.50"},
		{450, "R450.00"},
		{1234.56, "R1,234.56"},
		{100450, "R100,450.00"},
		{1234567.89, "R1,234,567.89"},
		{999.999, "R1,000.00"}, // rounding carry into the whole part
		{-1234.5, "-R1,234.50"},
		{-0.005, "-R0.01"},
	}
	for _, c := range cases {
		if got := Money(c.in); got != c.want {
			t.Errorf("Money(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestPercent(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{0.0625, "6.25%"},
		{0, "0.00%"},
		{-0.0123, "-1.23%"},
		{1, "100.00%"},
	}
	for _, c := range cases {
		if got := Percent(c.in); got != c.want {
			t.Errorf("Percent(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestPoints(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{0.199, "19.9pts"},
		{0, "0.0pts"},
		{-0.005, "-0.5pts"},
	}
	for _, c := range cases {
		if got := Points(c.in); got != c.want {
			t.Errorf("Points(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestSpreadFmt(t *testing.T) {
	// Spread is already in percent units — no scaling by 100.
	cases := []struct {
		in   float64
		want string
	}{
		{0.82, "0.82%"},
		{1.5, "1.50%"},
		{0, "0.00%"},
	}
	for _, c := range cases {
		if got := SpreadFmt(c.in); got != c.want {
			t.Errorf("SpreadFmt(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestSparkline(t *testing.T) {
	cases := []struct {
		name   string
		values []float64
		width  int
		want   string
	}{
		// Eight evenly spaced values map one-to-one onto the eight glyph levels.
		{"ramp", []float64{1, 2, 3, 4, 5, 6, 7, 8}, 8, "▁▂▃▄▅▆▇█"},
		// Zero span renders the lowest glyph everywhere.
		{"flat", []float64{5, 5, 5}, 3, "▁▁▁"},
		// More values than width: buckets average to 1.5 and 3.5 (min/max).
		{"resampled", []float64{1, 2, 3, 4}, 2, "▁█"},
		{"empty", nil, 8, ""},
		{"zero width", []float64{1, 2}, 0, ""},
	}
	for _, c := range cases {
		if got := Sparkline(c.values, c.width); got != c.want {
			t.Errorf("%s: Sparkline(%v, %d) = %q, want %q", c.name, c.values, c.width, got, c.want)
		}
	}
}
