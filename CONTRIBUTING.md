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

Read [AGENTS.md](AGENTS.md) for the full list of codebase rules and invariants (single source of truth for both humans and AI agents). In short:

- Engine: `cd engine && gofmt -l . && go vet ./... && go test -race ./...` must pass; Go standard library only; tests use `t.TempDir()`.
- Shell scripts: `shellcheck -s sh`.
- Apps: CI compiles both on every push (`android/core` for shared API/models; `android/app` and `android/phone` for UI/players).
- Add a line to `CHANGELOG.md` under "Unreleased" for anything a user would notice, starting with the part it touches (`Engine:`, `TV app:`, `Phone app:`, `Site:`).

## Pull requests

One change per pull request, with a short description of what it changes for someone using Media Core and how you checked it. CI must pass: engine tests, the app compile, the install test on Debian, Fedora and Arch, and the private-data checks.

By contributing you agree that your work is licensed under the [GPL-3.0](LICENSE), like the rest of the project.

## Using an AI agent

The same rules apply to changes made with an AI agent, and you're responsible for what it writes. [AGENTS.md](AGENTS.md) holds the coding rules; [docs/operating.md](docs/operating.md) is for agents running a server.

## Releases

A maintainer tags `vX.Y.Z` on `main`. The Release workflow tests, builds the server for Linux and macOS, builds and signs both apps after a maintainer approves the signing step, and publishes them with checksums, attestations and the notes from `CHANGELOG.md`. Before tagging, move the Unreleased lines under a new `## [X.Y.Z] - YYYY-MM-DD` heading; the release stops without one.
