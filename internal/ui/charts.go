package ui

import (
	"strconv"
	"strings"
	"time"

	"github.com/wolffshots/fftui/internal/analytics"
	"github.com/wolffshots/fftui/internal/model"
)

var sparkLevels = []rune("▁▂▃▄▅▆▇█")

// sparkline renders values as block glyphs scaled between the series min and
// max. If there are more values than width, they are sampled down to width
// points; fewer values render as-is.
func sparkline(values []float64, width int) string {
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

type chartsModel struct {
	cycles []model.Cycle
	now    time.Time
	rates  analytics.Rates
	width  int
	height int
}

func newChartsModel(now time.Time, rates analytics.Rates) chartsModel {
	return chartsModel{now: now, rates: rates}
}

func (m *chartsModel) setCycles(cs []model.Cycle) { m.cycles = cs }
func (m *chartsModel) setSize(w, h int)           { m.width, m.height = w, h }

func (m chartsModel) view() string {
	if len(m.cycles) == 0 {
		return dimStyle.Render("no data")
	}
	w := m.width - 4
	if w < 10 {
		w = 10
	}
	if w > 120 {
		w = 120
	}

	// Series 1: per-cycle return % over time.
	returns := make([]float64, len(m.cycles))
	for i, c := range m.cycles {
		returns[i] = c.Return()
	}

	// Series 2: monthly annualised rate over time (active months). Partial
	// months are excluded — their annualised figure is flagged unreliable and
	// would distort the sparkline's min/max scaling.
	monthly := analytics.Buckets(m.cycles, analytics.Month, m.now, false, m.rates)
	monthlyAnn := make([]float64, 0, len(monthly))
	for _, bk := range monthly {
		if bk.Partial {
			continue
		}
		monthlyAnn = append(monthlyAnn, bk.Annualised)
	}

	// Series 3: cumulative profit (running total).
	cum := make([]float64, len(m.cycles))
	var run float64
	for i, c := range m.cycles {
		run += c.NetProfit
		cum[i] = run
	}

	var b strings.Builder
	b.WriteString(m.chart("Return % per cycle (chronological)", returns, w, percent) + "\n\n")
	b.WriteString(m.chart("Monthly annualised rate", monthlyAnn, w, percent) + "\n\n")
	b.WriteString(m.chart("Cumulative profit", cum, w, money))
	return b.String()
}

func (m chartsModel) chart(title string, series []float64, w int, fmtFn func(float64) string) string {
	if len(series) == 0 {
		return titleStyle.Render(title) + "\n" + dimStyle.Render("  (no points)")
	}
	min, max := series[0], series[0]
	for _, v := range series {
		if v < min {
			min = v
		}
		if v > max {
			max = v
		}
	}
	latest := series[len(series)-1]

	lineStyle := positiveStyle
	if latest < 0 {
		lineStyle = negativeStyle
	}
	line := lineStyle.Render(sparkline(series, w))
	labels := dimStyle.Render("  min ") + valueStyle.Render(fmtFn(min)) +
		dimStyle.Render("  max ") + valueStyle.Render(fmtFn(max)) +
		dimStyle.Render("  latest ") + valueStyle.Render(fmtFn(latest)) +
		dimStyle.Render("  n=") + valueStyle.Render(strconv.Itoa(len(series)))
	return titleStyle.Render(title) + "\n" + line + "\n" + labels
}
