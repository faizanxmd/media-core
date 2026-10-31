#!/bin/bash
# Saves what the engine can't rebuild: watch progress, settings, intro data and title matches.
# The library, TMDB cache and thumbnails aren't included: they're too big or can be made again.
# A dated copy goes to the main disk, and one to a USB stick when one is mounted, so it survives that disk dying.
# Exits 1 if the stick is missing, so the unit shows as failed; the local copy is still made.
set -euo pipefail
DATA="$HOME/.local/share/media-core"
LOCAL="$DATA/backups/state"
USB="${MEDIA_CORE_BACKUP_USB:-/mnt/media-core-usb}"
KEEP=30

prune() { # newest $KEEP stay
  find "$1" -maxdepth 1 -name 'state-*.tgz' -printf '%f\n' | sort -r | tail -n +$((KEEP + 1)) | while read -r f; do rm -f "$1/$f"; done
}

umask 077
mkdir -p "$LOCAL"
cd "$DATA"
files=()
for f in watch.json settings.json intros.json tmdb/matches.json; do
  [ -f "$f" ] && files+=("$f")
done
[ -f watch.json ] || echo "warning: no watch.json to back up" >&2
[ ${#files[@]} -gt 0 ] || { echo "nothing to back up in $DATA" >&2; exit 1; }

out="$LOCAL/state-$(date +%F).tgz"
tar -czf "$out.tmp" "${files[@]}"
mv "$out.tmp" "$out"
prune "$LOCAL"
echo "saved $out: ${files[*]}"

# The stick mounts on demand. Without it the folder can't be made, because the mount point is root's.
if ! timeout 20 mkdir -p "$USB/media-core-backup" 2>/dev/null; then
  echo "no USB stick at $USB: only the local copy was made" >&2
  exit 1
fi
cp "$out" "$USB/media-core-backup/$(basename "$out").tmp"
mv "$USB/media-core-backup/$(basename "$out").tmp" "$USB/media-core-backup/$(basename "$out")"
sync -f "$USB/media-core-backup"
prune "$USB/media-core-backup"
echo "copied to $USB/media-core-backup"
