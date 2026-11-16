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
#   MEDIA_CORE_VERSION=v1.2.3       a specific release (default: the latest)
#   MEDIA_CORE_LIBRARY=/path        the library folder, made if it isn't there
#   MEDIA_CORE_NAME=Living room     the server's name, as the apps show it
#   MEDIA_CORE_TMDB_KEY=...         a TMDB API key, for posters and details
#   MEDIA_CORE_YES=1                ask nothing (for scripts and AI agents)
#   MEDIA_CORE_NO_SERVICE=1         don't set up the systemd service; run `media-core-engine serve` yourself
#   MEDIA_CORE_NO_APPS=1            don't fetch the TV and phone apps
#   MEDIA_CORE_INSTALL_DIR=/path    where the binary goes (default ~/.local/bin)
#   MEDIA_CORE_REPO=owner/name      install from a fork
#
# Exit codes: 0 done, 1 failed, 2 wrong settings, 3 busy, 4 partly done, 5 not supported here.
set -eu

main() {
  REPO="${MEDIA_CORE_REPO:-faizanxmd/media-core}"
  BIN_DIR="${MEDIA_CORE_INSTALL_DIR:-$HOME/.local/bin}"
  VERSION="${MEDIA_CORE_VERSION:-latest}"
  case "$VERSION" in latest | v*) ;; *) VERSION="v$VERSION" ;; esac

  [ "$(id -u)" != 0 ] || die 5 "don't run this as root: Media Core runs as your own user. Run it again as that user, without sudo."

  case "$(uname -s)" in
    Linux) os=linux ;;
    Darwin) os=darwin ;;
    *) die 5 "this computer runs $(uname -s); Media Core runs on Linux (and by hand on macOS)" ;;
  esac
  case "$(uname -m)" in
    x86_64 | amd64) arch=amd64 ;;
    aarch64 | arm64) arch=arm64 ;;
    *) die 5 "no build for $(uname -m) yet; build from source: see docs/development.md" ;;
  esac

  if command -v curl >/dev/null 2>&1; then
    fetch() { curl -fsSL --proto '=https' --tlsv1.2 --retry 3 -o "$2" "$1"; }
    [ -z "${MEDIA_CORE_BASE_URL:-}" ] || fetch() { curl -fsSL --retry 3 -o "$2" "$1"; } # tests, mirrors
  elif command -v wget >/dev/null 2>&1; then
    fetch() { wget -q --https-only -O "$2" "$1"; }
    [ -z "${MEDIA_CORE_BASE_URL:-}" ] || fetch() { wget -q -O "$2" "$1"; }
  else
    die 1 "needs curl or wget"
  fi
  if command -v sha256sum >/dev/null 2>&1; then
    sha() { sha256sum "$1" | cut -d' ' -f1; }
  elif command -v shasum >/dev/null 2>&1; then
    sha() { shasum -a 256 "$1" | cut -d' ' -f1; }
  else
    die 1 "needs sha256sum or shasum to check the download"
  fi

  # where the files are: GitHub, or a copy laid out as <base>/latest (the tag) and <base>/download/<tag>/<file>
  if [ -n "${MEDIA_CORE_BASE_URL:-}" ]; then
    base="${MEDIA_CORE_BASE_URL%/}"
    if [ "$VERSION" = latest ]; then
      tagfile=$(mktemp)
      fetch "$base/latest" "$tagfile" || die 1 "couldn't read $base/latest"
      VERSION=$(tr -d ' \r\n' <"$tagfile"); rm -f "$tagfile"
    fi
    base="$base/download/$VERSION"
  elif [ "$VERSION" = latest ]; then
    base="https://github.com/$REPO/releases/latest/download"
  else
    base="https://github.com/$REPO/releases/download/$VERSION"
  fi
  file="media-core-engine-$os-$arch"

  # downloaded next to where it ends up, so the last step is one rename on the same disk
  mkdir -p "$BIN_DIR"
  new="$BIN_DIR/.media-core-engine.new"
  sums="$BIN_DIR/.media-core-engine.sums"
  trap 'rm -f "$new" "$sums"' EXIT
  trap 'rm -f "$new" "$sums"; exit 1' INT TERM
  say "Downloading $file ($VERSION)…"
  fetch "$base/$file" "$new" || die 1 "couldn't download $base/$file"
  fetch "$base/SHA256SUMS" "$sums" || die 1 "couldn't download SHA256SUMS"
  want=$(awk -v f="$file" '$2 == f || $2 == "*"f {print $1}' "$sums")
  [ -n "$want" ] || die 1 "SHA256SUMS has no line for $file"
  [ "$(sha "$new")" = "$want" ] || die 1 "the download doesn't match SHA256SUMS; nothing was installed"
  chmod 755 "$new"
  "$new" version >/dev/null 2>&1 || die 5 "the downloaded engine doesn't run on this computer; nothing was installed"

  bin="$BIN_DIR/media-core-engine"
  [ ! -f "$bin" ] || cp -p "$bin" "$bin.prev" # the one before, for going back by hand
  mv -f "$new" "$bin"
  say "Installed $bin ($("$bin" version))"

  if [ "$os" = darwin ]; then
    say ""
    say "On a Mac, Media Core doesn't install itself as a service. Run it yourself:"
    say "  $bin setup --library ~/Media --make-folders"
    say "  $bin serve"
    say "then, in another window, $bin setup-code, and open http://localhost:8780/#/setup"
    return 0
  fi

  set -- install
  if [ -n "${MEDIA_CORE_LIBRARY:-}" ]; then set -- "$@" --library "$MEDIA_CORE_LIBRARY" --make-folders; fi
  if [ -n "${MEDIA_CORE_NAME:-}" ]; then set -- "$@" --name "$MEDIA_CORE_NAME"; fi
  if [ "${MEDIA_CORE_YES:-}" = 1 ]; then set -- "$@" --yes; fi
  if [ "${MEDIA_CORE_NO_SERVICE:-}" = 1 ]; then set -- "$@" --no-service; fi
  if [ "${MEDIA_CORE_NO_APPS:-}" = 1 ]; then set -- "$@" --no-apps; fi
  export MEDIA_CORE_INSTALL_DIR="$BIN_DIR"
  # answers come from the terminal even when this script arrives through a pipe; only when one can be opened
  status=0
  if [ "${MEDIA_CORE_YES:-}" != 1 ] && [ ! -t 0 ] && (: </dev/tty) 2>/dev/null; then
    "$bin" "$@" </dev/tty || status=$?
  else
    "$bin" "$@" || status=$?
  fi
  return "$status"
}

say() { printf '%s\n' "$*"; }
die() {
  code=$1; shift
  printf 'media-core install: %s\n' "$*" >&2
  exit "$code"
}

main "$@"
