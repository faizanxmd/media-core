# Changelog

What changed for people using Media Core, newest first. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow [Semantic Versioning](https://semver.org/): the server, the apps and the API version (`"api"` in `/api/v1/hello`) move together in each release.

## [Unreleased]

## [1.0.0]

The first public release.

### Added

- Engine: one-line install for Linux with systemd, and the commands `install`, `setup`, `upgrade` (checked against `SHA256SUMS`, with automatic rollback), `uninstall`, `doctor` (with `--json` for scripts and agents), `backup`, `backup list` and `restore`.
- Engine: downloads from links, filed into Movies, TV Shows and Music with TMDB posters and details; Dolby Vision profile 7 converted for Android TVs.
- Engine: versioned state. An upgrade that changes stored data backs it up first; an older version refuses newer data instead of damaging it.
- Engine: a nightly backup of progress, settings and devices, with an optional second copy on a USB stick or share.
- TV app and Phone app: find the server on the home network by themselves, play files as they are, share watch progress, skip intros; the phone keeps downloads for offline.
- Site: the TV app's look, a web player, first-run setup with a one-time code, and Manager for downloads and files.

### Security

- Every TV, phone and browser pairs with a code that an admin approves. Two roles: player and admin. Keys are stored only as hashes.
- The server answers only its home network and refuses unknown host names (DNS rebinding).
- The apps send their key only to a server that proves it knows it.
- Releases carry build attestations, and the apps are signed with one key in a protected environment.
