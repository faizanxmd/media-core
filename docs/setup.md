# Setting up Media Core

This turns a Linux computer at home into a Media Core server and connects the TV, phone and browser to it. Each step says what to run and how to check it worked. An AI agent doing this for someone should read [operating.md](operating.md) first.

**What you need:** a Linux computer with systemd (Debian, Ubuntu, Mint, Fedora, Arch and most others), on your home network, logged in as your normal user, and a folder for the library: one full of films and shows already, or an empty one.

## 1. Install

On the computer that will be the server, as your own user (not root):

```sh
curl -fsSL https://github.com/faizanxmd/media-core/releases/latest/download/install.sh | sh
```

It downloads the engine for this computer, checks it against the release's `SHA256SUMS`, puts it in `~/.local/bin`, and asks where your media is (default `~/Media`; it offers to make the folder with `Movies`, `TV Shows` and `Music` inside). It then starts the server as a systemd user service and fetches the TV and phone apps. It ends like this:

```
Media Core 1.0.0 is running. To finish, open this on a computer or phone at home:
  http://192.168.1.20:8780/#/setup
Setup code: K7MQ-2XPA   (lasts 24 hours; `media-core-engine setup-code` makes a new one)

Still to do, for you to run:
  sudo apt install aria2 ffmpeg p7zip-full mkvtoolnix   # aria2c, ffmpeg, ffprobe, 7z, mkvmerge
  sudo loginctl enable-linger you
```

It never uses sudo itself. The "still to do" lines are what it would like you to run: the tools it's missing, in your distribution's package names, and linger, without which the server stops when you log out. `dovi_tool`, which fixes Dolby Vision profile 7 for Android TVs, isn't packaged anywhere: put its binary from [its releases](https://github.com/quietvoid/dovi_tool/releases) in `~/.local/bin`.

To answer nothing, as a script or an AI agent would, set the answers instead:

```sh
curl -fsSL https://github.com/faizanxmd/media-core/releases/latest/download/install.sh \
  | MEDIA_CORE_YES=1 MEDIA_CORE_LIBRARY=/srv/media MEDIA_CORE_NAME="Living room" sh
```

`MEDIA_CORE_TMDB_KEY`, `MEDIA_CORE_VERSION`, `MEDIA_CORE_NO_APPS=1` and `MEDIA_CORE_NO_SERVICE=1` work the same way. If `~/.local/bin` isn't on your `PATH`, the install says so; until you add it, use the full path `~/.local/bin/media-core-engine`.

Check it:

```sh
media-core-engine doctor
```

Each line says `ok`, `warn` or `FAIL`, with the fix underneath. It exits 0 when all is well, 1 for warnings, 2 when something is broken. `--json` prints the same for a program to read.

## 2. Set it up in a browser

Open the link from the install on a computer or phone at home and enter the setup code. That browser becomes the server's admin. The code works once; `media-core-engine setup-code` makes a new one.

In Settings, Server, add a TMDB key (free at [themoviedb.org](https://www.themoviedb.org/settings/api)) so titles get posters and details. You can change the library folder and the server's name there too, or on the command line:

```sh
media-core-engine setup --name "Living room" --tmdb-key-file ~/tmdb.txt
```

A key in a file (or in `MEDIA_CORE_TMDB_KEY`) stays out of your shell history; `--tmdb-key KEY` works too. Saving restarts the server, so it waits until nothing is playing or downloading.

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

## Settings

The server reads `~/.config/media-core/engine.env`, one `KEY=value` per line; a real environment variable wins over it. `install`, `setup` and the site's Settings write the common ones. Every setting, with its default, is in [engine.env.example](../deploy/engine.env.example): the library, the name, the two ports, the backup's second folder and how many to keep, extra host names, and where `dovi_tool` is. Restart after editing it by hand: `systemctl --user restart media-core-engine`.

## Upgrading

```sh
media-core-engine upgrade --check   # is there a newer release?
media-core-engine upgrade
```

It refuses while something is playing or downloading (exit 3; `--force` skips that check). Otherwise it downloads the release, checks it against `SHA256SUMS` and that it runs here, backs up the settings and progress, keeps the old program as `media-core-engine.prev`, and starts the new one. If the new one doesn't answer within 45 seconds it puts the old one back by itself, data included, and exits 4. It also fetches the matching apps; `upgrade --apps` fetches them again on their own. `--version v1.2.0` picks a release.

To go back by hand: `cp ~/.local/bin/media-core-engine.prev ~/.local/bin/media-core-engine && systemctl --user restart media-core-engine`.

## Removing it

```sh
media-core-engine uninstall           # the services and the program; settings and progress stay
media-core-engine uninstall --purge   # those too
```

Both say what they'll remove and ask first (`--yes` skips the question), and wait while something is playing or downloading. Neither ever touches the library.

## Backups

Every night at about 4:00 the server saves watch progress, settings, paired devices and title matches to `~/.local/share/media-core/backups/state/`, keeping 30 (`MEDIA_CORE_BACKUP_KEEP` changes that). Secrets aren't included. `media-core-engine backup` makes one now.

For a copy that survives the disk, set `MEDIA_CORE_BACKUP_DIR` in `engine.env` to a USB stick or a network share. The folder must already exist: if the stick isn't plugged in one night, only the local copy is made and the timer shows as failed, so a copy never lands on the server's own disk by mistake. For a USB stick, mount it at a fixed place with a line like this in `/etc/fstab` (`sudo blkid` shows its UUID; `nofail` lets the computer start without it):

```
UUID=1234-ABCD  /mnt/media-core-usb  exfat  nofail,uid=1000,gid=1000  0  0
```

To restore one, stop the server, put the backup back and start it again. `restore` checks every file against the backup's list first, and saves the current files as `pre-restore-*.tgz` before replacing them. On a new computer, set the TMDB key again with `setup`.

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

The engine runs on a Mac, but it isn't installed as a service there, and it's less tested than Linux. The one-liner puts the program in `~/.local/bin` and prints the steps: install the tools with `brew install aria2 ffmpeg p7zip mkvtoolnix`, then

```sh
~/.local/bin/media-core-engine setup --library ~/Media --make-folders
~/.local/bin/media-core-engine serve
```

It runs while that window is open, and starts aria2 itself. macOS will ask whether it may accept connections, and may ask again after an upgrade. A library on an external drive or in Documents needs the permission macOS asks for. There's no nightly backup on a Mac: run `media-core-engine backup` yourself.

## Checking a download

Every release has a `SHA256SUMS` covering all its files, which the install and `upgrade` check, and a build attestation proving GitHub Actions built each file from this repository:

```sh
sha256sum -c SHA256SUMS --ignore-missing
gh attestation verify media-core-engine-linux-amd64 --repo faizanxmd/media-core
```

## Where things live

- `~/.local/bin/media-core-engine`: the program, and `.prev`, the one before the last upgrade.
- `~/.config/media-core/`: settings and secrets. `engine.env`, `devices.json` (paired devices; their keys are stored only as hashes), `tmdb.key`, `aria2.conf` (the downloader's settings and password), `cli.token` (the command line's admin key) and `webdav.pass`.
- `~/.local/share/media-core/`: watch progress, downloads in progress, the TMDB cache, thumbnails, the apps it hands out, and backups.
- Systemd user services: `media-core-engine`, `media-core-aria2` (the downloader) and `media-core-backup.timer`.

Logs: `journalctl --user -u media-core-engine -f`.

## When something's wrong

Start with `media-core-engine doctor`: every check that isn't ok says how to fix it. Then the logs, `journalctl --user -u media-core-engine -n 100`. The usual problems:

- **"No systemd user session"** (install exits 5). You're in a shell opened with `su` or `sudo -u`, or a container. Log in as that user normally (or over SSH), or set `XDG_RUNTIME_DIR=/run/user/$(id -u)`. Without systemd, `MEDIA_CORE_NO_SERVICE=1` installs anyway and you run `media-core-engine serve` yourself.
- **A port in use.** Another program has 8780 or 8790. Set `MEDIA_CORE_LISTEN=:8781` and `MEDIA_CORE_PLAYER_LISTEN=:8791` in `engine.env` and restart. The downloader picks its own free port at install.
- **The library isn't writable.** The server runs as you, so you must be able to write there: `touch /path/to/library/x`. For a disk mounted elsewhere, fix its owner or its mount options.
- **No ffmpeg**, or downloads that never start. Run the `sudo` line `doctor` gives for `tool:ffmpeg` or `tool:aria2c`.
- **The app can't find the server.** The phone or TV must be on the same network as the server, and the firewall must allow UDP and TCP 8790 (step 3). Guest Wi-Fi networks usually keep devices apart. As a last resort, type the server's address on the app's "can't find the server" screen.

## Building from source

See [development.md](development.md). `deploy/install.sh` tests and builds the engine from this tree, then runs the same `install`.
