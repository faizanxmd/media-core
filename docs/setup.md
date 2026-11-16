# Setting up Media Core

This turns a Linux computer at home into a Media Core server and connects the TV, phone and browser to it. Each step says what to run and how to check it worked. An AI agent doing this for someone should read [AGENTS.md](../AGENTS.md) first.

## 1. Install

On the computer that will be the server:

```sh
curl -fsSL https://github.com/faizanxmd/media-core/releases/latest/download/install.sh | sh
```

It downloads the engine for this computer, checks it against the release's checksums, puts it in `~/.local/bin`, and asks one question: where the library goes (default `~/Media`, with `Movies`, `TV Shows` and `Music` inside). It then starts the server as a background service and fetches the TV and phone apps. It never uses sudo. What needs sudo is printed at the end for you to run. Usually that's this:

```sh
sudo apt install aria2 ffmpeg p7zip-full mkvtoolnix   # or the dnf, pacman, zypper or brew line it prints
sudo loginctl enable-linger "$USER"                    # keeps the server running after you log out
```

To answer nothing (for scripts), set `MEDIA_CORE_YES=1`, and `MEDIA_CORE_LIBRARY=/path` for the library.

Check it:

```sh
media-core-engine doctor
```

Each line says `ok`, `warn` or `FAIL`, with the fix underneath. It exits 0 when all is well, 1 for warnings, 2 when something is broken. `--json` prints the same for a program to read.

## 2. Set it up in a browser

The install ends by printing a link and a setup code:

```
  http://192.168.1.20:8780/#/setup
  and enter the setup code K7MQ-2XPA
```

Open the link on a computer or phone at home and enter the code. That browser becomes the server's admin. The code lasts 24 hours and works once; `media-core-engine setup-code` makes a new one.

In Settings, Server, add a TMDB key (free at [themoviedb.org](https://www.themoviedb.org/settings/api)) so titles get posters and details. You can change the library folder and the server's name there too, or on the command line:

```sh
media-core-engine setup --tmdb-key KEY --name "Living room"
```

## 3. Firewall

Port 8780 is the website. Port 8790 is the apps' API (TCP) and how the apps find the server (UDP). If a firewall is on (`sudo ufw status`), allow both from the home network only, where `192.168.1.0/24` is yours:

```sh
sudo ufw allow from 192.168.1.0/24 to any port 8780 proto tcp
sudo ufw allow from 192.168.1.0/24 to any port 8790
```

Never forward either port to the internet. The server refuses anyone outside the home network anyway, but there's no reason to let them knock. See [SECURITY.md](../SECURITY.md).

## 4. Connect the TV and phone

On the phone, open `http://<server>:8790/` and install the app from there. On an Android TV, install it with a downloader app from `http://<server>:8790/api/v1/app.apk`. The app finds the server on the home network by itself.

Each app then shows a code. On the admin browser, open Settings, Devices and enter it, or scan the TV's QR code with the phone. The app goes on to Home by itself. Pair another browser the same way: open the site and it shows a code.

The apps offer an update whenever the server has a newer one.

## 5. Add something

Open `http://<server>:8780/`, go to Manager and paste a link. It shows what the link is before anything downloads. When the download finishes, the title shows up in the apps within a minute.

## Upgrading

```sh
media-core-engine upgrade
```

It waits for nothing to be playing or downloading, checks the new release's checksums, keeps the old version as `media-core-engine.prev`, and puts it back by itself if the new one doesn't start. It also fetches the matching apps. `--version v1.2.0` picks a release.

## Removing it

```sh
media-core-engine uninstall           # the services and the program; settings and progress stay
media-core-engine uninstall --purge   # those too (it asks you to type "delete")
```

Neither ever touches the library.

## Where things live

- `~/.local/bin/media-core-engine`: the program.
- `~/.config/media-core/`: settings and secrets. `engine.env` (settings, see [engine.env.example](../deploy/engine.env.example)), `devices.json` (paired devices; their keys are stored only as hashes), `tmdb.key`, `aria2.conf` (the downloader's settings and password) and `webdav.pass`.
- `~/.local/share/media-core/`: watch progress, downloads in progress, the TMDB cache, thumbnails, the apps it hands out, and backups.
- Three systemd user services: `media-core-engine`, `media-core-aria2` (the downloader) and `media-core-backup.timer`.

Logs: `journalctl --user -u media-core-engine -f`.

## Backups

Every night at 4:00 the server saves watch progress, settings, paired devices and title matches to `~/.local/share/media-core/backups/state/`, keeping 30. Secrets aren't included. Set `MEDIA_CORE_BACKUP_DIR` in `engine.env` to a USB stick or network share for a second copy that survives the disk; if it isn't there one night, only the local copy is made and the timer shows as failed. `MEDIA_CORE_BACKUP_KEEP` changes how many are kept. `media-core-engine backup` runs one now.

To restore one, stop the server, put the backup back and start it again. `restore` checks every file against the backup's list first, and saves the current files as `pre-restore-*.tgz` before replacing them. Secrets aren't in backups: on a new computer, set the TMDB key again with `setup`.

```sh
media-core-engine backup list
systemctl --user stop media-core-engine
media-core-engine restore ~/.local/share/media-core/backups/state/state-2026-01-02.tgz
systemctl --user start media-core-engine
```

When an upgrade changes how the server stores its data, it saves an `upgrade-*.tgz` backup before changing anything, and keeps the last five. A newer version's data is never touched by an older one: the older one refuses to start and says so.

## Trying it without installing

`media-core-engine serve --library ~/Videos` runs a temporary server for one folder until you press Ctrl-C. It keeps its settings in a temporary folder and removes them when it stops; `--state DIR` keeps them, and `--port N` moves it off 8780/8790 (the apps' port is N+10).

## macOS

The same one-liner works. The server runs as a LaunchAgent and starts aria2 itself. Install the tools with `brew install aria2 ffmpeg p7zip mkvtoolnix`.

## Building from source

See [docs/development.md](development.md). `deploy/install.sh` tests and builds the engine from this tree, then runs the same `install`.
