# For AI coding agents

Rules for changing this code. To install or run a server for someone, read [docs/operating.md](docs/operating.md) instead. People: [CONTRIBUTING.md](CONTRIBUTING.md) says the same at more length.

## Layout

- `engine/`: the server, Go, standard library only. The website is `engine/web`, plain JavaScript, built into the binary.
- `android/core`: what both apps use to talk to the server (API, models, offline copies, thumbnails). Fix those once, there.
- `android/app` (TV) and `android/phone`: screens and each app's own player. A fix in the player goes in both.
- [docs/architecture.md](docs/architecture.md) explains how they fit; [docs/api.md](docs/api.md) lists the API.

## Check before you finish

```sh
cd engine && gofmt -l . && go vet ./... && go test -race ./...
```

`scripts/test-install.sh` runs the one-line install end to end in a throwaway home.

## Rules

- **Tests never touch real state.** `engine/main_test.go` points every state path at a temporary folder; new tests use `t.TempDir()` and never call the real `systemctl`, `loginctl` or a running server (the package variables in `engine/cli.go` stand in for them).
- **Watch progress has one source of truth**, `engine/watch.go`. An item is not started, part-way or finished, never two at once. Rules about progress go there, not in an app.
- **Every route needs a device key** unless it's listed in `openRoute` (`engine/auth.go`); `TestEveryRouteNeedsAKey` checks it. New `/api/v1` routes go in `docs/api.md` (`TestAPIDocListsEveryRoute`).
- **State files**: a change of shape needs a migration and a higher `stateSchema` (`engine/migrate.go`).
- **Public names stay**: `/api/v1`, `doctor` check ids, `--json` field names and exit codes. Add; never rename or remove.
- **Nothing private in git.** Run `git config core.hooksPath .githooks` once; the hook runs `scripts/check-private.sh`. No real addresses, host names, home paths, emails or keys, in code, tests or docs: use `10.0.0.x`, `/srv/...` and made-up names.
- **Don't build or sign the apps** (Gradle, `android/build.sh`, `scripts/ci-build.sh`) or push tags unless the person you're working for asks: CI compiles them on every push, and a `v*` tag publishes a release.
- **Don't run anything against a live server** (installs, restarts, deploys) unless asked.
- Commits: one change each, with a `CHANGELOG.md` line under Unreleased when a user would notice it.
