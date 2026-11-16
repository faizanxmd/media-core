# Setting up Media Core

This sets up the engine on a Linux box and connects the apps to it. Each step says what to run and how to check it worked.

## 1. Install what the engine uses

On Debian or Ubuntu:

```sh
sudo apt install aria2 ffmpeg p7zip-full mkvtoolnix
```

Ubuntu's and Debian's ffmpeg include the chromaprint muxer that Skip Intro uses. Check with `ffmpeg -hide_banner -muxers | grep chromaprint`.

You also need:

- **Go 1.27 or newer**, to build the engine. Unpack the official tarball to `~/.local/go`.
- **dovi_tool 2.3.x**, for Dolby Vision profile 7 files. Put it in `~/.local/bin/dovi_tool` (from github.com/quietvoid/dovi_tool releases). It's optional: without it, profile 7 files play as HDR10.
- **Tailscale**, optional, for use away from home.

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
| `MEDIA_CORE_PUBLIC_HOST` | asked of Tailscale | a host name the apps can use away from home |
| `MEDIA_CORE_LISTEN` | `:8780` | the website and Manager |
| `MEDIA_CORE_PLAYER_LISTEN` | `:8790` | the apps' API, and the UDP port the apps search on |
| `MEDIA_CORE_DATA_DIR` | `~/.local/share/media-core` | jobs, watch progress, caches |
| `MEDIA_CORE_CONFIG_DIR` | `~/.config/media-core` | this file and the secrets |
| `MEDIA_CORE_DOVI_TOOL` | `dovi_tool` on the PATH, else `~/.local/bin/dovi_tool` | the Dolby Vision tool |

A value set in the environment wins over the file.

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
```

The second command prints the server's name and addresses.

## 6. Firewall

Allow port 8780 on `tailscale0` only. Allow port 8790, TCP and UDP, on the home network and on `tailscale0`. With ufw, where `192.168.1.0/24` is your home network:

```sh
sudo ufw allow in on tailscale0 to any port 8780 proto tcp
sudo ufw allow in on tailscale0 to any port 8790
sudo ufw allow from 192.168.1.0/24 to any port 8790
```

Never open either port to the internet: there are no accounts, and the home network is trusted.

## 7. Connect the apps

On a phone, open `http://<server>:8790/` and install the app from there. On a TV, use a downloader app or `adb install` with `http://<server>:8790/api/v1/app.apk`. At home the app finds the server by itself. Away from home, open the same page over Tailscale after installing and tap **Open in Media Core**. If neither works, type the server's address on the app's offline screen.

Each app offers an update when a newer build is published on the server.

## Where things live

None of this is in git.

- `~/.config/media-core/` holds settings and secrets: `engine.env`, `aria2.conf` (with the RPC secret), `tmdb.key`, `webdav.pass`, and, on the machine that builds the apps, the signing key `app.jks` with `app.jks.pass`.
- `~/.local/share/media-core/` holds the runtime data: jobs, watch state, the TMDB cache, thumbnails, intros and the published APKs.
- `~/.local/bin/media-core-engine` is the engine.

Three systemd user units do the work: `media-core-engine`, `media-core-aria2`, and `media-core-backup.timer` for the nightly backup.

## Backups

Every night `media-core-backup` saves watch progress, settings, intro data and title matches to `~/.local/share/media-core/backups/state/`, and keeps 30 days. If a USB stick is mounted at `/mnt/media-core-usb` (or `MEDIA_CORE_BACKUP_USB`), it saves a copy there too.

To restore, stop the engine, unpack the newest `state-*.tgz` into `~/.local/share/media-core/` and start the engine again.

A USB stick keeps the backup safe if the main disk dies. Find its UUID and filesystem type with `lsblk -f`, then add a line like this to `/etc/fstab`, with your own uid and gid:

```
UUID=<uuid> /mnt/media-core-usb exfat noauto,nofail,x-systemd.automount,x-systemd.idle-timeout=60,x-systemd.device-timeout=5,uid=1000,gid=1000,umask=077 0 0
```

```sh
sudo mkdir -p /mnt/media-core-usb
sudo systemctl daemon-reload && sudo systemctl start mnt-media\\x2dcore\\x2dusb.automount
```

## The signing key

If you build the apps, **keep `app.jks` and `app.jks.pass` safe, with a copy off the box.** Every build must be signed with them. If the key is lost, installed apps won't take updates and have to be uninstalled, which wipes their offline downloads. The nightly backup does not include the key.
