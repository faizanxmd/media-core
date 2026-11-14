# Waiting for the next build

Changes committed but not yet built or tested. Cleared after each build the user has tested.

## The TV redesign (branch `redesign`, TV only)

Home is the whole app now: the hero over Continue watching and Recently added, with Settings at the top right. Tabs, grids and search are gone. Title pages, a show's seasons and episodes, Picture details, Settings and the offline screen follow the boards in the redesign spec. The player is unchanged; only going into and out of it fades through black. Built from `redesign`, not main.

Since build 2026.11.14-2048: everything is sized for a TV across the room (body text 29 px at 1080p, nothing under 23, larger buttons and cards, the old app's 80 px edges), and focus is only a thin white border. Nothing fills white, nothing else dims, and Play sits in the same dark surface as the other buttons.

### To test
1. Home: Up and Down move between the buttons and the two rows, and the hero follows the focused card after a short pause.
2. A film and a show open their title pages; Down on a show reaches Episodes, then More.
3. Play and Resume fade to black, the player starts as before, and Back fades back to the same place.
4. Settings changes values with Left and Right and shows the connection to mint. The player doesn't use these values yet.
5. Unplug mint's network briefly: "mint is offline" appears and goes away by itself.
6. From the sofa, every screen reads comfortably, nothing is cut off at the edges or bottom, and the focused thing is clear from its border alone.

## A new Audio icon (TV and phone)

The headphones are replaced by a sound wave in a disc, the same icon the web player now has. Only the icon's path changed in each app's `AppIcons.kt`. Not compiled yet.

### To test
1. In the TV player and the phone player, Audio shows the new icon, sharp and the same size as the icons beside it.

## Original or Light, by connection (TV and phone)

Both apps measure the connection to mint in the background and play a title's Light copy when the connection can't carry the Original. When playback gets stuck, a "Slow connection · Switch to Light" card offers the switch. The engine side (the speed test and Light copies) is already live on mint. Not compiled yet: the next build is its first check.

### To test
1. A title without a Light copy plays as before, at home and away.
2. Add a Light copy to a film in Manager. At home it still plays the Original.
3. On 4G (or a slow link), the same film starts on the Light copy.
4. On a connection that slows down mid-film, the card appears after about 15 s of buffering. OK switches at the same spot; Back keeps the Original and it doesn't ask again for that film.
5. Skipping around, and the first load of a film, never bring the card up.

Last published builds: TV 2026.10.30-2319, phone 2026.10.31-0019.

## Shared code in android/core

The TV and phone apps now use one copy of Api, Models, Offline and Thumbs. Nothing should look or behave differently. This hasn't been compiled yet, so the next build is its first check. If it fails, nothing gets published.

### To test
1. Both apps open, find mint at home and over Tailscale, and play a film.
2. Scrub pictures appear on both.
3. On the phone, an existing offline copy still plays, and a new download waits for Wi-Fi.
4. On the TV, a title's Atmos, Dolby Vision and other format marks still show.

## Builds on GitHub Actions, and the phone updates itself

The next build is the first real one on GitHub Actions.

**Phone app (needs a build)**
- A "New version available" bar sits above the bottom tabs when mint has a newer phone build. Tapping it downloads and installs the update, as on the TV.
- The installed phone app doesn't have this yet, so install the next build by hand once: open `http://mint.tail25d09d.ts.net:8790/api/v1/phone-app.apk` in the phone's browser, or use Taildrop.

### To test
1. After the build, the TV shows "Update available" and installs it. It must install over the old app, which proves GitHub signs with the same key.
2. Install the phone build by hand. It must install over the old app and keep its offline downloads.
3. Run a later build. The bar appears on the phone, and tapping it asks once for permission to install updates, then installs.

## Progress sync across TV and phone ([#1](https://github.com/faizanxmd/media-core/issues/1))

**Engine (live on mint since 2026-10-31 15:24, no app build needed)**
- A title is now not started, part-way or finished, never two at once. Stopping a rewatch partway puts it back on Continue Watching with its time left and Start over.
- Existing entries in that bad state repair themselves when the engine starts.
- A show's Play button now picks the same episode as Continue Watching.
- A position saved offline carries its time, so an older one can't overwrite newer progress from another device.
- A short open no longer counts as activity, and finishing no longer counts two plays.

**TV and phone apps (need a build)**
- Leaving the player saves the position before Home reloads, and Home and title pages wait up to 1.5 s for it. Before, they could show the old position.
- While open, the app refreshes every 60 s, so progress from another device shows up without reopening it.
- An offline copy keeps resuming from its own last position after that position syncs.

### Decided
- Mark watched and Mark unwatched reset the position, as Plex does.
- A finished title has no separate replay button: Play starts it from the beginning.

### Known limits
- True Detective's old positions were wiped and can't be recovered.
- An offline copy doesn't learn about progress made on another device.
- Two devices playing the same title at once: the last report wins.

### To test
1. Stop a film partway on the TV. It's on Continue Watching with its time left and Start over.
2. Within a minute, the open phone app shows the same position. Resume there.
3. Finish it. It leaves Continue Watching on both, and Play starts from the beginning.
4. Replay a finished film for over 20 s and stop. It's part-way again.
5. Watch a downloaded episode offline on the phone, go online, and confirm the position arrives.
6. Leave the TV's film and show grids open past a minute. Focus must not jump when they refresh.
