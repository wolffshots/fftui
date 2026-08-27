package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/wolffshots/fftui/internal/analytics"
	"github.com/wolffshots/fftui/internal/data"
	"github.com/wolffshots/fftui/internal/model"
	"github.com/wolffshots/fftui/internal/ui"
	"github.com/wolffshots/fftui/internal/webui"
)

// version is overridden at release build time via -ldflags "-X main.version=…".
var version = "dev"

// minRefreshInterval is the floor for --refresh-interval: a live refresh is
// several API round trips, so anything tighter just hammers the brokerage for
// data that changes on the order of days.
const minRefreshInterval = 30 * time.Second

func main() {
	// Load credentials/config from a .env file first (real env vars still win),
	// so FF_IDLE_RATE can seed the flag default.
	loadDotEnv()

	csvPath := flag.String("csv", "", "read cycles from a CSV file instead of the live API")
	idlePct := flag.Float64("idle-rate", envFloat("FF_IDLE_RATE", 6.0), "annual % earned on idle cash (non-trading days); tracks the reserve bank rate")
	taxPct := flag.Float64("tax-rate", envFloat("FF_TAX_RATE", 41.0), "marginal tax % applied to returns for the effective (net) figure")
	sdaLimit := flag.Float64("sda-limit", envFloat("FF_SDA_LIMIT", 2_000_000), "annual Single Discretionary Allowance in rand (R2m since 8 Apr 2026)")
	aitLimit := flag.Float64("ait-limit", envFloat("FF_AIT_LIMIT", envFloat("FF_FIA_LIMIT", 10_000_000)), "annual Approval for International Transfer allowance in rand (ex-FIA); SDA+AIT form the planning pool, both 0 hides it")
	defFees := analytics.DefaultFees()
	feeFixed := flag.Float64("fee-fixed", envFloat("FF_FEE_FIXED", defFees.Fixed), "fixed third-party fees per cycle in rand (bank admin + EFT)")
	feeVarPct := flag.Float64("fee-variable", envFloat("FF_FEE_VARIABLE", defFees.Variable*100), "variable third-party fees per cycle as % of capital (exchange + offshore fees)")
	feeTiers := flag.String("fee-tiers", envStr("FF_FEE_TIERS", ""), `FF success-fee tiers as "capital:percent,..." (e.g. "100000:35,200000:30,400000:25"); empty uses the built-in schedule`)
	fromStr := flag.String("from", envStr("FF_FROM", ""), "only show cycles active on or after this date (YYYY-MM-DD); empty = no lower bound")
	toStr := flag.String("to", envStr("FF_TO", ""), "only show cycles active on or before this date (YYYY-MM-DD); empty = no upper bound")
	refreshEvery := flag.Duration("refresh-interval", envDuration("FF_REFRESH_INTERVAL", 0), "re-fetch automatically at this interval (e.g. 30s, 5m); 0 disables it")
	web := flag.Bool("web", false, "serve the web UI on --addr alongside the TUI")
	headless := flag.Bool("headless", false, "with --web: serve the web UI only (no TUI); runs until SIGINT/SIGTERM")
	addr := flag.String("addr", envStr("FF_WEB_ADDR", "127.0.0.1:8442"), "listen address for the web UI (--web)")
	showVersion := flag.Bool("version", false, "print version and exit")
	logout := flag.Bool("logout", false, "clear the cached login token and exit")
	initConfigFlag := flag.Bool("init-config", false, "write a commented config template to the user config path and exit")
	flag.Parse()

	if *headless && !*web {
		fmt.Fprintln(os.Stderr, "--headless requires --web")
		os.Exit(1)
	}

	if *showVersion {
		fmt.Println("fftui", version)
		return
	}

	if *initConfigFlag {
		path := userConfigPath()
		if path == "" {
			fmt.Fprintln(os.Stderr, "could not determine user config path")
			os.Exit(1)
		}
		wrote, err := initConfig(path)
		if err != nil {
			fmt.Fprintln(os.Stderr, "init-config failed:", err)
			os.Exit(1)
		}
		if wrote {
			fmt.Println("wrote config template to", path)
		} else {
			fmt.Println(path, "already exists, not overwriting")
		}
		return
	}

	if *logout {
		model.NewLiveSource().Logout()
		fmt.Println("cached login token cleared")
		return
	}

	// After the exit-early modes so a config-seeded date window or interval
	// can't break --version/--init-config/--logout — including --init-config,
	// the command you reach for to repair a bad config — but still before any
	// login/OTP, so a typo fails without costing an OTP round trip.
	from, err := parseDate("--from", *fromStr)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	to, err := parseDate("--to", *toStr)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if !from.IsZero() && !to.IsZero() && to.Before(from) {
		fmt.Fprintf(os.Stderr, "--to %s is before --from %s\n", *toStr, *fromStr)
		os.Exit(1)
	}
	if *refreshEvery != 0 && *refreshEvery < minRefreshInterval {
		fmt.Fprintf(os.Stderr, "--refresh-interval %s is below the %s minimum\n", *refreshEvery, minRefreshInterval)
		os.Exit(1)
	}

	// Selection logic (§3): --csv → CSVSource, otherwise the live API source.
	var source model.CycleSource
	if *csvPath != "" {
		source = model.NewCSVSource(*csvPath)
	} else {
		// Resolve FF_PASSWORD from FF_PASSWORD_CMD if needed (e.g. a 1Password
		// `op read` one-liner) before anything tries to log in. Only the live
		// source needs credentials — CSV mode must run without them.
		if err := resolvePasswordCmd(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		live := model.NewLiveSource()
		// Authenticate up front so an OTP prompt (if the account requires one)
		// happens here on the terminal rather than inside the alt-screen UI.
		live.OTPFunc = promptOTP
		if err := live.EnsureToken(context.Background()); err != nil {
			fmt.Fprintln(os.Stderr, "login failed:", err)
			os.Exit(1)
		}
		live.OTPFunc = nil // never prompt from inside the running UI
		source = live
	}

	now := ui.Today()
	// Flags are percentages; analytics wants fractions.
	rates := analytics.Rates{Idle: *idlePct / 100, Tax: *taxPct / 100}

	allow := analytics.Allowances{SDALimit: *sdaLimit, AITLimit: *aitLimit}
	fees := analytics.Fees{Fixed: *feeFixed, Variable: *feeVarPct / 100, Tiers: defFees.Tiers}
	if *feeTiers != "" {
		tiers, err := parseFeeTiers(*feeTiers)
		if err != nil {
			fmt.Fprintln(os.Stderr, "bad --fee-tiers:", err)
			os.Exit(1)
		}
		fees.Tiers = tiers
	}
	svc := data.NewService(source)
	svc.SetDateRange(from, to)

	// Web front end: same service, same figures. Listen before the TUI takes
	// the terminal so a bad address (port in use) fails loudly up front.
	var websrv *http.Server
	var webErr chan error
	if *web {
		handler, err := webui.New(svc, webui.Options{
			Token:   os.Getenv("FF_WEB_TOKEN"),
			Version: version,
			CSVMode: *csvPath != "",
			Rates:   rates,
			Allow:   allow,
			Fees:    fees,
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, "web ui:", err)
			os.Exit(1)
		}
		warnIfExposed(*addr, os.Getenv("FF_WEB_TOKEN"))

		if *headless {
			// Best-effort initial fetch so the first page has data; a failure
			// is stored on the service and shown as the page's error banner.
			if _, err := svc.Refresh(context.Background()); err != nil {
				fmt.Fprintln(os.Stderr, "initial refresh failed (serving anyway):", err)
			}
		}

		ln, err := net.Listen("tcp", *addr)
		if err != nil {
			fmt.Fprintln(os.Stderr, "web listen:", err)
			os.Exit(1)
		}
		websrv = &http.Server{Handler: handler}
		webErr = make(chan error, 1)
		go func() { webErr <- websrv.Serve(ln) }()
		fmt.Printf("web ui on http://%s\n", ln.Addr())

		if *headless {
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			if *refreshEvery > 0 {
				// No TUI to drive the auto-refresh tick here, so re-fetch the
				// shared service directly. Best-effort like the initial fetch:
				// a failure keeps the last good snapshot and shows the page's
				// error banner. Ticking and fetching on ctx means a
				// SIGINT/SIGTERM ends the loop and cancels an in-flight fetch
				// rather than leaving both running through shutdown.
				go func() {
					tick := time.NewTicker(*refreshEvery)
					defer tick.Stop()
					for {
						select {
						case <-ctx.Done():
							return
						case <-tick.C:
							_, _ = svc.Refresh(ctx)
						}
					}
				}()
			}
			select {
			case <-ctx.Done():
			case err := <-webErr:
				fmt.Fprintln(os.Stderr, "web server error:", err)
				os.Exit(1)
			}
			shutdownWeb(websrv, webErr)
			return
		}
	}

	p := tea.NewProgram(ui.New(svc, now, rates, allow, fees, *refreshEvery), tea.WithAltScreen())
	_, runErr := p.Run()
	if websrv != nil {
		shutdownWeb(websrv, webErr)
	}
	if runErr != nil {
		fmt.Fprintln(os.Stderr, "error:", runErr)
		os.Exit(1)
	}
}

// shutdownWeb stops the web server with a short grace period and surfaces any
// serve error that happened while the other front end was running.
func shutdownWeb(srv *http.Server, errCh chan error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	srv.Shutdown(ctx)
	if err := <-errCh; err != nil && !errors.Is(err, http.ErrServerClosed) {
		fmt.Fprintln(os.Stderr, "web server error:", err)
	}
}

// warnIfExposed prints a loud warning when the web UI will listen beyond
// loopback with auth disabled — that serves the full trading history
// unauthenticated to whatever network the address is on.
func warnIfExposed(addr, token string) {
	if token != "" {
		return
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	if host == "localhost" {
		return
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return
	}
	fmt.Fprintf(os.Stderr, "WARNING: --addr %s is not loopback and FF_WEB_TOKEN is unset — the web UI will serve your full trading history UNAUTHENTICATED on the network\n", addr)
}

// promptOTP asks the user for the OTP the API just sent (via WhatsApp/SMS) and
// returns the entered code. Reads a line from stdin, so it must run before the
// alt-screen UI takes over the terminal.
func promptOTP(detail string, channels []string) (string, error) {
	if detail != "" {
		fmt.Println(detail)
	}
	if len(channels) > 0 {
		fmt.Printf("(sent via %s) ", strings.Join(channels, "/"))
	}
	fmt.Print("Enter OTP code: ")
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

// parseDate parses an optional YYYY-MM-DD flag value; empty means unset and
// returns the zero time (an open bound).
func parseDate(name, s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Time{}, fmt.Errorf("bad %s: %q is not a YYYY-MM-DD date", name, s)
	}
	return t, nil
}

// parseFeeTiers parses "capital:percent,..." (e.g. "100000:35,400000:25") into
// an ascending tier schedule. Percent is FF's share of gross profit.
func parseFeeTiers(s string) ([]analytics.FeeTier, error) {
	var tiers []analytics.FeeTier
	for _, part := range strings.Split(s, ",") {
		lohi := strings.SplitN(strings.TrimSpace(part), ":", 2)
		if len(lohi) != 2 {
			return nil, fmt.Errorf("%q is not capital:percent", part)
		}
		min, err1 := strconv.ParseFloat(strings.TrimSpace(lohi[0]), 64)
		pct, err2 := strconv.ParseFloat(strings.TrimSpace(lohi[1]), 64)
		if err1 != nil || err2 != nil {
			return nil, fmt.Errorf("%q is not capital:percent", part)
		}
		if len(tiers) > 0 && min <= tiers[len(tiers)-1].Min {
			return nil, fmt.Errorf("tiers must be in ascending capital order at %q", part)
		}
		tiers = append(tiers, analytics.FeeTier{Min: min, Rate: pct / 100})
	}
	return tiers, nil
}

// envStr reads a string env var, falling back to def when unset.
func envStr(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}

// envFloat reads a number from an env var (a percentage like "41.5" or a rand
// amount like "2000000"), falling back to def when unset or unparseable. Used
// as a flag default so env seeds it and an explicit flag still overrides.
func envFloat(name string, def float64) float64 {
	if v := os.Getenv(name); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return def
}

// envDuration reads a Go duration from an env var (e.g. "30s", "5m"), falling
// back to def when unset or unparseable. Unlike envFloat it warns on stderr
// before falling back: a wrong rate still shows a wrong number the user can
// notice, but a wrong interval leaves the feature it configures off with
// nothing on screen to say so. Used as a flag default so env seeds it and an
// explicit flag still overrides.
func envDuration(name string, def time.Duration) time.Duration {
	if v := os.Getenv(name); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
		fmt.Fprintf(os.Stderr, "bad %s: %q is not a Go duration (e.g. 30s, 5m); falling back to %s\n", name, v, def)
	}
	return def
}
