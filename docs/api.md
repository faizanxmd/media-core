# Engine API

Two ports. The site's port (8780) serves the website and the admin API under `/api/`. The apps' port (8790) serves the apps' API under `/api/v1/`, streaming, the download page and WebDAV. Both answer the home network only, and every route needs a device key (`Authorization: Bearer mc_…`, or the browser's session cookie) except the open ones below. JSON in, JSON out; errors are `{"ok": false, "error": "<code>", "message": "<for people>"}`.

`/api/v1/hello` carries `"api": 1`. It goes up only when an older app can no longer talk to the server.

## Open (no key)

| Route | What |
| --- | --- |
| `GET /api/v1/hello` | name, `lan` addresses, `remote` addresses (when an add-on serves them), `api`, `pair`, `version`, the TV app's version |
| `POST /api/v1/pair/start` `{kind: tv\|phone\|web, name}` | a code to show: `{code, poll, url, expires_in, interval}` |
| `POST /api/v1/pair/poll` `{poll}` | `{state: pending\|approved\|denied\|expired}`; on approved, an app gets `{token, role, name}` once (a browser gets its cookie instead) |
| `GET /api/v1/prove?k=<16 hex>&n=<nonce>` | `{mac}`: HMAC-SHA256 of the nonce, keyed by the hash of the key whose hash starts with k; 404 if none. Lets an app check the server before sending its key |
| `GET /api/setup/state` | `{claimed, name, locked_s?}` |
| `POST /api/setup/claim` `{code, name}` | turns the setup code into the first admin (this browser) |
| `GET /api/v1/app`, `/api/v1/phone-app` | the apps' versions; `app.apk`, `phone-app.apk` the files |
| `GET /` on 8790 | the download page |
| `/dav/…` | read-only WebDAV, with its own password (`webdav.pass`, user `media-core`) |

Unauthorised: `401 {"error": "pair"}` (no key, or a removed one: pair again); `403 {"error": "setup"}` (nobody has set the server up yet); `403 {"error": "admin"}` (a player key on an admin route).

## Player (any paired device)

On port 8790 (and 8780): `POST /api/weblog` (the web player's log), `GET /api/v1/home`, `movies`, `shows`, `search?q=`, `item/{id}`, `me`; `POST /api/v1/progress {id, pos, dur, ended}`, `watched`; `GET /api/v1/stream/{id}` (range requests), `sub/{id}/{n}`, `thumbs/{id}`, `img/{size}/{path}`, `speedtest?bytes=`. The website's player also uses `hls/…` and `web/…`.

## Admin

On port 8780.

- Downloads: `POST /api/probe {url}` says what a link is and returns `{probe, plan}`; `POST /api/jobs {probe, plan}` starts it; `POST /api/quick {url, kind?, title?}` does both; `POST /api/plan` re-plans a probe. `GET /api/jobs`, `POST /api/jobs/{id}/{action}` (pause, resume, cancel, retry and the like), `POST /api/clear` (clears finished ones), `GET /api/events` (live events), `GET /api/system` (disk, health, what's playing), `POST /api/settings` (download settings), `GET /api/cover`.
- Library: `GET /api/library`, `POST /api/library/delete`; Manager's `GET /api/manager/titles`, `POST /api/manager/light`, `POST /api/manager/light/delete`.
- Devices: `GET /api/devices` (`{devices, waiting}`), `GET /api/pair/{code}`, `POST /api/pair/approve {code, name?, role?}`, `POST /api/pair/deny {code}`, `POST /api/devices/{id}/revoke`.
- Server: `GET /api/setup/config`, `POST /api/setup/config {library?, make_folders?, name?, tmdb_key?}` (restarts the engine; refused while something plays or downloads), `GET /api/setup/busy`, `GET /api/tools`.

The routes are registered with Go's `http.ServeMux` patterns, so `grep -n HandleFunc engine/*.go` lists every one; `engine/auth_test.go` checks each needs a key.
