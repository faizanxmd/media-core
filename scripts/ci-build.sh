#!/bin/bash
# Build the apps on GitHub Actions, then publish them where the engine serves
# them, so the TV and phone offer the update.
# Usage: scripts/ci-build.sh [both|tv|phone]   build origin/main, wait, publish
#        scripts/ci-build.sh --latest          publish the newest finished build
#                                              (one started from the GitHub site or app)
# Env (optional): MEDIA_CORE_APK_DIR
set -euo pipefail
cd "$(dirname "$0")/.."

OUT="${MEDIA_CORE_APK_DIR:-$HOME/.local/share/media-core/app}"
WORKFLOW=build.yml

publish() {
  local tmp n
  tmp=$(mktemp -d)
  gh run download "$1" -n apks -D "$tmp"
  mkdir -p "$OUT"
  for n in media-core media-core-phone; do
    [ -f "$tmp/$n.apk" ] || continue
    # the APK first: the version file is what makes the apps offer it
    cp "$tmp/$n.apk" "$OUT/$n.apk.tmp" && mv "$OUT/$n.apk.tmp" "$OUT/$n.apk"
    cp "$tmp/$n.json" "$OUT/$n.json.tmp" && mv "$OUT/$n.json.tmp" "$OUT/$n.json"
    echo "published $n: $(cat "$OUT/$n.json")"
  done
  rm -rf "$tmp"
}

latest_run() {
  gh run list -w "$WORKFLOW" -e workflow_dispatch --limit 1 --json databaseId --jq '.[0].databaseId // ""'
}

if [ "${1:-}" = --latest ]; then
  id=$(gh api 'repos/{owner}/{repo}/actions/artifacts?name=apks&per_page=1' --jq '.artifacts[0].workflow_run.id // ""')
  [ -n "$id" ] || { echo "no finished build to publish" >&2; exit 1; }
  publish "$id"
  exit 0
fi

what="${1:-both}"
case "$what" in
  both|tv|phone) ;;
  *) echo "usage: $0 [both|tv|phone] | --latest" >&2; exit 2 ;;
esac

# GitHub builds what's on GitHub: don't let unpushed commits go missing silently
git fetch -q origin main
if [ "$(git rev-parse HEAD)" != "$(git rev-parse origin/main)" ]; then
  echo "local HEAD differs from origin/main: push or pull first" >&2
  exit 1
fi

before=$(latest_run)
gh workflow run "$WORKFLOW" --ref main -f what="$what"
id=""
for _ in $(seq 30); do
  sleep 2
  id=$(latest_run)
  [ "$id" != "$before" ] && break
  id=""
done
[ -n "$id" ] || { echo "the build didn't appear on GitHub" >&2; exit 1; }

echo "building $what from $(git rev-parse --short HEAD): run $id"
gh run watch "$id" --exit-status --interval 30 >/dev/null || {
  echo "build failed: gh run view $id --log-failed" >&2
  exit 1
}
publish "$id"
