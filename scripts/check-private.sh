#!/bin/bash
# Fails if a file holds something private: a tailnet name, a home or Tailscale
# address, a home path, an email, a private key, or any string in a private
# denylist kept outside the repo. Prints file:line only, never the match, so
# its output is safe in public CI logs.
#
# Usage: scripts/check-private.sh            staged changes (the pre-commit hook)
#        scripts/check-private.sh --all      every tracked file
#        scripts/check-private.sh --history  every commit on every branch
# Denylist: one string per line (# comments ok) in $MEDIA_CORE_DENYLIST or
# ~/.config/media-core/private-denylist. Put your tailnet name, IPs and host
# names there.
set -uo pipefail
cd "$(git rev-parse --show-toplevel)"

mode=${1:---staged}
deny=${MEDIA_CORE_DENYLIST:-$HOME/.config/media-core/private-denylist}

# name|regex (PCRE). Documentation ranges stay allowed: 192.168.1.x, 10.0.0.x,
# 100.64.0.x, and noreply / example addresses.
rules=(
  "tailnet name|\b[a-z0-9-]+\.tail[0-9a-f]{4,}\.ts\.net\b|\btail[0-9a-f]{6}\b"
  "Tailscale address|\b100\.(6[4-9]|[7-9][0-9]|1[01][0-9]|12[0-7])\.(?!0\.)[0-9]{1,3}\.[0-9]{1,3}\b"
  "home network address|\b192\.168\.(?!1\.|0\.)[0-9]{1,3}\.[0-9]{1,3}\b|\b10\.(?!0\.0\.)[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}\b"
  "home path|/home/(?!runner/|user/)[a-z_][a-z0-9_-]*/|/Users/(?!runner/|user/)[A-Za-z0-9_-]+/"
  "email address|(?<![\w.])(?!(this|git)@)[A-Za-z0-9._%+-]+@(?!users\.noreply\.github\.com\b|anthropic\.com\b|example\.(com|org)\b)[a-z0-9-]+(\.[a-z0-9-]+)*\.[a-z]{2,}\b"
  "private key|-----BEGIN [A-Z ]*PRIVATE KEY-----"
)
skip='(scripts/check-private\.sh|engine/web/vendor/|android/gradle/wrapper/)'

tmp=$(mktemp); trap 'rm -f "$tmp" "$tmp.l"' EXIT

# collect "file:line:content" for whatever the mode covers
case "$mode" in
  --staged)
    git diff --cached --name-only --diff-filter=ACMR -z | while IFS= read -r -d '' f; do
      git show ":$f" 2>/dev/null | grep -nI '' | sed "s|^|$f:|"
    done ;;
  --all)
    git grep -nI '' -- . ;;
  --history)
    # every line ever added, labelled commit/file (the number is a position in the log)
    git log --all -p --no-color --format='commit %h' | awk '
      /^commit /{c=$2; next}
      /^\+\+\+ b\//{f=substr($0,7); next}
      /^\+[^+]/{print c "/" f ":" NR ":" substr($0,2)}' ;;
  *) echo "usage: $0 [--staged|--all|--history]" >&2; exit 2 ;;
esac | grep -vP "^(\w+/)?$skip" > "$tmp"

out=""
for r in "${rules[@]}"; do
  name=${r%%|*}; re=${r#*|}
  hits=$(cut -d: -f3- "$tmp" | grep -nP -- "$re" | cut -d: -f1)
  for n in $hits; do out+="private data ($name): $(sed -n "${n}p" "$tmp" | cut -d: -f1,2)"$'\n'; done
done
if [ -s "$deny" ]; then
  grep -v '^\s*#' "$deny" | grep -v '^\s*$' > "$tmp.l"
  if [ -s "$tmp.l" ]; then
    hits=$(cut -d: -f3- "$tmp" | grep -niF -f "$tmp.l" | cut -d: -f1)
    for n in $hits; do out+="private data (denylist): $(sed -n "${n}p" "$tmp" | cut -d: -f1,2)"$'\n'; done
  fi
fi

if [ -n "$out" ]; then
  printf '%s' "$out" | sort -u
  exit 1
fi
