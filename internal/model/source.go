package model

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

// CycleSource is the single seam all data loading goes through.
type CycleSource interface {
	Fetch(ctx context.Context) ([]Cycle, error)
}

const dateLayout = "2006-01-02"

// sortByStart sorts cycles chronologically by StartDate ascending, in place,
// and returns the slice. This is the canonical internal order.
func sortByStart(cs []Cycle) []Cycle {
	sort.SliceStable(cs, func(i, j int) bool {
		return cs[i].StartDate.Before(cs[j].StartDate)
	})
	return cs
}

// ---------------------------------------------------------------------------
// CSVSource
// ---------------------------------------------------------------------------

// CSVSource reads cycles from a CSV export. The export has blank spacer columns
// (E and H) and a "Net Return" column we ignore; NetProfit is recomputed from
// ZarOut-ZarIn to stay honest.
type CSVSource struct {
	Path string
}

func NewCSVSource(path string) *CSVSource { return &CSVSource{Path: path} }

func (s *CSVSource) Fetch(ctx context.Context) ([]Cycle, error) {
	f, err := os.Open(s.Path)
	if err != nil {
		return nil, fmt.Errorf("open csv: %w", err)
	}
	defer f.Close()
	return parseCSV(f)
}

// Column layout of the export:
//
//	0 Cycle Code | 1 Trade Type | 2 Start Date | 3 End Date | 4 (blank) |
//	5 ZAR in | 6 ZAR out | 7 (blank) | 8 Net Profit | 9 Net Return
func parseCSV(r io.Reader) ([]Cycle, error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1 // tolerate ragged rows
	rows, err := cr.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("read csv: %w", err)
	}
	if len(rows) < 2 {
		return nil, fmt.Errorf("csv has no data rows")
	}

	var cycles []Cycle
	for i, row := range rows[1:] { // skip header
		if len(row) < 7 {
			continue // blank / malformed line
		}
		code := strings.TrimSpace(row[0])
		if code == "" {
			continue
		}
		start, err := time.Parse(dateLayout, strings.TrimSpace(row[2]))
		if err != nil {
			return nil, fmt.Errorf("row %d: bad start date %q: %w", i+2, row[2], err)
		}
		end, err := time.Parse(dateLayout, strings.TrimSpace(row[3]))
		if err != nil {
			return nil, fmt.Errorf("row %d: bad end date %q: %w", i+2, row[3], err)
		}
		zarIn, err := parseMoney(row[5])
		if err != nil {
			return nil, fmt.Errorf("row %d: bad ZAR in %q: %w", i+2, row[5], err)
		}
		zarOut, err := parseMoney(row[6])
		if err != nil {
			return nil, fmt.Errorf("row %d: bad ZAR out %q: %w", i+2, row[6], err)
		}
		cycles = append(cycles, Cycle{
			Code:      code,
			TradeType: strings.TrimSpace(row[1]),
			StartDate: start,
			EndDate:   end,
			ZarIn:     zarIn,
			ZarOut:    zarOut,
			NetProfit: zarOut - zarIn, // recompute; ignore stored value
		})
	}
	return sortByStart(cycles), nil
}

// parseMoney strips thousands separators, currency symbols and whitespace.
func parseMoney(s string) (float64, error) {
	s = strings.TrimSpace(s)
	s = strings.NewReplacer(",", "", "R", "", "ZAR", "", " ", "").Replace(s)
	if s == "" {
		return 0, nil
	}
	return strconv.ParseFloat(s, 64)
}
