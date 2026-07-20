# fftui

A terminal UI (Go + Bubble Tea) for browsing forex arbitrage **cycles** and
comparing their annualised returns against a savings account.

![fftui walking through the bundled demo dataset](demo.gif)

## Install

Download the binary for your platform from the [latest release](https://github.com/wolffshots/fftui/releases/latest):

| Platform | Asset |
|---|---|
| Linux x86-64 | `fftui_<version>_linux_amd64` |
| Windows x86-64 | `fftui_<version>_windows_amd64.exe` |
| macOS (Apple silicon) | `fftui_<version>_darwin_arm64` |

On Linux/macOS, make it executable and check it runs:

```sh
chmod +x fftui_*            # the file you downloaded
./fftui_* --version
```

macOS binaries are unsigned, so the first launch is blocked by Gatekeeper —
right-click → **Open**, or clear the quarantine flag with
`xattr -d com.apple.quarantine fftui_*_darwin_arm64`. To verify a download, run
`sha256sum -c checksums.txt` (Linux) or `shasum -a 256 -c checksums.txt` (macOS).

Prefer to build it yourself? `go install github.com/wolffshots/fftui@latest`
puts the binary in `$(go env GOPATH)/bin`.

## Run

The examples below run from a source checkout (`go run .`); with an installed
binary, use your `fftui` executable in its place.

```sh
# Offline / dev — read from the CSV export:
go run . --csv testdata/cycles.csv

# Live — pull from the brokerage API (default when --csv is omitted).
# Easiest: put credentials in a .env file (gitignored), then just run:
cp .env.example .env       # then edit .env with your email + password
go run .
```

Build a binary with `go build -o fftui .`.

### Credentials

The app reads a **`.env`** file (in the working directory or next to the binary)
on startup; it's gitignored. Real environment variables override it. Values are
literal — no `$` escaping needed.

```
FF_USERNAME=you@example.com
FF_PASSWORD=your-password
```

The app logs in with your email + password (CSRF flow), mints a token, and loads
your cycles. Alternatively set `FF_TOKEN` to a still-valid token to skip login
(they expire ~hourly). `r` in the app re-mints and refreshes. Your client id is
auto-detected from the account after login; set `FF_CLIENT_ID` only to override
it (or if the account has more than one client).

If the account has OTP enabled, fftui prompts for the code (sent via WhatsApp/SMS)
on the terminal before the UI opens.

### Live-source environment variables

| Var | Default | Purpose |
|---|---|---|
| `FF_TOKEN` | — | token (bare or `Token <token>`); skips login if set |
| `FF_USERNAME` / `FF_PASSWORD` | — | mint a token via CSRF login when no token is set |
| `FF_CLIENT_ID` | auto | client id; auto-detected from the account after login — set to override |
| `FF_BASE_URL` | — | override the data API host |
| `FF_AUTH_URL` | — | override the login host (CSRF + login) |
| `FF_IDLE_RATE` | `6` | idle-cash rate (% p.a.); also `--idle-rate` |
| `FF_TAX_RATE` | `41` | marginal tax rate (%) on returns; also `--tax-rate` |

> Login is a two-step CSRF flow on a separate host that returns an
> `arb_auth_token` (valid ~1 hour); `r` in the app re-mints and refreshes.

## Views

`1` Cycles table · `2` Analytics · `3` Detail · `4` Charts. `?` toggles full
help; `r` refreshes; `q` quits.

- **Table** — all cycles; `s`/`S` sort, `/` filter, `enter` opens detail.
- **Analytics** — Year/Quarter/Month (`tab`) buckets with compound + annualised
  columns, a variance strip (`a` toggles active-only vs incl-dead buckets), and
  a ⚠ flag on partial periods whose annualised figure is unreliable.
- **Detail** — one cycle, including its hold-days ("best-case, no-idle")
  annualisation, explicitly separated from the savings-comparable headline rate.
- **Charts** — braille/block sparklines for per-cycle return, monthly annualised
  rate, and cumulative profit.

## Methodology

**Reporting convention:** every rate is a **nominal annual rate compounded
monthly** (as a bank quotes "6% p.a. compounded monthly"), so the idle account
reads back as exactly its input rate and the figures are directly comparable to
it. Internally the maths uses effective growth factors and converts to the
nominal-monthly quote at the end.

Headline annualised returns compound each cycle (`G = Π(1+rᵢ) − 1`) and annualise
over the bucket's **elapsed calendar days** (not holding days), so idle time
honestly lowers the rate:

```
annualFactor = (1 + G) ^ (365 / calendarDays)
annualised   = 12 × ( annualFactor ^ (1/12) − 1 )      # nominal p.a., monthly
```

On the bundled demo dataset this is **9.78%** (the effective-annual equivalent is
10.23%; the underlying compound growth is 19.42%). The regression tests in
`internal/analytics/analytics_test.go` pin the full set (quarterly variance
8.47% / 9.50% / 2.43pts, etc.).

Three fairness details: a cycle spanning a bucket boundary is **prorated**
across the buckets by days spent in each (growth geometrically, profit
linearly); **trading days are distinct calendar days** with a cycle open, so a
same-day rollover counts once; and ⚠ **partial buckets are excluded from the
variance stats** — their exploding `^(365/days)` figure is flagged unreliable
in the table, so it doesn't feed the mean/median/std either.

### With-idle annualised

Each bucket and the lifetime footer also show an **annualised rate that credits
idle cash** — on every calendar day the capital is *not* in a trade it earns the
configurable idle rate (`--idle-rate`, default 6% p.a. compounded monthly, set to
your reserve-bank rate). So the return is the arb spread while trading and the
idle rate otherwise:

```
idleDaily    = (1 + idle/12) ^ (12/365)                    # monthly-compounded, per day
periodFactor = Π(1+rᵢ) × idleDaily ^ (calendarDays − tradingDays)
annualised   = 12 × ( (periodFactor ^ (365/calendarDays)) ^ (1/12) − 1 )
```

On the demo dataset this lifts 9.78% → **15.13%** at 6%. An empty (no-trade) period
collapses to exactly the idle rate (6.00%). The arb-only columns are unchanged next
to it.

### After-tax effective return

A final **`Net@41%`** column (and footer figure) applies a configurable marginal
tax rate (`--tax-rate`, default 41%) to all returns — both arb profit and idle
interest are taxable, so each is scaled by `(1 − tax)` before annualising. This is
the true take-home. On the demo dataset: 15.13% (with idle) → **8.91%** after tax;
the arb-only after-tax figure is 5.77%. So the progression shown is:

```
annualised 9.78%  →  +idle@6% 15.13%  →  net@41% 8.91%
```

All three coexist; nothing is replaced. Zero rates collapse every overlay back to
the arb-only number.

### Full-period floor (pessimistic bound)

For the **in-progress** period, the `Annualised`/`+Idle`/`Net` columns *extrapolate*
the pace so far to a full year. Below the table, a floor callout instead shows the
**guaranteed-minimum** outcome: the actual trades so far plus idle for the entire
remainder of the period, computed over the full calendar span with **no
extrapolation**:

```
floor = withIdle(G, tradingDays, fullPeriodDays, idle)     # remainder = idle days
```

Because the real remainder will contain more (profitable) trades that replace idle
days, the actual period should meet or beat this. Example (2026, as of 2026-07-10):
floor **11.47%** +idle / **6.76%** net, versus the extrapolated 16.48% / 9.71%. For
a completed period there is no remainder, so its floor equals its realised
with-idle / net figures. The callout follows the active sub-tab (current
year/quarter/month) and is especially useful for short partial buckets where the
extrapolated figure is flagged ⚠ unreliable.

## Test

```sh
go test ./...
```

## Layout

```
main.go                      flag parsing, source selection, program start
internal/model/              Cycle, CSVSource, live API source (+ auth)
internal/analytics/          bucketing, annualisation, variance (+ regression tests)
internal/ui/                 root model, table/analytics/detail/charts views
testdata/cycles.csv          reference export used by tests
```
