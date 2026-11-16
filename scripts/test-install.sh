#!/bin/sh
# The one-line install, end to end, without GitHub or systemd: builds a release's files into a folder, serves
# them on localhost, runs install.sh in a throwaway home, starts the server, checks it, uninstalls with
# --purge and checks the library is still there. CI runs it on Debian, Fedora and Arch.
# Usage: scripts/test-install.sh [binary-0.0.0-test binary-0.0.1-test]   (prebuilt; else built here with Go)
set -eu
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
WORK="$(mktemp -d)"
trap 'kill $SRV $ENGINE 2>/dev/null || true; rm -rf "$WORK"' EXIT INT TERM
SRV="" ENGINE=""
arch=$(uname -m); case "$arch" in x86_64) arch=amd64 ;; aarch64) arch=arm64 ;; esac
file="media-core-engine-linux-$arch"

# a release host laid out as MEDIA_CORE_BASE_URL expects: latest, download/<tag>/<file>
T1=v0.0.0-test T2=v0.0.1-test
D1="$WORK/rel/download/$T1" D2="$WORK/rel/download/$T2"
mkdir -p "$D1" "$D2" "$WORK/home" "$WORK/bin"
if [ -n "${1:-}" ]; then cp "$1" "$D1/$file"; else
  (cd "$ROOT/engine" && CGO_ENABLED=0 go build -trimpath -ldflags "-X main.version=${T1#v}" -o "$D1/$file" .)
fi
if [ -n "${2:-}" ]; then cp "$2" "$D2/$file"; else
  (cd "$ROOT/engine" && CGO_ENABLED=0 go build -trimpath -ldflags "-X main.version=${T2#v}" -o "$D2/$file" .)
fi
for d in "$D1" "$D2"; do cp "$ROOT/install.sh" "$d/install.sh"; (cd "$d" && sha256sum "$file" install.sh > SHA256SUMS); done
echo "$T1" > "$WORK/rel/latest"
(cd "$WORK/rel" && exec python3 -m http.server 28999 --bind 127.0.0.1 >/dev/null 2>&1) &
SRV=$!
sleep 1

# a PATH without systemctl: the install must work with --no-service (and refuse without it)
for t in sh cat cp id tr mkdir mv rm chmod mktemp uname awk cut sha256sum curl aria2c ffmpeg ffprobe; do
  p=$(command -v "$t" 2>/dev/null) && ln -s "$p" "$WORK/bin/$t"
done
run() { env -i HOME="$WORK/home" USER=test PATH="$WORK/bin" MEDIA_CORE_LISTEN=:28980 MEDIA_CORE_PLAYER_LISTEN=:28990 \
  MEDIA_CORE_BASE_URL=http://127.0.0.1:28999 "$@"; }

set +e
run MEDIA_CORE_YES=1 MEDIA_CORE_NO_APPS=1 sh "$D1/install.sh" >/dev/null 2>&1
code=$?
set -e
[ "$code" = 5 ] || { echo "FAIL: install without systemd exited $code, not 5" >&2; exit 1; }

run MEDIA_CORE_YES=1 MEDIA_CORE_NO_SERVICE=1 MEDIA_CORE_NO_APPS=1 MEDIA_CORE_LIBRARY="$WORK/home/Media" sh "$D1/install.sh"
B="$WORK/home/.local/bin/media-core-engine"
test -x "$B"
for d in Movies "TV Shows" Music; do test -d "$WORK/home/Media/$d"; done
grep -q "^MEDIA_CORE_LIBRARY=$WORK/home/Media\$" "$WORK/home/.config/media-core/engine.env"
test "$(stat -c %a "$WORK/home/.config/media-core")" = 700
test "$(stat -c %a "$WORK/home/.config/media-core/aria2.conf")" = 600

# a tampered download is refused, and the installed binary stays
cp "$D1/$file" "$WORK/good"; printf x >> "$D1/$file"
if run MEDIA_CORE_YES=1 MEDIA_CORE_NO_SERVICE=1 MEDIA_CORE_NO_APPS=1 sh "$D1/install.sh" 2>/dev/null; then
  echo "FAIL: a download that doesn't match SHA256SUMS was installed" >&2; exit 1
fi
mv "$WORK/good" "$D1/$file"
test "$("$B" version)" = "media-core-engine ${T1#v}"

# running again keeps the settings
run "$B" install --yes --no-service --no-apps --name "Test box" >/dev/null
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

# upgrade with no service manager: swap, keep .prev, say to restart, exit 0
run "$B" upgrade --version "$T2" >/dev/null
test "$("$B" version)" = "media-core-engine ${T2#v}"
test "$("$B.prev" version)" = "media-core-engine ${T1#v}"

touch "$WORK/home/Media/Movies/keep-me"
run "$B" uninstall --purge --yes >/dev/null
test ! -e "$B" && test ! -e "$WORK/home/.config/media-core" && test ! -e "$WORK/home/.local/share/media-core"
test -f "$WORK/home/Media/Movies/keep-me"
echo "install test passed"
