package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// loadDotEnv reads KEY=VALUE lines from a .env file and sets any variable that
// isn't already present in the environment (a real env var always wins). A
// missing file is not an error. Values are taken literally — no variable
// expansion — so a password containing $ is safe. Supports an optional `export`
// prefix, # comments, and single- or double-quoted values.
//
// It looks in the working directory first, then next to the executable, and
// finally in the per-user config file, so the app finds config whether you
// `go run .` from the project, double-click the exe, or install via brew (whose
// exe lives in the Cellar and is wiped on upgrade). First-wins means the user
// config has the lowest precedence: flags > real env > ./.env > exe-dir .env >
// user config.
func loadDotEnv() {
	paths := []string{".env"}
	if exe, err := os.Executable(); err == nil {
		paths = append(paths, filepath.Join(filepath.Dir(exe), ".env"))
	}
	if p := userConfigPath(); p != "" {
		paths = append(paths, p)
	}
	for _, p := range paths {
		applyDotEnv(p)
	}
}

// userConfigPath returns the per-user config file location, or "" if it can't be
// determined. On Unix (Linux and macOS alike) this is $XDG_CONFIG_HOME/fftui/
// config.env, defaulting to ~/.config/fftui/config.env — deliberately the
// gh/lazygit convention rather than os.UserConfigDir(), which picks
// ~/Library/Application Support on macOS. On Windows it's
// %AppData%\fftui\config.env (os.UserConfigDir()).
func userConfigPath() string {
	if runtime.GOOS == "windows" {
		dir, err := os.UserConfigDir()
		if err != nil {
			return ""
		}
		return filepath.Join(dir, "fftui", "config.env")
	}
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "fftui", "config.env")
}

func applyDotEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = unquote(strings.TrimSpace(val))
		if key != "" {
			if _, set := os.LookupEnv(key); !set {
				os.Setenv(key, val)
			}
		}
	}
}

// unquote strips a single pair of matching surrounding quotes.
func unquote(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// configTemplate is a commented KEY=VALUE template of every supported key. It
// mirrors .env.example; keep the two in sync.
const configTemplate = `# fftui user config. KEY=VALUE, read literally (no $ expansion), quotes optional.
# Lowest precedence: flags > real env > ./.env > exe-dir .env > this file.

# FF_USERNAME=you@example.com
# FF_PASSWORD=your-password-here

# Instead of storing the password in plaintext, fetch it from a command. Runs
# only when FF_PASSWORD is unset; trailing whitespace is trimmed. Example uses
# the 1Password CLI:
# FF_PASSWORD_CMD=op read op://Personal/FutureForex/password

# Optional — auto-detected from your account after login. Set it only to skip
# that lookup, or if your account has more than one client.
# FF_CLIENT_ID=

# Optional — supply a token directly to skip login (expires ~hourly):
# FF_TOKEN=

# Idle-cash rate (% per year) credited to days the capital isn't in a trade.
# Track the reserve bank rate here. Default 6. (--idle-rate flag overrides.)
# FF_IDLE_RATE=6

# Marginal tax rate (%) on returns, for the effective (net take-home) figure.
# Default 41. (--tax-rate flag overrides.)
# FF_TAX_RATE=41

# Annual exchange-control allowances in rand; SDA+FIA form the planning pool
# shown on the analytics tab (both 0 hides it). Defaults R2m / R10m.
# (--sda-limit / --fia-limit flags override.)
# FF_SDA_LIMIT=2000000
# FF_FIA_LIMIT=10000000

# Per-cycle fee model for the fee-aware capital projections. Fixed is rand per
# cycle; variable is % of cycle capital; tiers are "capital:percent,..." where
# percent is FF's share of gross profit from that capital upward.
# (--fee-fixed / --fee-variable / --fee-tiers flags override.)
# FF_FEE_FIXED=530
# FF_FEE_VARIABLE=0.23
# FF_FEE_TIERS=100000:35,150000:33,200000:30,300000:28,400000:25

# Optional host overrides (data + auth API hosts) — usually unnecessary:
# FF_BASE_URL=
# FF_AUTH_URL=
`

// initConfig writes the commented config template to path, creating the parent
// directory (0700) and file (0600). It never overwrites: if path already exists
// it returns wrote=false with no error. The mode bits are advisory on Windows.
func initConfig(path string) (wrote bool, err error) {
	if _, err := os.Stat(path); err == nil {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return false, err
	}
	if err := os.WriteFile(path, []byte(configTemplate), 0o600); err != nil {
		return false, err
	}
	return true, nil
}

// resolvePasswordCmd sets FF_PASSWORD from FF_PASSWORD_CMD when the password is
// unset/empty and a command is configured. The command is a shell one-liner
// (e.g. `op read op://Personal/FutureForex/password`) run via `sh -c` on Unix
// or `cmd /C` on Windows; its stdout is used verbatim minus trailing whitespace.
// A no-op returns nil; a command failure returns an error including its stderr.
func resolvePasswordCmd() error {
	if os.Getenv("FF_PASSWORD") != "" {
		return nil
	}
	cmdStr := os.Getenv("FF_PASSWORD_CMD")
	if cmdStr == "" {
		return nil
	}
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/C", cmdStr)
	} else {
		cmd = exec.Command("sh", "-c", cmdStr)
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return fmt.Errorf("FF_PASSWORD_CMD %q failed: %w: %s", cmdStr, err, msg)
		}
		return fmt.Errorf("FF_PASSWORD_CMD %q failed: %w", cmdStr, err)
	}
	os.Setenv("FF_PASSWORD", strings.TrimRight(string(out), " \t\r\n"))
	return nil
}
