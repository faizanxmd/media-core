#!/bin/bash
# Build and (re)start the Media Core engine.  Usage: ./install.sh
set -euo pipefail
cd "$(dirname "$0")"
export PATH="$HOME/.local/go/bin:$PATH"
go vet ./...
go test ./...
go build -trimpath -ldflags='-s -w' -o "$HOME/.local/bin/media-core-engine.new" .
mv "$HOME/.local/bin/media-core-engine.new" "$HOME/.local/bin/media-core-engine"
systemctl --user daemon-reload
systemctl --user enable --now media-core-aria2.service media-core-engine.service >/dev/null
systemctl --user restart media-core-engine.service
sleep 1
systemctl --user --no-pager --lines=5 status media-core-engine.service | head -12
