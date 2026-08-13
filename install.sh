#!/usr/bin/env bash
# Provider Hub installer for Linux / macOS.
# Builds the ph binary and installs it, then writes the harness wrappers.
#
# Usage:
#   ./install.sh                 # install to ~/.local/bin (user) or /usr/local/bin (root)
#   PREFIX=$HOME/tools ./install.sh   # custom install dir
#
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# --- 1. Detect OS / arch -----------------------------------------------------
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"
case "$ARCH" in
  x86_64|amd64) ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  *) echo "Unsupported arch: $ARCH" >&2; exit 1 ;;
esac
BIN="ph"
if [ "$OS" = "windows" ]; then BIN="ph.exe"; fi

# --- 2. Pick install dir -----------------------------------------------------
PREFIX="${PREFIX:-}"
if [ -z "$PREFIX" ]; then
  if [ "$(id -u)" -eq 0 ]; then
    PREFIX="/usr/local"
  else
    PREFIX="$HOME/.local"
  fi
fi
DEST="$PREFIX/bin"
mkdir -p "$DEST"

echo "==> Building $BIN for $OS/$ARCH"
( cd "$ROOT" && go build -o "$BIN" ./cmd/ph )

echo "==> Installing to $DEST"
install -m 755 "$ROOT/$BIN" "$DEST/$BIN"
rm -f "$ROOT/$BIN"

# --- 3. Ensure on PATH -------------------------------------------------------
if ! command -v ph >/dev/null 2>&1; then
  case ":$PATH:" in
    *":$DEST:"*) : ;;
    *)
      SHELLRC=""
      case "${SHELL:-}" in
        *zsh) SHELLRC="$HOME/.zshrc" ;;
        *bash) SHELLRC="$HOME/.bashrc" ;;
      esac
      if [ -n "$SHELLRC" ]; then
        echo "==> Adding $DEST to PATH in $SHELLRC"
        printf '\n# Provider Hub\n[ -d "%s" ] && export PATH="%s:$PATH"\n' "$DEST" "$DEST" >> "$SHELLRC"
      else
        echo "==> Add $DEST to your PATH (not detected as bash/zsh)"
      fi
      ;;
  esac
fi

# --- 4. Set up harness wrappers ----------------------------------------------
if command -v ph >/dev/null 2>&1 || [ -x "$DEST/ph" ]; then
  echo "==> Writing harness wrappers (ph-claude, ph-codex, ph-pi, ph-opencode)"
  "$DEST/ph" hsi setup
fi

echo
echo "Installed provider-hub to $DEST/ph"
echo "Run 'ph hsi setup' again later if you remove the wrappers, and 'ph hsi set <tool> --provider <id>' to change defaults."
echo "Next: ph add   -> add your first provider"