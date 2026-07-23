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
	idlePct := flag.Float64("idle-rate", envFloat("FF_IDLE_RATE", 6.0), "annual % earned on idle cash (non-trading days); tracks the reserve bank rate")
	taxPct := flag.Float64("tax-rate", envFloat("FF_TAX_RATE", 41.0), "marginal tax % applied to returns for the effective (net) figure")
	sdaLimit := flag.Float64("sda-limit", envFloat("FF_SDA_LIMIT", 2_000_000), "annual Single Discretionary Allowance in rand (R2m since 8 Apr 2026)")
	fiaLimit := flag.Float64("fia-limit", envFloat("FF_FIA_LIMIT", 10_000_000), "annual Foreign Investment Allowance in rand (AIT-cleared); SDA+FIA form the planning pool, both 0 hides it")
	defFees := analytics.DefaultFees()
	feeFixed := flag.Float64("fee-fixed", envFloat("FF_FEE_FIXED", defFees.Fixed), "fixed third-party fees per cycle in rand (bank admin + EFT)")
	feeVarPct := flag.Float64("fee-variable", envFloat("FF_FEE_VARIABLE", defFees.Variable*100), "variable third-party fees per cycle as % of capital (exchange + offshore fees)")
	feeTiers := flag.String("fee-tiers", envStr("FF_FEE_TIERS", ""), `FF success-fee tiers as "capital:percent,..." (e.g. "100000:35,200000:30,400000:25"); empty uses the built-in schedule`)
	showVersion := flag.Bool("version", false, "print version and exit")
	logout := flag.Bool("logout", false, "clear the cached login token and exit")
	initConfigFlag := flag.Bool("init-config", false, "write a commented config template to the user config path and exit")
	flag.Parse()

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

	// Resolve FF_PASSWORD from FF_PASSWORD_CMD if needed (e.g. a 1Password
	// `op read` one-liner) before anything tries to log in.
	if err := resolvePasswordCmd(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
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

	allow := analytics.Allowances{SDALimit: *sdaLimit, FIALimit: *fiaLimit}
	fees := analytics.Fees{Fixed: *feeFixed, Variable: *feeVarPct / 100, Tiers: defFees.Tiers}
	if *feeTiers != "" {
		tiers, err := parseFeeTiers(*feeTiers)
		if err != nil {
			fmt.Fprintln(os.Stderr, "bad --fee-tiers:", err)
			os.Exit(1)
		}
		fees.Tiers = tiers
	}
	p := tea.NewProgram(ui.New(source, now, rates, allow, fees), tea.WithAltScreen())
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
