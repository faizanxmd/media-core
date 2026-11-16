#!/bin/sh
# The one-line install, end to end, without GitHub or systemd: builds a release's files into a folder, serves
# them on localhost, runs install.sh in a throwaway home, starts the server, checks it, uninstalls with
# --purge and checks the library is still there. CI runs it on Debian, Fedora and Arch.
# Usage: scripts/test-install.sh [prebuilt-binary]
set -eu
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
WORK="$(mktemp -d)"
trap 'kill $SRV $ENGINE 2>/dev/null || true; rm -rf "$WORK"' EXIT INT TERM
SRV="" ENGINE=""
arch=$(uname -m); case "$arch" in x86_64) arch=amd64 ;; aarch64) arch=arm64 ;; esac
file="media-core-engine-linux-$arch"

mkdir -p "$WORK/dist" "$WORK/home" "$WORK/bin"
if [ -n "${1:-}" ]; then cp "$1" "$WORK/dist/$file"; else
  (cd "$ROOT/engine" && CGO_ENABLED=0 go build -trimpath -ldflags "-X main.version=0.0.0-test" -o "$WORK/dist/$file" .)
fi
cp "$ROOT/install.sh" "$WORK/dist/install.sh"
(cd "$WORK/dist" && sha256sum "$file" install.sh > SHA256SUMS)
(cd "$WORK/dist" && exec python3 -m http.server 28999 --bind 127.0.0.1 >/dev/null 2>&1) &
SRV=$!
sleep 1

# a PATH without systemctl: the install must work (and say how to start it) without a service manager
for t in sh cat mkdir mv rm chmod mktemp uname awk cut sha256sum curl aria2c ffmpeg ffprobe; do
  p=$(command -v "$t" 2>/dev/null) && ln -s "$p" "$WORK/bin/$t"
done
run() { env -i HOME="$WORK/home" USER=test PATH="$WORK/bin" MEDIA_CORE_LISTEN=:28980 MEDIA_CORE_PLAYER_LISTEN=:28990 "$@"; }

run MEDIA_CORE_BASE_URL=http://127.0.0.1:28999 MEDIA_CORE_YES=1 sh "$WORK/dist/install.sh"
B="$WORK/home/.local/bin/media-core-engine"
test -x "$B"
for d in Movies "TV Shows" Music; do test -d "$WORK/home/Media/$d"; done
grep -q "^MEDIA_CORE_LIBRARY=$WORK/home/Media\$" "$WORK/home/.config/media-core/engine.env"
test "$(stat -c %a "$WORK/home/.config/media-core")" = 700
test "$(stat -c %a "$WORK/home/.config/media-core/aria2.conf")" = 600

# a tampered download is refused
cp "$WORK/dist/$file" "$WORK/good"; printf x >> "$WORK/dist/$file"
if run MEDIA_CORE_BASE_URL=http://127.0.0.1:28999 MEDIA_CORE_YES=1 sh "$WORK/dist/install.sh" 2>/dev/null; then
  echo "FAIL: a download that doesn't match SHA256SUMS was installed" >&2; exit 1
fi
mv "$WORK/good" "$WORK/dist/$file"

# running again keeps the settings
run MEDIA_CORE_YES=1 "$B" install --yes --name "Test box" >/dev/null
grep -q '^MEDIA_CORE_NAME=Test box$' "$WORK/home/.config/media-core/engine.env"

env -i HOME="$WORK/home" USER=test PATH="$WORK/bin" MEDIA_CORE_LISTEN=:28980 MEDIA_CORE_PLAYER_LISTEN=:28990 \
  "$B" serve >"$WORK/serve.log" 2>&1 &
ENGINE=$!
i=0; until curl -fs http://127.0.0.1:28990/api/v1/hello >"$WORK/hello"; do i=$((i+1)); [ $i -lt 40 ] || { cat "$WORK/serve.log"; exit 1; }; sleep 0.25; done
grep -q '"name":"Test box"' "$WORK/hello"
i=0; until curl -s http://127.0.0.1:28980/ >/dev/null; do i=$((i+1)); [ $i -lt 40 ] || exit 1; sleep 0.25; done
code=$(curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:28980/api/jobs)
[ "$code" = 403 ] || { echo "FAIL: an unclaimed server answered /api/jobs with $code" >&2; exit 1; }
run "$B" doctor --json >"$WORK/doctor.json" || true
grep -q '"id": "server"' "$WORK/doctor.json"
kill "$ENGINE" 2>/dev/null || { echo "the server had stopped by itself:" >&2; cat "$WORK/serve.log" >&2; exit 1; }
wait "$ENGINE" 2>/dev/null || true
ENGINE=""

touch "$WORK/home/Media/Movies/keep-me"
run "$B" uninstall --purge --yes >/dev/null
test ! -e "$B" && test ! -e "$WORK/home/.config/media-core" && test ! -e "$WORK/home/.local/share/media-core"
test -f "$WORK/home/Media/Movies/keep-me"
echo "install test passed"
