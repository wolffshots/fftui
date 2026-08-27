// Package format holds the pure text formatters shared by the TUI (and any
// other front end): money/percent/points rendering and plain-rune sparklines.
// Nothing here emits ANSI styling — colour stays in the ui package.
package format

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Money formats a ZAR amount as R1,234.56.
func Money(v float64) string {
	neg := v < 0
	if neg {
		v = -v
	}
	whole := int64(v)
	frac := int64(math.Round((v - float64(whole)) * 100))
	if frac == 100 { // rounding carry
		whole++
		frac = 0
	}
	s := "R" + groupThousands(whole) + "." + fmt.Sprintf("%02d", frac)
	if neg {
		return "-" + s
	}
	return s
}

// groupThousands inserts comma separators into a non-negative integer.
func groupThousands(n int64) string {
	s := strconv.FormatInt(n, 10)
	if len(s) <= 3 {
		return s
	}
	var b strings.Builder
	pre := len(s) % 3
	if pre > 0 {
		b.WriteString(s[:pre])
		if len(s) > pre {
			b.WriteByte(',')
		}
	}
	for i := pre; i < len(s); i += 3 {
		b.WriteString(s[i : i+3])
		if i+3 < len(s) {
			b.WriteByte(',')
		}
	}
	return b.String()
}

// Percent formats a fractional value as a 2dp percentage, e.g. 6.25%.
func Percent(frac float64) string {
	return fmt.Sprintf("%.2f%%", frac*100)
}

// Points formats a fractional spread as percentage points, e.g. 19.9pts.
func Points(frac float64) string {
	return fmt.Sprintf("%.1fpts", frac*100)
}

// SpreadFmt formats a market spread that is ALREADY in percent units (the API
// returns 0.82 to mean 0.82%), so unlike Percent() it does not scale by 100.
func SpreadFmt(v float64) string {
	return fmt.Sprintf("%.2f%%", v)
}

var sparkLevels = []rune("▁▂▃▄▅▆▇█")

// Sparkline renders values as block glyphs scaled between the series min and
// max. If there are more values than width, they are sampled down to width
// points; fewer values render as-is.
func Sparkline(values []float64, width int) string {
	if len(values) == 0 || width <= 0 {
		return ""
	}
	pts := resample(values, width)

	min, max := pts[0], pts[0]
	for _, v := range pts {
		if v < min {
			min = v
		}
		if v > max {
			max = v
		}
	}
	span := max - min
	var b strings.Builder
	for _, v := range pts {
		var level int
		if span > 0 {
			level = int((v - min) / span * float64(len(sparkLevels)-1))
		}
		if level < 0 {
			level = 0
		}
		if level >= len(sparkLevels) {
			level = len(sparkLevels) - 1
		}
		b.WriteRune(sparkLevels[level])
	}
	return b.String()
}

// resample reduces or passes through values to at most width points by
// averaging each output bucket's source range.
func resample(values []float64, width int) []float64 {
	if len(values) <= width {
		return values
	}
	out := make([]float64, width)
	for i := 0; i < width; i++ {
		start := i * len(values) / width
		end := (i + 1) * len(values) / width
		if end <= start {
			end = start + 1
		}
		var sum float64
		for j := start; j < end; j++ {
			sum += values[j]
		}
		out[i] = sum / float64(end-start)
	}
	return out
}
