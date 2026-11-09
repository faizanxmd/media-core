# Known issues

The running list of what's wrong, gathered before fixing, so the player, audio and network work can be planned as a whole rather than patched one report at a time. Each entry says where it happens (TV, phone, browser, engine), what the user sees, and the cause when it's known. Fixed entries move to the bottom with the commit that fixed them.

## Audio and tracks

1. **Apps don't prefer English** (TV, phone). With no saved choice for a show, the player takes the file's first or default track, so a release with a dub first (True Detective S01: Hindi, then English) starts in the dub. The choice is saved per show only after the user switches once.
2. **Browser audio switch does nothing on MP4s** (browser, Chrome). A directly played MP4 has no `audioTracks` in Chrome, so `pickAudio` in `engine/web/player.js` changes the menu but not the sound. The menu still lists every track as if it could switch.
3. **Browser can't play most original tracks** (browser). Only AAC, MP3, Opus and FLAC play; EAC3, AC3, DTS and TrueHD don't. An MP4 carries an AAC copy of the first track only, so every other language is listed but silent or unplayable.
4. **Track names are inconsistent across apps** (all). The engine, web player, TV and phone each build their own label from language, title and channels, so the same file reads differently on each, and two tracks in one language can look identical.
5. **Library files made before the English-first fix** keep their old order. Only Georgie S01E05 was rebuilt; any other MP4 from a foreign release still starts in the dub.
6. **Light copies are separate releases** (TV, phone, browser). Their audio tracks, languages and order can differ from the Original's, so switching to Light can change the language.

## Browser playback and MP4s

1. **4K and HDR need a Light copy to play in a browser**, and nothing says so at download time unless the link was read in time.
2. **MKVs already in the library** (True Detective S01) don't play in browsers until each one is made into an MP4 in Manager.

## Player (TV and phone)

To be filled in from the user's reports.

## Network

To be filled in from the user's reports.

## Fixed

- Foreign releases became MP4s with the dub first and the browser AAC copy made from the dub. English now goes first (`13c0ae1`).
- The MP4 option was missing when a slow link couldn't be read within 20 s, and quick adds never made MP4s (`6b8afa0`).
- A link with its name only in a loosely encoded Content-Disposition header came in as a movie named after its hash (`52b8314`).
