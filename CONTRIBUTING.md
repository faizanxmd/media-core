# Contributing

Thanks for helping. Bug reports, fixes and ideas are all welcome; for a bigger change, open an issue first so we can agree on the shape before you build it.

## Set up

```sh
git clone https://github.com/faizanxmd/media-core.git && cd media-core
git config core.hooksPath .githooks
```

The pre-commit hook runs `scripts/check-private.sh`, which stops a commit that holds a home path, a private IP address, an email or a key. Put your own extra strings in `~/.config/media-core/private-denylist`, one per line.

[docs/development.md](docs/development.md) explains building the engine and the apps and running a temporary server against a folder of videos.

## Before you send a change

- Engine: `cd engine && gofmt -l . && go vet ./... && go test ./...` are clean. Tests never touch real state: `main_test.go` points every path at a temporary folder, and new tests use `t.TempDir()`.
- A new API route needs a device key unless it's in `openRoute` (`engine/auth.go`); `TestEveryRouteNeedsAKey` checks this.
- Apps: CI compiles both on every push. Fixes to talking to the server go in `android/core` once; each app keeps its own screens and player, so a player fix goes in both.
- Watch progress has one source of truth, `engine/watch.go`. Rules about progress belong there, not in an app.
- Shell scripts pass `shellcheck -s sh`.
- Add a line to `CHANGELOG.md` under "Unreleased" for anything a user would notice, starting with the part it touches ("Engine:", "TV app:", "Phone app:", "Site:").
- A change to the shape of a state file needs a migration and a higher `stateSchema` (`engine/migrate.go`), so upgrades move old data forward and older versions refuse the new data.
- `doctor` check ids, `--json` field names and everything under `/api/v1` are public: add to them, never rename or remove. `docs/api.md` must list every `/api/v1` route (`TestAPIDocListsEveryRoute` checks).
- The engine uses the Go standard library only, and `go test -race` passes.

## Pull requests

One change per pull request, with a short description of what it changes for someone using Media Core and how you checked it. CI must pass: engine tests, the app compile, the install test on Debian, Fedora and Arch, and the private-data checks.

By contributing you agree that your work is licensed under the [GPL-3.0](LICENSE), like the rest of the project.

## Using an AI agent

The same rules apply to changes made with an AI agent, and you're responsible for what it writes. An agent must not build or sign the apps, or run anything against a live server. [AGENTS.md](AGENTS.md) is for agents operating a server, not for changing the code.

## Releases

A maintainer tags `vX.Y.Z` on `main`. The Release workflow tests, builds the server for Linux and macOS, builds and signs both apps after a maintainer approves the signing step, and publishes them with checksums, attestations and the notes from `CHANGELOG.md`. Before tagging, move the Unreleased lines under a new `## [X.Y.Z] - YYYY-MM-DD` heading; the release stops without one.
