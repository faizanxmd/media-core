#!/bin/sh
# Media Core: the one-line install.
#
#   curl -fsSL https://github.com/faizanxmd/media-core/releases/latest/download/install.sh | sh
#
# Downloads the engine for this computer from GitHub Releases, checks it against the release's SHA256SUMS,
# puts it in ~/.local/bin and runs `media-core-engine install`, which sets up the service and prints how to
# finish in a browser. It never uses sudo: anything that needs it (installing ffmpeg and aria2, keeping the
# server running after you log out) is printed for you to run.
#
# Settings (environment variables):
#   MEDIA_CORE_VERSION=v1.2.3   a specific release (default: the latest)
#   MEDIA_CORE_LIBRARY=/path    the library folder, without asking
#   MEDIA_CORE_YES=1            don't ask anything (for scripts and AI agents)
#   MEDIA_CORE_REPO=owner/name  install from a fork
set -eu

REPO="${MEDIA_CORE_REPO:-faizanxmd/media-core}"
VERSION="${MEDIA_CORE_VERSION:-latest}"
BIN_DIR="$HOME/.local/bin"

say() { printf '%s\n' "$*"; }
die() { printf 'media-core install: %s\n' "$*" >&2; exit 1; }

case "$(uname -s)" in
  Linux) os=linux ;;
  Darwin) os=darwin ;;
  *) die "this computer runs $(uname -s); Media Core runs on Linux and macOS" ;;
esac
case "$(uname -m)" in
  x86_64 | amd64) arch=amd64 ;;
  aarch64 | arm64) arch=arm64 ;;
  *) die "no build for $(uname -m) yet; build from source: see docs/development.md" ;;
esac

if command -v curl >/dev/null 2>&1; then
  fetch() { curl -fsSL --retry 3 -o "$2" "$1"; }
elif command -v wget >/dev/null 2>&1; then
  fetch() { wget -q -O "$2" "$1"; }
else
  die "needs curl or wget"
fi
if command -v sha256sum >/dev/null 2>&1; then
  sha() { sha256sum "$1" | cut -d' ' -f1; }
elif command -v shasum >/dev/null 2>&1; then
  sha() { shasum -a 256 "$1" | cut -d' ' -f1; }
else
  die "needs sha256sum or shasum to check the download"
fi

if [ "$VERSION" = latest ]; then
  base="https://github.com/$REPO/releases/latest/download"
else
  base="https://github.com/$REPO/releases/download/$VERSION"
fi
file="media-core-engine-$os-$arch"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT INT TERM
say "Downloading $file ($VERSION)…"
fetch "$base/$file" "$tmp/$file" || die "couldn't download $base/$file"
fetch "$base/SHA256SUMS" "$tmp/SHA256SUMS" || die "couldn't download SHA256SUMS"
want=$(awk -v f="$file" '$2 == f || $2 == "*"f {print $1}' "$tmp/SHA256SUMS")
[ -n "$want" ] || die "SHA256SUMS has no line for $file"
[ "$(sha "$tmp/$file")" = "$want" ] || die "the download doesn't match SHA256SUMS; nothing was installed"

mkdir -p "$BIN_DIR"
chmod 755 "$tmp/$file"
mv "$tmp/$file" "$BIN_DIR/media-core-engine"
say "Installed $BIN_DIR/media-core-engine ($("$BIN_DIR/media-core-engine" version))"

case ":$PATH:" in
  *":$BIN_DIR:"*) ;;
  *) say "Note: $BIN_DIR isn't on your PATH. Add it, or run $BIN_DIR/media-core-engine." ;;
esac

set -- install
[ -n "${MEDIA_CORE_LIBRARY:-}" ] && set -- "$@" --library "$MEDIA_CORE_LIBRARY"
[ "${MEDIA_CORE_YES:-}" = 1 ] && set -- "$@" --yes
# reading answers from the terminal even when this script arrives through a pipe
if [ "${MEDIA_CORE_YES:-}" != 1 ] && [ -r /dev/tty ] && [ ! -t 0 ]; then
  exec "$BIN_DIR/media-core-engine" "$@" </dev/tty
fi
exec "$BIN_DIR/media-core-engine" "$@"
