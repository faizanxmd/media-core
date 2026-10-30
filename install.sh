#!/bin/bash
# Build and (re)start Theater.  Usage: ./install.sh
set -euo pipefail
cd "$(dirname "$0")"
export PATH="$HOME/.local/go/bin:$PATH"
go vet ./...
go test ./...
go build -trimpath -ldflags='-s -w' -o "$HOME/.local/bin/theater.new" .
mv "$HOME/.local/bin/theater.new" "$HOME/.local/bin/theater"
systemctl --user daemon-reload
systemctl --user enable --now theater-aria2.service theater.service >/dev/null
systemctl --user restart theater.service
sleep 1
systemctl --user --no-pager --lines=5 status theater.service | head -12
