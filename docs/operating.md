# Operating a server (for AI agents)

This page is for an AI agent installing, checking or running a Media Core server for someone. People should read [setup.md](setup.md). For changing the code, see [AGENTS.md](../AGENTS.md) and [CONTRIBUTING.md](../CONTRIBUTING.md).

## Never

- Never delete or overwrite `~/.config/media-core`, `~/.local/share/media-core` or the library folder. They hold the paired devices, everyone's watch progress and the person's media. `uninstall --purge` deletes the first two: run it only when the person asks for exactly that.
- Never use sudo without showing the person the exact command and getting their OK. Never run the install itself as root; it refuses.
- Never forward ports 8780 or 8790 to the internet, or set `MEDIA_CORE_HOSTS` to a public name, to "make it work away from home". It isn't supported, and the server refuses it.
- Never print, log or send anywhere the contents of `tmdb.key`, `aria2.conf`, `cli.token`, `devices.json`, `webdav.pass` or a setup code, except the setup code to the person who'll use it.
- Never upgrade, restart or uninstall while something is playing or downloading unless the person says so. The commands check this and exit 3; `--force` skips the check.

## Ask the person

- Where the library goes (a folder with room for their films and shows). Default: `~/Media`.
- Their TMDB key, if they want posters and details (free at themoviedb.org).
- Before any sudo command: installing tools, `loginctl enable-linger`, firewall rules.

## Install

```sh
curl -fsSL https://github.com/faizanxmd/media-core/releases/latest/download/install.sh \
  | MEDIA_CORE_YES=1 MEDIA_CORE_LIBRARY=/path/to/library MEDIA_CORE_TMDB_KEY=KEY sh
```

Other settings: `MEDIA_CORE_NAME` (the server's name), `MEDIA_CORE_VERSION`, `MEDIA_CORE_NO_APPS=1`, and `MEDIA_CORE_NO_SERVICE=1` where there's no systemd user session (then run `media-core-engine serve` yourself). The binary goes in `~/.local/bin`; if that isn't on `PATH`, use the full path, `~/.local/bin/media-core-engine`.

The install prints the sudo commands it needs (missing tools, linger) and, at the end, a link and a setup code. Give both to the person: they open the link in a browser at home and enter the code, which makes that browser the admin. Then check it: `media-core-engine doctor --json`. `media-core-engine install --json` prints the same result as one object (`service`, `urls`, `setup_url`, `setup_code`, `needs_sudo`, `warnings`).

## Exit codes

Every command: 0 done, 1 failed, 2 wrong usage, 3 busy (something plays or downloads; wait and try again), 4 partly done (for `upgrade`: it failed and the old version is running again), 5 not supported here (root, no systemd user session).

## Check

`media-core-engine doctor --json` prints:

```json
{"schema": 1, "version": "1.0.0", "os": "linux", "arch": "amd64", "config_dir": "...", "data_dir": "...",
 "status": "ok|warn|fail", "checks": [{"id": "aria2", "status": "ok", "message": "...", "fix": "..."}]}
```

Its exit code is 0 for ok, 1 for warnings only, 2 when something is broken. Each check that isn't ok has a `fix`: a command to run (ask before sudo) or a step for the person. Check ids: `library`, `disk`, `tool:<name>`, `config`, `state`, `devices`, `tmdb`, `service`, `units`, `linger`, `server`, `aria2`, `path`, `backup`, `apps`, and `update` with `--online` (doctor calls nothing outside the computer without it).

## Operate

| Task | Command |
| --- | --- |
| Queue a download | `media-core-engine add <url> [movie\|tv\|music] [title]` |
| List downloads | `media-core-engine ls` |
| List devices, and any waiting to pair | `media-core-engine devices` |
| Approve a device showing a code | `media-core-engine devices approve K7M-2XQ` (add `--admin` only if the person says so) |
| Remove a device | `media-core-engine devices revoke <id>` |
| New setup code | `media-core-engine setup-code` |
| Change library, name, TMDB key | `media-core-engine setup --library DIR --make-folders --name NAME --tmdb-key-file FILE` |
| Is there a newer release? | `media-core-engine upgrade --check` |
| Upgrade | `media-core-engine upgrade` (rolls back by itself if the new version doesn't start) |
| Fetch the apps again | `media-core-engine upgrade --apps` |
| Back up now | `media-core-engine backup` (exit 4: saved here, but the second copy failed) |
| List backups | `media-core-engine backup list` |
| Restore a backup | stop the server, then `media-core-engine restore FILE` (ask first: it replaces progress, settings and devices) |
| Uninstall | `media-core-engine uninstall --yes` (keeps settings and data; `--purge` deletes them, never the library) |
| Logs | `journalctl --user -u media-core-engine -n 100` |
| Restart | `systemctl --user restart media-core-engine` |

Most of these talk to the running server with the command line's own admin key (`~/.config/media-core/cli.token`), so they work only on the server itself. `install`, `setup`, `upgrade`, `apps`, `backup`, `uninstall` and `doctor` take `--json` for one object instead of text.

## Library layout

The server names and files downloads itself. When placing files by hand, use:

- `Movies/<Title> (<Year>)/<Title> (<Year>).<ext>`
- `TV Shows/<Show>/Season 01/<Show> - S01E01.<ext>`
- `Music/<Artist>/<Album>/`

Folders starting with `.` are ignored. New folders show up in the apps within a minute; no scan is needed.
