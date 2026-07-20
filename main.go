package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/wolffshots/fftui/internal/analytics"
	"github.com/wolffshots/fftui/internal/model"
	"github.com/wolffshots/fftui/internal/ui"
)

// version is overridden at release build time via -ldflags "-X main.version=…".
var version = "dev"

func main() {
	// Load credentials/config from a .env file first (real env vars still win),
	// so FF_IDLE_RATE can seed the flag default.
	loadDotEnv()

	csvPath := flag.String("csv", "", "read cycles from a CSV file instead of the live API")
	idlePct := flag.Float64("idle-rate", envPct("FF_IDLE_RATE", 6.0), "annual % earned on idle cash (non-trading days); tracks the reserve bank rate")
	taxPct := flag.Float64("tax-rate", envPct("FF_TAX_RATE", 41.0), "marginal tax % applied to returns for the effective (net) figure")
	showVersion := flag.Bool("version", false, "print version and exit")
	logout := flag.Bool("logout", false, "clear the cached login token and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println("fftui", version)
		return
	}

	if *logout {
		model.NewLiveSource().Logout()
		fmt.Println("cached login token cleared")
		return
	}

	// Selection logic (§3): --csv → CSVSource, otherwise the live API source.
	var source model.CycleSource
	if *csvPath != "" {
		source = model.NewCSVSource(*csvPath)
	} else {
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

	p := tea.NewProgram(ui.New(source, now, rates), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
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

// envPct reads a percentage from an env var (e.g. "6" or "41.5"), falling back
// to def when unset or unparseable. Used as a flag default so env seeds it and
// an explicit flag still overrides.
func envPct(name string, def float64) float64 {
	if v := os.Getenv(name); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return def
}
