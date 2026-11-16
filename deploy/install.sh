#!/bin/bash
# Build the engine, install it as a systemd user service and (re)start it.
# Safe to re-run: existing config and secrets in ~/.config/media-core are kept.
# Usage: deploy/install.sh
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
CONF="$HOME/.config/media-core"
UNITS="$HOME/.config/systemd/user"
export PATH="$HOME/.local/go/bin:$PATH"

for tool in go aria2c ffmpeg ffprobe 7z mkvmerge; do
  command -v "$tool" >/dev/null || echo "warning: $tool not found (see docs/setup.md)" >&2
done
{ command -v dovi_tool || [ -x "$HOME/.local/bin/dovi_tool" ]; } >/dev/null || echo "warning: dovi_tool missing: Dolby Vision P7 -> 8.1 conversion will fail" >&2

# config: aria2 with a random RPC secret, first time only
mkdir -p "$CONF" "$HOME/.local/share/media-core" "$HOME/.local/bin" "$UNITS"
chmod 700 "$CONF"
if [ ! -f "$CONF/aria2.conf" ]; then
  secret=$(head -c 24 /dev/urandom | base64 | tr -d '/+=')
  (umask 077; sed -e "s|@HOME@|$HOME|g" -e "s|@SECRET@|$secret|" "$ROOT/deploy/aria2.conf.in" > "$CONF/aria2.conf")
  echo "wrote $CONF/aria2.conf"
fi
[ -f "$CONF/tmdb.key" ] || echo "warning: no $CONF/tmdb.key (TMDB API key): posters and titles won't load" >&2

# engine binary
cd "$ROOT/engine"
go vet ./...
go test ./...
go build -trimpath -ldflags='-s -w' -o "$HOME/.local/bin/media-core-engine.new" .
mv "$HOME/.local/bin/media-core-engine.new" "$HOME/.local/bin/media-core-engine"

# nightly backup of watch progress and settings
install -m 755 "$ROOT/deploy/backup-state.sh" "$HOME/.local/bin/media-core-backup"

# services
for u in media-core-aria2.service media-core-engine.service media-core-backup.service media-core-backup.timer; do
  cmp -s "$ROOT/deploy/$u" "$UNITS/$u" || { cp "$ROOT/deploy/$u" "$UNITS/$u"; echo "installed $UNITS/$u"; }
done
systemctl --user daemon-reload
systemctl --user enable --now media-core-aria2.service media-core-engine.service media-core-backup.timer >/dev/null
systemctl --user restart media-core-engine.service
sleep 1
systemctl --user --no-pager --lines=5 status media-core-engine.service | head -12
