#!/usr/bin/env bash
#
# render-demo.sh — render demo.gif from demo.tape using VHS. Built for WSL/Ubuntu.
#
# Run from a WSL (Ubuntu/Debian) shell, from anywhere inside the repo:
#   bash scripts/render-demo.sh        # or ./scripts/render-demo.sh if executable
#
# Installs the tools VHS needs (ttyd, ffmpeg, and the headless-Chromium runtime
# libs) via apt when missing, installs vhs via `go install` if absent, builds
# fftui, then renders demo.gif. VHS downloads Chromium itself on first run.
set -euo pipefail

cd "$(git rev-parse --show-toplevel 2>/dev/null || dirname "$(dirname "$0")")"
echo "repo: $(pwd)"

have() { command -v "$1" >/dev/null 2>&1; }

if ! have go; then
  echo "error: Go is required — install from https://go.dev/dl/ or: sudo apt install golang-go" >&2
  exit 1
fi
export PATH="$(go env GOPATH)/bin:$PATH"

# ttyd + ffmpeg are VHS's documented dependencies.
need_apt=()
have ttyd   || need_apt+=(ttyd)
have ffmpeg || need_apt+=(ffmpeg)
if [ "${#need_apt[@]}" -gt 0 ]; then
  have apt-get || { echo "error: need ${need_apt[*]} but no apt-get here — install them manually." >&2; exit 1; }
  echo "installing: ${need_apt[*]}"
  sudo apt-get update -y
  sudo apt-get install -y "${need_apt[@]}"
fi

# Best-effort: runtime libs the headless Chromium needs. Package names vary by
# Ubuntu release (libasound2 vs libasound2t64), so don't abort on a mismatch.
if have apt-get; then
  sudo apt-get install -y libnss3 libgbm1 libasound2 >/dev/null 2>&1 \
    || sudo apt-get install -y libnss3 libgbm1 libasound2t64 >/dev/null 2>&1 \
    || echo "note: could not auto-install Chromium libs; if VHS can't launch Chromium see github.com/charmbracelet/vhs#dependencies"
fi

if ! have vhs; then
  echo "installing vhs (go install github.com/charmbracelet/vhs@latest)..."
  go install github.com/charmbracelet/vhs@latest
fi
have vhs || { echo "error: vhs not on PATH — add \"$(go env GOPATH)/bin\" to your PATH." >&2; exit 1; }

echo "building fftui..."
go build -o fftui .
chmod +x fftui

echo "rendering demo.gif (drives the TUI ~30s; first run downloads Chromium)..."
vhs demo.tape

echo
echo "done → $(pwd)/demo.gif"
echo "commit it: git add demo.gif && git commit -m 'Add demo.gif' && git push"
