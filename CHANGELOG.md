# Changelog

What changed for people using Media Core, newest first. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow [Semantic Versioning](https://semver.org/): the server, the apps and the API version (`"api"` in `/api/v1/hello`) move together in each release.

## [Unreleased]

### Added

- `backup list` and `restore FILE`. Backups now carry a manifest with each file's checksum, and `restore` refuses damaged or unexpected files.
- The data folder has a schema number (`state.json`). An upgrade that changes stored data backs it up first, and an older version refuses newer data instead of damaging it.

### Changed

- App versions follow the release tag (`1.2.3`, code 10203000) instead of the build time. Apps installed before 1.0.0 need one uninstall and reinstall.
- A release is one draft-then-published GitHub release with every file under a single `SHA256SUMS`; signing the apps needs the owner's approval in the `release` environment.

## [1.0.0]

The first public release.

- One-line install for Linux and macOS, with `install`, `setup`, `upgrade` (with automatic rollback), `uninstall`, `doctor` and `backup` commands.
- Every TV, phone and browser pairs with a code; an admin approves it. Two roles: player and admin.
- First-run setup in the browser with a one-time setup code: library, TMDB key, server name and the tools the server uses.
- Downloads from links, filed into Movies, TV Shows and Music with TMDB posters and details; Dolby Vision profile 7 converted for Android TVs.
- Android TV and phone apps that find the server on the home network by themselves, play files as they are, share watch progress, skip intros, and (phone) keep downloads for offline.
- A website with the TV app's look, a web player and Manager for downloads and files.
