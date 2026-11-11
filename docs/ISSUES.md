# Known issues

Everything known to be wrong, gathered before fixing so the player, sound, browser and network work can be planned as a whole rather than patched one report at a time. Each entry says where it happens (TV app, phone app, browser, engine), what the user sees, and the cause when it's known. Fixed entries move to the bottom with their commit.

## Start here

Media Core is the user's own Plex replacement: the engine (`engine/`, Go, on the box "mint") downloads into the library and serves it; the TV app (`android/app`) and phone app (`android/phone`) play files as they are with Media3; the web player (`engine/web`, served on `:8780`) plays in browsers. `README.md` has the layout, `CLAUDE.md` the rules. The ones that matter most here:

- **Never build the apps unasked.** Commit, list the change in `docs/PENDING.md`, and the user asks for a build when a batch is ready. Engine and web changes go live with `deploy/install.sh` (check nothing is playing first: it restarts the engine).
- **Talk before changing the player.** The user wants a proper redesign, agreed first, not piecemeal changes.
- **Safari on the user's M1 MacBook Air is the browser to design for.** Chrome and others are a fallback.
- **The engine holds the rules** (which track, which version, what a browser can play); the apps and web player follow them. Watch progress lives only in `engine/watch.go`.
- **The user tests on the real TV and phone**, not emulators. The web player reports what the browser did to mint's journal (`journalctl --user -u media-core-engine | grep "^.*web "`).

Devices: TCL Google TV 14 (MediaTek, weak 32-bit CPU, 3 GB RAM, TV speakers only), Galaxy A55 (no Dolby Vision), MacBook Air M1 (SDR screen). The library is mostly WEB-DLs (H.264/HEVC, DD+ or Atmos, SRT, Dolby Vision 5 or 8.1); remuxes are rare.

## Player (TV, phone, browser)

1. **Skipping forward freezes on the old picture first** (reported 2026-11-10; device not stated, all three skip the same way). In the web player part of it was a repeat, fixed 2026-11-11 (see Fixed); what follows is about the TV and phone. A tap of Right jumps at once with an exact seek (`seekExact` in both `PlayerActivity.kt`, `SeekParameters.EXACT`; `tapSkip` in `engine/web/player.js`), then jumps again 350 ms after the last tap when taps add up (`commit`). An exact seek decodes forward from the previous keyframe, which can be several seconds away in a WEB-DL, so the old frame stays up meanwhile; on the TV's weak CPU, with 4K HEVC, that's long. The second seek can repeat the stall. Worth weighing: keyframe seeks (`CLOSEST_SYNC`, already used for scrubbing) for skips, one seek per burst of taps, and keeping the skip target inside the buffer.
2. **The player as a whole feels like an old Android player** (user's verdict, 2026-10-30: "it mimics modernity, it is not modern"). The brief, : nothing on screen while playing; tap Left/Right skips 10 s silently and taps add up; pause shows the controls; holding scrubs through thumbnails and that moment must look really good. No mockups: build the real thing, test on the TV, then polish.
3. **Polish list from build 1721** (2026-10-30, not yet done): remove chapters from the UI entirely (button, row, cuts in the bar, chapter names in pause and scrub text); move the Audio and other controls from the left to the right.
4. **More to come from the user.** They said there are many player issues; ask them to describe each with the device and what they did.

## Sound and tracks

1. **Apps don't prefer English** (TV, phone). With no saved choice for a show, the player takes the file's first or default track, so a release with a dub first (True Detective S01: Hindi, then English) starts in the dub. The choice is saved per show only after switching once. Needs a house language, decided in the engine.
2. **Chrome can't switch tracks on MP4s and can't play Dolby audio** (browser). Chrome has no `audioTracks`, so only the first track is heard, and it plays only AAC, MP3, Opus and FLAC. The menu now marks unplayable tracks ("Not in this browser"). Safari switches and plays DD+.
3. **Make MP4 downgrades the TV's sound** (engine, `mp4Args` in `engine/mp4.go`). DTS, TrueHD and PCM become AAC, so a remux made MP4 loses DTS, lossless sound and Atmos on the TV for good. It should keep every original track and only add AAC copies where some browser needs them.
4. **Same language, several formats** (TV). An English TrueHD Atmos track next to an English DD 5.1 one: the TV can't decode TrueHD and falls back to software on its weak CPU, Atmos lost; the DD track would be the better pick there.
5. **Track names differ across apps** (all). The engine, web player, TV and phone each build their own label, so the same file reads differently, and two tracks in one language can look identical. The engine should name each track once.
6. **Tracks with no language tag** ("und") can't be ranked; every player just takes the first.
7. **Library MP4s made before the English-first fix** (`13c0ae1`) keep the dub first. Only Georgie S01E05 was rebuilt.
8. **Light copies are separate releases** (all). Their tracks and languages can differ from the Original's, so switching to Light can change the language. Check a Light copy's languages when it's added.

## Picture, formats and browser playback

1. **MKVs don't play in any browser** (Safari can't open MKV at all). True Detective S01 is MKV. Make MP4 rewraps them, but it's only offered for 1080p SDR files until it keeps the original sound tracks (Sound 3); after that, 4K and HDR MKVs can be rewrapped for Safari too.
2. **Dolby Vision profile 5 breaks on the phone.** The A55 has no Dolby Vision and profile 5 has no HDR10 underneath: wrong colours or no picture. Nothing warns at download time; a Light copy is the way out.
3. **Picture subtitles after Make MP4** (TV, phone). PGS moves to a `.sup` file only the web player reads, so the TV and phone lose it; VobSub is dropped entirely; ASS loses its styling (becomes `.srt`).
4. **Formats with no browser path**: AV1, VP9 and H.264 10-bit never get an MP4 offer that helps, and H.264 10-bit looks like ordinary H.264 to the engine although the TV and phone can't decode it.
5. **For a Few Dollars More** (MP4 with FLAC first, then AC-3, AAC and a data track): check it plays with sound in Safari now that the network is fixed.
6. **The old HLS copy** of True Detective S01E01 (`~/.local/share/media-core/web/ff46061779b1`) is the last of its kind. Safari now uses its own HLS player for it; retire it once S01 is MP4.

## Network

1. **Direct Tailscale can silently break.** The Deco's automatic mapping for mint moved from 41641 to 41642 on 2026-11-09, so the ISP router's forward led nowhere and the Mac went through the DERP relay in Mumbai: anything over about 10 Mb/s stalled, and every skip stalled. Restored with a UPnP mapping that lasts until the Deco reboots (details in `~/SERVER-NOTES.md`). Open: the user adding a manual Port Forwarding rule in the Deco app (UDP 41641 → 192.168.68.101:41641), and something that notices when a device falls back to the relay.
2. **The web player has no notion of connection speed.** The apps pick Original or Light by measured speed (`core/Speed.kt`, not yet built); the browser always plays the Original when it can decode it, even over a slow link.
3. **Port 8790 has no owner lock.** The site on 8780 turns away other Tailscale accounts (`engine/owner.go`), but the player API on 8790, which the apps use, answers anyone who can reach it, including `/api/v1/match`. Only matters if mint is ever shared.
4. **Waiting on a build**: speed-based Original/Light picking and the slow-connection card in both apps, and the shared `android/core` code (`docs/PENDING.md`). Nothing has been compiled yet.

## After the 2026-11-10 work (browser streams, web player, network)

What that work left open or unproven. The entries above it settles are listed under Fixed.

1. **Safari and Chrome haven't played the finished stream.** They played hand-made pieces of the same kind (4K Dolby Vision MKV, both labels) on 2026-11-10. The real stream and the reworked player were only driven in Firefox on mint. First thing to check on the Mac: A Clockwork Orange and a True Detective episode in both browsers, with jumps, a sound switch and subtitles. The journal has each open and jump (`grep "^web "`), and `STREAM_DEBUG=1` on the engine logs every piece.
2. **One stall seen once.** In Firefox, after a jump to the end and back, the picture buffer stopped growing for 45 s while sound kept loading. Two repeats of the same steps didn't show it. If a browser stalls with sound still loading, that's this.
3. **Port 8790 now refuses Tailscale devices that aren't the owner's.** The home network is unaffected. Check the phone app away from home still connects.
4. **TV and phone still pick and name tracks themselves** (Sound 1, 4, 5 above). The engine now names each track and marks the one to start with (`name`, `detail`, `main` in `/api/v1/web/{id}`), but only the web player uses it. The apps need the same from `/api/v1/item` and a build.
5. **The site's look is unchanged.** Manager got fixes (long details, lasting errors, the tab in the address, phone text size), not a design. The web has no design system of its own: `docs/design-system.md` moves the TV's to a mouse, and its rule that a button is only blue while focused leaves a mouse page with no resting main action. That needs agreeing with the user before building.
6. **BBR and the other TCP settings** on mint (`~/SERVER-NOTES.md`) weren't measured before and after.
7. **Not done from the player list:** switching to the Light copy by itself when a film keeps stalling mid-play (it's chosen at the start only), and subtitle size.
8. **The user's line's upload speed** is still unmeasured; it caps everything away from home. The Deco port forward (Network 1) is still theirs to add.

## Fixed

- A single tap of Right (or Left) in the web player jumped, then 350 ms later jumped back to the same spot, because the film had played on past it, so the same moment played twice. It now remembers the jump it made, as the apps do with `seekedTo` (2026-11-11). The stream itself was checked: pieces from a run started at a jump match the file frame for frame, with no gaps or repeats where two runs meet.
- MKVs, and MP4s with Dolby or DTS sound in Chrome, play in browsers as a stream made while they play; the file isn't rewritten (`f9aaaa0`; Picture 1 and 4, Sound 2 and 3).
- Subtitles inside a file reach browsers, read out once in the background; Manager no longer offers Make MP4, and links don't become MP4s by default (`691892f`; Picture 3 for new files).
- The web player starts with the engine's pick of sound track (English, then the file's default), names tracks as the engine does, loads a stuck stream again by itself, and has a computer's keys; port 8790 is for the owner's devices (`551f349`; Network 3, Sound 1 and 5 for the browser).
- The web player measures the link and plays a Light copy when the film needs more than it carries; System shows each device as direct or relayed and notes relayed playing in the journal (`f7af33f`; Network 2, half of Network 1).

- Foreign releases became MP4s with the dub first and the browser AAC copy made from the dub. English now goes first (`13c0ae1`).
- The MP4 option was missing when a slow link couldn't be read within 20 s, and quick adds never made MP4s (`6b8afa0`).
- A link with its name only in a loosely encoded Content-Disposition header came in as a movie named after its hash (`52b8314`).
- Guest sharing removed for now; the site answers only the owner's devices (`335dfab`).
- Safari opened the player but never started the video: the click was gone by the time the player called play() after its fetches (`c056c2f`).
- Browsers were held to 1080p without HDR. The page now reports what the browser can decode and the engine decides with it, so Safari gets 4K, HDR, Dolby Vision and DD+ MP4s as they are (`52e7b4e`).
- Safari played the HLS copy with sound but no picture through hls.js; it now uses its own HLS player. Copy tracks were marked unplayable; Safari's track list fills in late, so the chosen track is applied as tracks arrive (`4f44302`).
- The Deco's mapping drift that sent remote devices through the Mumbai relay (2026-11-09, router-side; see Network 1).
