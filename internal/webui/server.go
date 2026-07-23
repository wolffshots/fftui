// Package webui serves the TUI's views over HTTP as server-rendered HTML.
// Every page is functional without JavaScript: sort, filter, granularity and
// the dead-bucket toggle all live in query parameters. Templates consume only
// view-model structs plus the pure formatters in internal/format — nothing
// from internal/ui (no ANSI) ever reaches HTML.
package webui

import (
	"bytes"
	"embed"
	"fmt"
	"html"
	"html/template"
	"net/http"
	"sync"
	"time"

	"github.com/wolffshots/fftui/internal/analytics"
	"github.com/wolffshots/fftui/internal/data"
	"github.com/wolffshots/fftui/internal/format"
)

//go:embed templates static
var content embed.FS

// bootstrapResamples matches the TUI's analytics view (ui/analytics.go
// setCycles), so both front ends quote the same band.
const bootstrapResamples = 10_000

// chartWidth is the sparkline width used by the charts and live pages.
const chartWidth = 56

// Options configures the web front end. Rates/Allow/Fees are the same values
// handed to ui.New so both front ends compute identical figures.
type Options struct {
	Token   string // access token (FF_WEB_TOKEN); empty disables auth
	Version string
	CSVMode bool
	Rates   analytics.Rates
	Allow   analytics.Allowances
	Fees    analytics.Fees
}

// Server is the web front end. It implements http.Handler.
type Server struct {
	svc     *data.Service
	opts    Options
	pages   map[string]*template.Template
	handler http.Handler

	// Bootstrap is too heavy to redo per request; memoise per snapshot,
	// keyed by its FetchedAt (a new refresh produces a new key).
	bootMu  sync.Mutex
	bootKey time.Time
	boot    analytics.BootstrapResult
}

// New builds the server, parsing all templates up front so a broken template
// fails at construction rather than on first request.
func New(svc *data.Service, opts Options) (*Server, error) {
	s := &Server{svc: svc, opts: opts, pages: map[string]*template.Template{}}

	funcs := template.FuncMap{
		"money":     format.Money,
		"percent":   format.Percent,
		"points":    format.Points,
		"spreadFmt": format.SpreadFmt,
		"spark":     format.Sparkline,
		"signClass": signClass,
	}
	for _, page := range []string{"cycles", "detail", "analytics", "charts", "live"} {
		t, err := template.New("base.html").Funcs(funcs).ParseFS(content,
			"templates/base.html", "templates/"+page+".html")
		if err != nil {
			return nil, fmt.Errorf("parse %s templates: %w", page, err)
		}
		s.pages[page] = t
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/cycles", http.StatusFound)
	})
	mux.HandleFunc("GET /cycles", s.handleCycles)
	mux.HandleFunc("GET /cycles/{code}", s.handleDetail)
	mux.HandleFunc("GET /analytics", s.handleAnalytics)
	mux.HandleFunc("GET /charts", s.handleCharts)
	mux.HandleFunc("GET /live", s.handleLive)
	mux.HandleFunc("POST /refresh", s.handleRefresh)
	mux.Handle("GET /static/", staticHandler())
	s.handler = s.withAuth(mux)
	return s, nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.handler.ServeHTTP(w, r)
}

// staticHandler serves the embedded static/ tree with a short cache window.
func staticHandler() http.Handler {
	files := http.FileServerFS(content)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "max-age=300")
		files.ServeHTTP(w, r)
	})
}

// render executes a page template into a buffer first, so a runtime template
// error yields a clean 500 instead of a half-written page.
func (s *Server) render(w http.ResponseWriter, page string, vm any) {
	var buf bytes.Buffer
	if err := s.pages[page].ExecuteTemplate(&buf, "base.html", vm); err != nil {
		http.Error(w, "template error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	buf.WriteTo(w)
}

// errorPage writes a minimal self-contained error page (used for 404s).
func errorPage(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(code)
	fmt.Fprintf(w, `<!DOCTYPE html>
<html lang="en"><head><meta charset="utf-8"><title>%d · fftui</title>
<link rel="stylesheet" href="/static/style.css"></head>
<body><main class="errorpage"><h1>%d</h1><p>%s</p>
<p><a href="/cycles">back to cycles</a></p></main></body></html>`,
		code, code, html.EscapeString(msg))
}

// bootstrap returns the memoised bootstrap band for snap, recomputing only
// when a new snapshot (new FetchedAt) is seen. Holding the lock across the
// compute means concurrent first requests wait rather than duplicating the
// 10k-resample work.
func (s *Server) bootstrap(snap *data.Snapshot) analytics.BootstrapResult {
	s.bootMu.Lock()
	defer s.bootMu.Unlock()
	if !snap.FetchedAt.IsZero() && snap.FetchedAt.Equal(s.bootKey) {
		return s.boot
	}
	b := analytics.Bootstrap(snap.Cycles, s.opts.Rates, bootstrapResamples)
	s.bootKey, s.boot = snap.FetchedAt, b
	return b
}
