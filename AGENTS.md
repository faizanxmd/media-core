# For AI agents

This page is for an AI agent installing, checking or operating a Media Core server for someone. People should read [docs/setup.md](docs/setup.md). For changing the code, see [CONTRIBUTING.md](CONTRIBUTING.md) and [docs/development.md](docs/development.md).

## Never

- Never delete or overwrite `~/.config/media-core`, `~/.local/share/media-core` or the library folder. They hold the paired devices, everyone's watch progress and the person's media. `uninstall --purge` deletes the first two: run it only when the person asks for exactly that.
- Never use sudo without showing the person the exact command and getting their OK.
- Never forward ports 8780 or 8790 to the internet, or set `MEDIA_CORE_HOSTS` to a public name, to "make it work away from home". It isn't supported, and the server refuses it.
- Never print, log or send anywhere the contents of `tmdb.key`, `aria2.conf`, `cli.token`, `devices.json`, `webdav.pass` or a setup code, except the setup code to the person who'll use it.
- Never upgrade or restart while something is playing or downloading unless the person says so. `upgrade` checks this; `--force` skips the check.

## Ask the person

- Where the library goes (a folder with room for their films and shows). Default: `~/Media`.
- Their TMDB key, if they want posters and details (free at themoviedb.org).
- Before any sudo command: installing tools, `loginctl enable-linger`, firewall rules.

## Install

```sh
curl -fsSL https://github.com/faizanxmd/media-core/releases/latest/download/install.sh \
  | MEDIA_CORE_YES=1 MEDIA_CORE_LIBRARY=/path/to/library sh
```

The install exits non-zero if it fails. It prints the sudo commands it needs (missing tools, linger) and, at the end, a link and a setup code. Give both to the person: they open the link in a browser at home and enter the code, which makes that browser the admin. Then:

```sh
media-core-engine setup --tmdb-key KEY      # if they gave one
media-core-engine doctor --json
```

## Check

`media-core-engine doctor --json` prints:

```json
{"version": "1.0.0", "status": "ok|warn|fail", "checks": [{"id": "aria2", "status": "ok", "message": "...", "fix": "..."}]}
```

The exit code is 0 for ok, 1 for warnings only, 2 when something is broken. Each check that isn't ok has a `fix`: a command to run (ask before sudo) or a step for the person. Check ids: `library`, `disk`, `tool:<name>`, `config`, `devices`, `tmdb`, `open-home`, `service`, `linger`, `server`, `aria2`.

## Operate

| Task | Command |
| --- | --- |
| Queue a download | `media-core-engine add <url> [movie\|tv\|music] [title]` |
| List downloads | `media-core-engine ls` |
| List devices, and any waiting to pair | `media-core-engine devices` |
| Approve a device showing a code | `media-core-engine devices approve K7M-2XQ` (add `--admin` only if the person says so) |
| Remove a device | `media-core-engine devices revoke <id>` |
| New setup code | `media-core-engine setup-code` |
| Change library, name, TMDB key | `media-core-engine setup --library DIR --name NAME --tmdb-key KEY` |
| Upgrade | `media-core-engine upgrade` |
| Fetch the apps again | `media-core-engine apps` |
| Back up now | `media-core-engine backup` (exit 4: saved here, but the second copy failed) |
| List backups | `media-core-engine backup list` |
| Restore a backup | stop the server, then `media-core-engine restore FILE` (ask first: it replaces progress, settings and devices) |
| Logs | `journalctl --user -u media-core-engine -n 100` |
| Restart | `systemctl --user restart media-core-engine` |

These commands talk to the running server with the command line's own admin key (`~/.config/media-core/cli.token`), so they work only on the server itself.

## Library layout

The server names and files downloads itself. When placing files by hand, use:

- `Movies/<Title> (<Year>)/<Title> (<Year>).<ext>`
- `TV Shows/<Show>/Season 01/<Show> - S01E01.<ext>`
- `Music/<Artist>/<Album>/`

Folders starting with `.` are ignored. New folders show up in the apps within a minute; no scan is needed.
