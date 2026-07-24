package webui

import (
	"bytes"
	"context"
	"html/template"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wolffshots/fftui/internal/analytics"
	"github.com/wolffshots/fftui/internal/data"
	"github.com/wolffshots/fftui/internal/model"
)

// testOptions uses the same rates/allowances/fees as ui_test.go, so the
// figures asserted here match the TUI reference tests (and the README).
func testOptions(token string) Options {
	return Options{
		Token:   token,
		Version: "test",
		CSVMode: true,
		Rates:   analytics.Rates{Idle: 0.06, Tax: 0.41},
		Allow:   analytics.Allowances{SDALimit: 2_000_000, FIALimit: 10_000_000},
		Fees:    analytics.DefaultFees(),
	}
}

// testNow is the same fixed date ui_test.go uses, so date-relative figures
// (trend windows, in-progress buckets) are deterministic forever.
var testNow = time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)

func newTestServer(t *testing.T, token string, refresh bool) (*Server, *data.Service) {
	t.Helper()
	svc := data.NewService(model.NewCSVSource("../../testdata/cycles.csv"))
	svc.SetNow(func() time.Time { return testNow })
	if refresh {
		if _, err := svc.Refresh(context.Background()); err != nil {
			t.Fatalf("refresh: %v", err)
		}
	}
	s, err := New(svc, testOptions(token))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s, svc
}

func get(t *testing.T, s *Server, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
	return rec
}

// firstRowCode extracts the cycle code of the first table row.
func firstRowCode(t *testing.T, body string) string {
	t.Helper()
	i := strings.Index(body, `<td class="code">`)
	if i < 0 {
		t.Fatal("no table rows in body")
	}
	rest := body[i:]
	j := strings.Index(rest, `">FX`)
	if j < 0 {
		t.Fatalf("no code in first row: %.120s", rest)
	}
	end := strings.Index(rest[j+2:], "<")
	return rest[j+2 : j+2+end]
}

// TestRoutes drives every GET route and checks each renders its page marker.
func TestRoutes(t *testing.T) {
	s, _ := newTestServer(t, "", true)
	cases := []struct {
		path, marker string
	}{
		{"/cycles", "FX0001"},
		{"/cycles/FX0001", "Hold days"},
		{"/analytics", "money-weighted (IRR)"},
		{"/analytics?gran=quarter", "variance"},
		{"/charts", "Cumulative profit"},
		{"/live", "only available from the live API"},
		{"/static/style.css", "Phase C"},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			rec := get(t, s, tc.path)
			if rec.Code != http.StatusOK {
				t.Fatalf("status %d", rec.Code)
			}
			body := rec.Body.String()
			if strings.TrimSpace(body) == "" {
				t.Fatal("empty body")
			}
			if !strings.Contains(body, tc.marker) {
				t.Errorf("missing marker %q", tc.marker)
			}
		})
	}

	rec := get(t, s, "/")
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/cycles" {
		t.Errorf("/ should 302 to /cycles, got %d %q", rec.Code, rec.Header().Get("Location"))
	}
}

// TestCyclesTable checks row count, default order (newest first) and the
// lifetime footer figures pinned by the README / analytics regression tests.
func TestCyclesTable(t *testing.T) {
	s, _ := newTestServer(t, "", true)
	body := get(t, s, "/cycles").Body.String()

	if n := strings.Count(body, `<td class="code">`); n != 43 {
		t.Errorf("want 43 rows, got %d", n)
	}
	if code := firstRowCode(t, body); code != "FX0043" {
		t.Errorf("default sort should be newest first, first row %s", code)
	}
	// Lifetime footer: annualised → +idle@6% → net@41%, plus total profit.
	for _, want := range []string{"9.78%", "15.13%", "8.91%", "R19,422.50", "+idle@6.00%", "net@41.00%"} {
		if !strings.Contains(body, want) {
			t.Errorf("footer missing %q", want)
		}
	}
}

// TestCyclesFilterAndSort: q narrows the set (and hides the annualised
// footer, same rule as the TUI); sort params reorder.
func TestCyclesFilterAndSort(t *testing.T) {
	s, _ := newTestServer(t, "", true)

	body := get(t, s, "/cycles?q=FX001").Body.String()
	if n := strings.Count(body, `<td class="code">`); n != 10 {
		t.Errorf("q=FX001 should match 10 rows (FX0010–FX0019), got %d", n)
	}
	if !strings.Contains(body, "annualised n/a (filtered)") {
		t.Error("filtered footer should hide annualised rates")
	}
	if strings.Contains(body, "+idle@") {
		t.Error("filtered footer should not show the +idle rate")
	}

	body = get(t, s, "/cycles?sort=profit&dir=desc").Body.String()
	if code := firstRowCode(t, body); code != "FX0008" {
		t.Errorf("sort=profit desc: first row should be FX0008 (max profit), got %s", code)
	}
	body = get(t, s, "/cycles?sort=start&dir=asc").Body.String()
	if code := firstRowCode(t, body); code != "FX0001" {
		t.Errorf("sort=start asc: first row should be FX0001, got %s", code)
	}
}

// TestDetail checks the detail page fields and the 404 path.
func TestDetail(t *testing.T) {
	s, _ := newTestServer(t, "", true)
	body := get(t, s, "/cycles/FX0001").Body.String()
	for _, want := range []string{"2024-09-10", "Hold days", "best-case, no-idle", "R100,000.00", "R100,450.00"} {
		if !strings.Contains(body, want) {
			t.Errorf("detail missing %q", want)
		}
	}

	rec := get(t, s, "/cycles/NOPE")
	if rec.Code != http.StatusNotFound {
		t.Errorf("unknown code: want 404, got %d", rec.Code)
	}
}

// TestAnalytics checks granularity buckets and the dead-bucket toggle
// (the fixture has a dead month — Nov 2024 — so dead=1 adds buckets).
func TestAnalytics(t *testing.T) {
	s, _ := newTestServer(t, "", true)

	body := get(t, s, "/analytics?gran=month").Body.String()
	// Note: values injected from Go get HTML-escaped, so "+Idle@6%" appears
	// as "&#43;Idle@6%" in the source; match past the plus.
	for _, want := range []string{"2024-09", "Annualised%", "Idle@6%", "Net@41%", "bootstrap 90% band", "return trend", "taxable profit"} {
		if !strings.Contains(body, want) {
			t.Errorf("analytics missing %q", want)
		}
	}
	active := strings.Count(body, `class="bucket`)

	deadBody := get(t, s, "/analytics?gran=month&dead=1").Body.String()
	dead := strings.Count(deadBody, `class="bucket`)
	if dead <= active {
		t.Errorf("dead=1 should add empty buckets: active=%d dead=%d", active, dead)
	}
	if !strings.Contains(deadBody, "incl. dead buckets") {
		t.Error("dead=1 should flip the scope label")
	}
	// CSV mode: the planning strip shows the inferred-usage note, not live SDA/FIA.
	if !strings.Contains(body, "usage inferred from cycle history") {
		t.Error("CSV-mode planning strip should show the inferred-usage note")
	}
}

// TestCharts checks the three chart titles and that a sparkline rendered.
func TestCharts(t *testing.T) {
	s, _ := newTestServer(t, "", true)
	body := get(t, s, "/charts").Body.String()
	for _, want := range []string{"Return % per cycle (chronological)", "Monthly annualised rate", "Cumulative profit"} {
		if !strings.Contains(body, want) {
			t.Errorf("charts missing %q", want)
		}
	}
	if !strings.ContainsAny(body, "▁▂▃▄▅▆▇█") {
		t.Error("charts missing sparkline block runes")
	}
}

// TestNoData: before any refresh, every page must render the no-data state
// with a 200, never a 500.
func TestNoData(t *testing.T) {
	s, _ := newTestServer(t, "", false)
	for _, path := range []string{"/cycles", "/cycles/FX0001", "/analytics", "/charts", "/live"} {
		rec := get(t, s, path)
		if rec.Code != http.StatusOK {
			t.Errorf("%s: want 200, got %d", path, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "no data yet") {
			t.Errorf("%s: missing no-data state", path)
		}
	}
}

// TestRefresh: POST /refresh redirects back to the Referer (path+query only)
// or /cycles, and populates the service.
func TestRefresh(t *testing.T) {
	s, svc := newTestServer(t, "", false)

	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest("POST", "/refresh", nil))
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/cycles" {
		t.Errorf("want 303 to /cycles, got %d %q", rec.Code, rec.Header().Get("Location"))
	}
	if snap, err := svc.Latest(); snap == nil || err != nil {
		t.Errorf("refresh should have populated the service (snap=%v err=%v)", snap != nil, err)
	}

	req := httptest.NewRequest("POST", "/refresh", nil)
	req.Header.Set("Referer", "http://example.com/analytics?gran=month")
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if loc := rec.Header().Get("Location"); loc != "/analytics?gran=month" {
		t.Errorf("want redirect to referer path, got %q", loc)
	}

	// A scheme-relative path (//host/...) in the Referer must not become an
	// open redirect — it falls back to /cycles.
	for _, ref := range []string{"http://example.com//evil.com/pwn", `http://example.com/\evil.com`, "not a url"} {
		req = httptest.NewRequest("POST", "/refresh", nil)
		req.Header.Set("Referer", ref)
		rec = httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		if loc := rec.Header().Get("Location"); loc != "/cycles" {
			t.Errorf("Referer %q: want fallback to /cycles, got %q", ref, loc)
		}
	}
}

// TestAuth exercises the token middleware: cookie, bearer, one-shot query
// param (cookie + redirect with the token stripped), and the 401 paths.
func TestAuth(t *testing.T) {
	s, _ := newTestServer(t, "testtok", true)

	t.Run("bare request 401", func(t *testing.T) {
		rec := get(t, s, "/cycles")
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("want 401, got %d", rec.Code)
		}
		if strings.Contains(rec.Body.String(), "FX0001") {
			t.Error("401 page leaked content")
		}
	})

	t.Run("query token sets cookie and strips param", func(t *testing.T) {
		rec := get(t, s, "/cycles?sort=profit&token=testtok")
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("want 303, got %d", rec.Code)
		}
		loc := rec.Header().Get("Location")
		if strings.Contains(loc, "token") {
			t.Errorf("redirect must strip the token param, got %q", loc)
		}
		if !strings.Contains(loc, "sort=profit") {
			t.Errorf("redirect should keep other params, got %q", loc)
		}
		cookies := rec.Result().Cookies()
		if len(cookies) != 1 || cookies[0].Name != tokenCookie || cookies[0].Value != "testtok" {
			t.Fatalf("want fftui_token cookie, got %v", cookies)
		}
		if !cookies[0].HttpOnly {
			t.Error("cookie must be HttpOnly")
		}
		if cookies[0].SameSite != http.SameSiteLaxMode {
			t.Errorf("cookie must be SameSite=Lax, got %v", cookies[0].SameSite)
		}
	})

	t.Run("cookie passes", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/cycles", nil)
		req.AddCookie(&http.Cookie{Name: tokenCookie, Value: "testtok"})
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("want 200, got %d", rec.Code)
		}
	})

	t.Run("bearer passes", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/cycles", nil)
		req.Header.Set("Authorization", "Bearer testtok")
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("want 200, got %d", rec.Code)
		}
	})

	t.Run("wrong token 401 and no cookie", func(t *testing.T) {
		for _, path := range []string{"/cycles?token=wrong", "/cycles"} {
			req := httptest.NewRequest("GET", path, nil)
			req.AddCookie(&http.Cookie{Name: tokenCookie, Value: "wrong"})
			req.Header.Set("Authorization", "Bearer wrong")
			rec := httptest.NewRecorder()
			s.ServeHTTP(rec, req)
			if rec.Code != http.StatusUnauthorized {
				t.Errorf("%s: want 401, got %d", path, rec.Code)
			}
			if len(rec.Result().Cookies()) != 0 {
				t.Errorf("%s: wrong token must not set a cookie", path)
			}
		}
	})

	t.Run("static exempt", func(t *testing.T) {
		rec := get(t, s, "/static/style.css")
		if rec.Code != http.StatusOK {
			t.Fatalf("static should not need auth, got %d", rec.Code)
		}
	})
}

// TestConcurrentAccess hammers reads and refreshes together; meaningful under
// -race (the snapshot must never be mutated by a request).
func TestConcurrentAccess(t *testing.T) {
	s, svc := newTestServer(t, "", true)
	paths := []string{
		"/cycles", "/cycles?sort=profit&dir=desc", "/cycles?q=FX001",
		"/analytics?gran=month", "/analytics?gran=year&dead=1",
		"/charts", "/live", "/cycles/FX0001",
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				path := paths[(i+j)%len(paths)]
				rec := get(t, s, path)
				if rec.Code != http.StatusOK {
					t.Errorf("%s: status %d", path, rec.Code)
					return
				}
			}
		}(i)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for j := 0; j < 5; j++ {
			if _, err := svc.Refresh(context.Background()); err != nil {
				t.Errorf("refresh: %v", err)
			}
		}
	}()
	wg.Wait()
}

// TestTooltips checks the jargon tooltips render on each page: /analytics
// carries the IRR and sweet-spot explanations, /cycles the footer terms. The
// live page has no live data in CSV-mode tests, so its template is executed
// directly with a populated view-model.
func TestTooltips(t *testing.T) {
	s, _ := newTestServer(t, "", true)

	body := get(t, s, "/analytics").Body.String()
	for _, want := range []string{
		`role="tooltip"`,
		"weights every stretch by the capital actually in it", // irr
		"allowance pool runs out before the year does",        // sweet-spot
	} {
		if !strings.Contains(body, want) {
			t.Errorf("analytics missing tooltip content %q", want)
		}
	}

	if !strings.Contains(get(t, s, "/cycles").Body.String(), `role="tooltip"`) {
		t.Error("cycles page missing tooltips")
	}

	var buf bytes.Buffer
	vm := liveVM{
		baseVM: baseVM{Title: "Live", Active: "live"},
		Client: &liveClientVM{Label: "idle"},
	}
	if err := s.pages["live"].ExecuteTemplate(&buf, "base.html", vm); err != nil {
		t.Fatalf("render live: %v", err)
	}
	live := buf.String()
	if !strings.Contains(live, `role="tooltip"`) {
		t.Error("live page missing tooltips")
	}
	if !strings.Contains(live, "Single Discretionary Allowance") {
		t.Error("live page missing the SDA tip text")
	}
}

// TestTipKeys guards the tips map against template drift: every key used as a
// {{tip "…"}} literal in a template must exist, as must the keys handlers
// inject as data. Unknown keys must degrade to the plain escaped label.
func TestTipKeys(t *testing.T) {
	re := regexp.MustCompile(`\{\{\s*tip\s+"([^"]+)"`)
	entries, err := content.ReadDir("templates")
	if err != nil {
		t.Fatalf("read templates dir: %v", err)
	}
	seen := 0
	for _, e := range entries {
		data, err := content.ReadFile("templates/" + e.Name())
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		for _, m := range re.FindAllStringSubmatch(string(data), -1) {
			seen++
			if _, ok := tips[m[1]]; !ok {
				t.Errorf("%s uses tip key %q with no entry in tips", e.Name(), m[1])
			}
		}
	}
	if seen == 0 {
		t.Error("no tip keys found in templates — scan regexp broken?")
	}
	// Keys passed as view-model data rather than template literals.
	for _, k := range []string{
		"variance", "variance-idle", "variance-net", // varianceVM.Key
		"type", "zar-in", "zar-out", "return", "days", // colVM.Key
		"monthly-annualised", // chartVM.Key
	} {
		if _, ok := tips[k]; !ok {
			t.Errorf("handler-injected tip key %q missing from tips", k)
		}
	}

	if got := tipHTML("no-such-key", "a<b"); got != template.HTML("a&lt;b") {
		t.Errorf("unknown key should return the escaped label, got %q", got)
	}
}

// TestTemplatesParse makes sure a bad Options set still constructs (template
// parsing is validated at New) — mostly a canary for template syntax errors.
func TestTemplatesParse(t *testing.T) {
	svc := data.NewService(model.NewCSVSource("../../testdata/cycles.csv"))
	if _, err := New(svc, Options{}); err != nil {
		t.Fatalf("New with zero options: %v", err)
	}
}
