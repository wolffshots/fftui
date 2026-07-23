# fftui

A terminal UI (Go + Bubble Tea) for browsing forex arbitrage **cycles** and
comparing their annualised returns against a savings account.

![fftui walking through the bundled demo dataset](demo.gif)

## Install

### Homebrew (macOS / Linux)

```sh
brew install wolffshots/tap/fftui
```

Builds from source (Go is installed as a build-only dependency) via
[wolffshots/homebrew-tap](https://github.com/wolffshots/homebrew-tap), so it
works on Intel and Apple silicon Macs and on Linux, with no Gatekeeper
quarantine step.

### Prebuilt binaries

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
on the terminal before the UI opens. The minted token is then cached (in your OS
cache dir, ~1h TTL) so later runs skip login — and the OTP — until it expires;
`fftui --logout` clears it.

#### User config file

The same keys can live in a per-user config file, which fftui reads at the
**lowest** precedence (flags > real env > `./.env` > exe-dir `.env` > user config).
This is the convenient home for an installed binary — e.g. a Homebrew install
whose executable sits in the Cellar and is wiped on each upgrade. Locations:

- **Linux and macOS:** `$XDG_CONFIG_HOME/fftui/config.env`, defaulting to
  `~/.config/fftui/config.env` when `XDG_CONFIG_HOME` is unset (the gh/lazygit
  convention — used on macOS too, not `~/Library/Application Support`).
- **Windows:** `%AppData%\fftui\config.env`.

Run `fftui --init-config` to write a commented template of every supported key to
that path (parent dir `0700`, file `0600`) and print the location. It never
overwrites an existing file.

#### Fetching the password from a command

Rather than store `FF_PASSWORD` in plaintext, set `FF_PASSWORD_CMD` to a shell
one-liner that prints the password on stdout; fftui runs it (only when
`FF_PASSWORD` is unset) and uses the output, trailing whitespace trimmed. Handy
with a secret manager such as the 1Password CLI:

```
FF_PASSWORD_CMD=op read op://Personal/FutureForex/password
```

### Live-source environment variables

| Var | Default | Purpose |
|---|---|---|
| `FF_TOKEN` | — | token (bare or `Token <token>`); skips login if set |
| `FF_USERNAME` / `FF_PASSWORD` | — | mint a token via CSRF login when no token is set |
| `FF_PASSWORD_CMD` | — | shell one-liner that prints the password; used only when `FF_PASSWORD` is unset (e.g. `op read op://Personal/FutureForex/password`) |
| `FF_CLIENT_ID` | auto | client id; auto-detected from the account after login — set to override |
| `FF_BASE_URL` | — | override the data API host |
| `FF_AUTH_URL` | — | override the login host (CSRF + login) |
| `FF_IDLE_RATE` | `6` | idle-cash rate (% p.a.); also `--idle-rate` |
| `FF_TAX_RATE` | `41` | marginal tax rate (%) on returns; also `--tax-rate` |
| `FF_SDA_LIMIT` | `2000000` | annual Single Discretionary Allowance in rand; also `--sda-limit` |
| `FF_FIA_LIMIT` | `10000000` | annual Foreign Investment Allowance in rand; also `--fia-limit`. SDA+FIA form the planning pool; both `0` hides it |
| `FF_FEE_FIXED` | `530` | fixed third-party fees per cycle in rand; also `--fee-fixed` |
| `FF_FEE_VARIABLE` | `0.23` | variable third-party fees as % of cycle capital; also `--fee-variable` |
| `FF_FEE_TIERS` | built-in | FF success-fee tiers as `capital:percent,...`; also `--fee-tiers` |

> Login is a two-step CSRF flow on a separate host that returns an
> `arb_auth_token` (valid ~1 hour); `r` in the app re-mints and refreshes.

## Web UI

The same views are available in a browser: `--web` serves them alongside the
TUI (the URL is printed before the TUI starts), and `--web --headless` serves
them without the TUI — for leaving fftui running on a home server.

```sh
# Alongside the TUI:
go run . --web

# Headless, reachable from other machines (set a token first — see below):
go run . --web --headless --addr 0.0.0.0:8442
```

Every page works without JavaScript — sort, filter, granularity and the
dead-bucket toggle all live in query parameters, so views are bookmarkable.
The refresh button re-pulls from the source (shared with the TUI's `r`).

| Flag / var | Default | Purpose |
|---|---|---|
| `--web` | off | serve the web UI on `--addr` alongside the TUI |
| `--headless` | off | with `--web`: no TUI; runs until SIGINT/SIGTERM |
| `--addr` / `FF_WEB_ADDR` | `127.0.0.1:8442` | listen address |
| `FF_WEB_TOKEN` | — | access token; auth is disabled when unset |

### Access from a phone

Set `FF_WEB_TOKEN` (any long random string) and bind beyond loopback
(`--addr 0.0.0.0:8442`). Then open
`http://<your-machine>:8442/cycles?token=<token>` once on the phone — the
token is exchanged for an `HttpOnly` cookie and stripped from the URL, so it
doesn't linger in the address bar or history. Scripts can send
`Authorization: Bearer <token>` instead. Without a token on a non-loopback
address, fftui prints a loud warning and serves your trading history
unauthenticated — only do that on a network you trust.

### Headless home-server recipe

```sh
FF_WEB_TOKEN=$(openssl rand -hex 24) ./fftui --web --headless --addr 0.0.0.0:8442
```

Login (and any OTP prompt) still happens on the terminal at startup, so if
your account uses OTP, run fftui once interactively first to seed the cached
login token, then start the headless server (or set `FF_TOKEN`). An initial
refresh is attempted at startup; if it fails, the server starts anyway and
every page offers a retry.

## Views

`1` Cycles table · `2` Analytics · `3` Detail · `4` Charts. `?` toggles full
help; `r` refreshes; `q` quits.

- **Table** — all cycles; `s`/`S` sort, `/` filter, `enter` opens detail.
- **Analytics** — Year/Quarter/Month/Tax-year (`tab`) buckets with compound +
  annualised columns, a variance strip (`a` toggles active-only vs incl-dead
  buckets), a lifetime money-weighted (IRR) line, a bootstrap 90% band on the
  headline rates, a planning strip (tax-year profit + estimated tax, allowance
  runway, capital sweet spot, fee ladder), a trend strip (return decay test,
  90-day rate/cadence comparison, live market-spread drift), and a ⚠ flag on
  partial periods whose annualised figure is unreliable.
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

### Bootstrap band (how solid is the headline rate?)

Under the IRR line, a **bootstrap 90% band** resamples the per-cycle returns
with replacement (10,000 draws, fixed RNG seed so the figures don't jitter
between refreshes) while holding the observed timeline — calendar span,
trading days, cycle count — fixed, then reports the 5th–95th percentile of the
recomputed lifetime arb-only and net (take-home) rates. Read it as "given this
many cycles of this variability, the headline rate could plausibly have landed
anywhere in this band". Cadence variability is deliberately not resampled, and
the band needs at least 8 cycles to show.

### Trend strip (is the arb decaying?)

Below the planning strip, three decay signals over the trailing year:

- **return trend** — an OLS slope on per-cycle returns over time, quoted in
  percentage points per 90 days, with a t-test: `noise (not significant)`
  until |t| ≥ 2, then `significant decay`/`significant improvement`.
- **90d vs prior 90d** — arb-only annualised and cycle count for the trailing
  90 days against the 90 days before, catching cadence slowdowns the slope
  can't see.
- **market spread** (live only) — the 30-day average arbitrage spread vs the
  365-day average from the market-conditions feed: whether the raw opportunity
  itself is thinning, independent of your execution.

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

### Planning strip (tax year, SDA runway, capital sweet spot)

Below the variance stats the analytics view shows three planning figures:

- **Tax year** — the `Tax year` granularity buckets on the South African tax
  year (1 March – end February, labelled by the year it ends: TY2027 = 1 Mar
  2026 – 28 Feb 2027). The planning strip separately shows taxable profit for
  the *current* tax year using **realisation accounting** — a cycle's whole
  profit lands in the tax year its end date falls in, which is what a
  provisional return needs (the rate buckets instead prorate boundary-spanning
  cycles, so the two can differ slightly) — times `--tax-rate` for the
  estimated tax owed.
- **Allowance runway** — every cycle sends its `ZAR in` offshore afresh, so
  each consumes that much annual exchange-control allowance again. Future
  Forex trades against the **combined pool** — the clearance-free SDA
  (`--sda-limit`, default R2m — doubled from R1m in the 2026 Budget, effective
  8 April 2026 per SARB Exchange Control Circular 6-2026) plus the FIA
  (`--fia-limit`, default R10m, on the AIT clearances FF files for you) —
  R12m/year combined. With the live source, usage comes from the API's actual
  `SDA available` / `FIA available` balances (which also see in-flight cycles
  and non-arb transfers) and the strip shows the split; in `--csv` mode it is
  inferred by summing the year's cycle `ZAR in`s. Either way the strip shows
  usage, remaining, and the projected exhaustion date at the year-to-date pace.
- **Capital sweet spot** — `(sda+fia) / cycles-per-year` (trailing 365 days):
  the per-cycle capital above which the combined allowance runs out before the
  year does. Below it, an extra rand compounds through every cycle (≈ avg
  return × cycles/yr per year, gross); above it, extra capital only ever earns
  the idle rate, because the allowance — not capital — is the binding
  constraint on annual profit (`max gross ≈ avg return × (sda+fia)`).

### Fee model (fee ladder and capital projections)

The capital figures in the planning strip are **fee-aware**. Each cycle's fee
waterfall (verified to the cent against real cycle statements) is:

```
gross earnings  = capital × market spread        # from live trade rates
− third-party   = fixed rand (bank admin R500 + instant EFT R30 = R530)
                + variable × capital (bank exchange + offshore fees ≈ 0.23%)
= gross profit
− FF success fee = gross profit × tier(capital)  # 35% ≥R100k · 33% ≥R150k ·
                                                 # 30% ≥R200k · 28% ≥R300k · 25% ≥R400k
= net profit
```

Because the fixed fees dilute and the FF tier falls as cycle capital grows,
the net return per cycle **improves with size**. The planning strip backs the
average market spread out of the trailing cycles through this waterfall, then
projects net returns at other capital sizes: the `fee ladder` line shows your
current tier vs the top tier and the modelled net-return-per-cycle at each,
and the `+R100k` figure prices extra capital including tier crossings (above
the sweet spot only the fee-position improvement remains, since deployment is
allowance-capped). All parameters are configurable (`--fee-fixed`,
`--fee-variable`, `--fee-tiers "100000:35,...,400000:25"`) because FF can
revise the schedule; only the 35% and 30% tiers are corroborated by statements
on file, the rest follow FF's published table.

### Money-weighted (IRR)

The analytics view also shows a lifetime **money-weighted** rate (XIRR). The
headline figures above are *time-weighted* — every cycle counts equally
regardless of size — while the IRR weights each stretch by the capital actually
in it, so it tracks what your specific rands earned across deposits and
withdrawals. External flows are inferred from the cycle series: each payout is
assumed to sit at 0% until redeployed, and any gap between a cycle's `ZAR in`
and the cash on hand is a deposit (or withdrawal). With clean full reinvestment
(each `ZAR in` = previous `ZAR out`, as in the demo dataset) the intermediate
flows vanish and the IRR equals the arb-only annualised (9.78%); the two
diverge only when capital jumps between cycles. Quoted in the same
nominal-monthly convention, arb-only (no idle credit).

## Test

```sh
go test ./...
```

## Layout

```
main.go                      flag parsing, source selection, program start
internal/model/              Cycle, CSVSource, live API source (+ auth)
internal/analytics/          bucketing, annualisation, variance (+ regression tests)
internal/data/               Service: fetch/cache seam shared by both front ends
internal/format/             pure text formatters (money/percent/sparklines)
internal/ui/                 root model, table/analytics/detail/charts views
internal/webui/              the browser front end (--web): handlers + templates
testdata/cycles.csv          reference export used by tests
```
