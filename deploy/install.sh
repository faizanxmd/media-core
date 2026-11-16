#!/bin/sh
# Install the engine from this source tree (for development; everyone else uses the one-line install).
# Tests and builds it, keeps the old binary as .prev, then runs `media-core-engine install --yes`, which keeps
# every setting, rewrites the service files only if they changed, and restarts the engine.
# Usage: deploy/install.sh [install flags, e.g. --library DIR]
set -eu
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BIN="$HOME/.local/bin/media-core-engine"
PATH="$HOME/.local/go/bin:$PATH"
command -v go >/dev/null || { echo "needs Go: https://go.dev/dl/" >&2; exit 1; }

cd "$ROOT/engine"
test -z "$(gofmt -l .)" || { gofmt -l .; echo "run gofmt -w ." >&2; exit 1; }
go vet ./...
go test -count=1 ./...
ver="$(git -C "$ROOT" describe --tags --match "v[0-9]*" --dirty 2>/dev/null || echo "dev-$(git -C "$ROOT" rev-parse --short HEAD 2>/dev/null)")"
mkdir -p "$(dirname "$BIN")"
go build -trimpath -ldflags "-s -w -X main.version=${ver#v}" -o "$BIN.new" .
[ -f "$BIN" ] && cp -p "$BIN" "$BIN.prev"
mv "$BIN.new" "$BIN"
exec "$BIN" install --yes "$@"
