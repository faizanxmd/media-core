# Setting up Media Core

This sets up the engine on a Linux box and connects the apps to it. Each step says what to run and how to check it worked. `<server>` means the box's host name or IP address (`hostname -I` shows the address).

## 0. Get the code

```sh
git clone https://github.com/faizanxmd/media-core.git && cd media-core
```

Every later command runs from this folder.

## 1. Install what the engine uses

On Debian or Ubuntu:

```sh
sudo apt install aria2 ffmpeg p7zip-full mkvtoolnix
```

Skip Intro needs ffmpeg's chromaprint support, which Debian and Ubuntu include. Check: `ffmpeg -hide_banner -muxers | grep chromaprint` prints a line.

You also need:

- **Go 1.27 or newer**, to build the engine:
  ```sh
  curl -LO https://go.dev/dl/go1.27.0.linux-amd64.tar.gz && mkdir -p ~/.local && tar -C ~/.local -xzf go1.27.0.linux-amd64.tar.gz
  ~/.local/go/bin/go version
  ```
  On an ARM box, use the `linux-arm64` file instead.
- **dovi_tool**, optional. It converts Dolby Vision profile 7 files (Blu-ray remuxes) so Android TVs play them in Dolby Vision; without it they play as HDR10. Skip it unless you have such files. Otherwise download the Linux release from github.com/quietvoid/dovi_tool and put the `dovi_tool` binary in `~/.local/bin/`.

## 2. Make the library folder

```sh
sudo mkdir -p /srv/data/media/{Movies,"TV Shows",Music}
sudo chown -R "$USER": /srv/data/media
```

Any folder works. If you use another, set `MEDIA_CORE_LIBRARY` in step 4.

## 3. Add a TMDB key

Posters and titles come from TMDB. Get a free v3 API key at themoviedb.org, then:

```sh
mkdir -p ~/.config/media-core && chmod 700 ~/.config/media-core
printf '%s' 'YOUR_KEY' > ~/.config/media-core/tmdb.key && chmod 600 ~/.config/media-core/tmdb.key
```

## 4. Settings (optional)

The engine works without a settings file. To change something, copy the example and edit it:

```sh
cp deploy/engine.env.example ~/.config/media-core/engine.env
```

It holds plain `KEY=value` lines:

| Key | Default | What it is |
| --- | --- | --- |
| `MEDIA_CORE_LIBRARY` | `/srv/data/media` | the library folder |
| `MEDIA_CORE_NAME` | the host name | the server's name in the apps ("mint is offline") |
| `MEDIA_CORE_LISTEN` | `:8780` | the website and Manager |
| `MEDIA_CORE_PLAYER_LISTEN` | `:8790` | the apps' API, and the UDP port the apps search on |
| `MEDIA_CORE_DOVI_TOOL` | `dovi_tool` on the PATH, else `~/.local/bin/dovi_tool` | the Dolby Vision tool |

A value set in the environment wins over the file. Write full paths: `~` isn't expanded. Two more settings work only as environment variables, since they say where this file lives: `MEDIA_CORE_CONFIG_DIR` (default `~/.config/media-core`) and `MEDIA_CORE_DATA_DIR` (default `~/.local/share/media-core`, for jobs, watch progress and caches). Leave both alone on a real install: `install.sh`, the downloader and the nightly backup always use the default folders.

## 5. Install and start the engine

```sh
sudo loginctl enable-linger "$USER"   # user services keep running after you log out
deploy/install.sh
```

`deploy/install.sh` builds and tests the engine, installs the binary to `~/.local/bin` and the systemd user units, and starts them. The first time, it also writes `~/.config/media-core/aria2.conf` with a random RPC secret. It never overwrites your settings, secrets or data, so run it again whenever you want new engine code to go live.

Check it:

```sh
systemctl --user status media-core-engine media-core-aria2
curl -s http://127.0.0.1:8790/api/v1/hello
curl -sI http://127.0.0.1:8780/ | head -1
```

Both services say `active (running)`, the second command prints the server's name and addresses, and the third prints `HTTP/1.1 200 OK`.

## 6. Firewall

Port 8780 is the website and Manager; port 8790 is the apps' API (TCP) and how the apps search for the server (UDP). Allow both from the home network only. With ufw (`sudo ufw status` shows whether it's on), where `192.168.1.0/24` is your home network:

```sh
sudo ufw allow from 192.168.1.0/24 to any port 8780 proto tcp
sudo ufw allow from 192.168.1.0/24 to any port 8790
```

With another firewall, open the same ports.

Never open either port to the internet. There are no accounts, so the engine refuses anything that isn't the home network or the box itself.

## 7. Connect the apps

On a phone, open `http://<server>:8790/` and install the app from there. On a TV, use a downloader app or `adb install` with `http://<server>:8790/api/v1/app.apk`. The app finds the server on the home network by itself. If it doesn't, type the server's address on the app's offline screen. Use away from home isn't supported yet.

Each app offers an update when a newer build is published on the server.

## 8. Add something

Open `http://<server>:8780/`, go to Manager, and paste a link. It shows what the link is before anything downloads. When the download finishes, the title appears in the apps within a minute.

## Where things live

None of this is in git.

- `~/.config/media-core/` holds settings and secrets: `engine.env`, `aria2.conf` (the downloader's settings, with the password the engine uses to talk to it), `tmdb.key`, `webdav.pass`, and, on the machine that builds the apps, the signing key `app.jks` with `app.jks.pass`.
- `webdav.pass` is made by the engine. With the user `media-core` and that password, players such as Infuse or VLC can browse the library read-only at `http://<server>:8790/dav`.
- `~/.local/share/media-core/` holds the runtime data: jobs, watch state, the TMDB cache, thumbnails, intros and the published APKs.
- `~/.local/bin/media-core-engine` is the engine.

Three systemd user units do the work: `media-core-engine`, `media-core-aria2`, and `media-core-backup.timer` for the nightly backup.

## Backups

Every night `media-core-backup` saves watch progress, the download settings, intro data and title matches to `~/.local/share/media-core/backups/state/`, and keeps 30 days. It does not include `engine.env`, `tmdb.key` or the signing key. If a USB stick is mounted at `/mnt/media-core-usb` (or `MEDIA_CORE_BACKUP_USB`), it saves a copy there too. Without the stick, the unit shows as failed each night, but the local copy is still made.

To restore, stop the engine, unpack the newest `state-*.tgz` into `~/.local/share/media-core/` and start the engine again.

A USB stick keeps the backup safe if the main disk dies. Find its UUID and filesystem type with `lsblk -f`, then add a line like this to `/etc/fstab`, with your own uid and gid:

```
UUID=<uuid> /mnt/media-core-usb exfat noauto,nofail,x-systemd.automount,x-systemd.idle-timeout=60,x-systemd.device-timeout=5,uid=1000,gid=1000,umask=077 0 0
```

```sh
sudo mkdir -p /mnt/media-core-usb
sudo systemctl daemon-reload && sudo systemctl start "$(systemd-escape -p --suffix=automount /mnt/media-core-usb)"
```

## The signing key

If you build the apps, **keep `app.jks` and `app.jks.pass` safe, with a copy off the box.** Every build must be signed with them. If the key is lost, installed apps won't take updates and have to be uninstalled, which wipes their offline downloads. The nightly backup does not include the key.
