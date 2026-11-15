# Waiting for the next build

Changes committed but not yet built or tested. Cleared after each build the user has tested.

## The TV redesign (branch `redesign`, TV only)

Home is the whole app now: the hero over Continue watching and Recently added, with Settings at the top right. Tabs, grids and search are gone. Title pages, a show's seasons and episodes, Picture details, Settings and the offline screen follow the boards in the redesign spec. Going into and out of the player fades through black (the player itself has its own section below). Built from `redesign`, not main.

Built so far as 2026.11.15-0933 (items 1-22). Since build 2026.11.14-2048: everything is sized for a TV across the room (body text 29 px at 1080p, nothing under 23, the old app's 80 px edges). A focused picture lifts with a white border; Play and Resume turn deep blue when focused, other buttons white. The stamped-on logo mark is gone; Home instead has the small MEDIA/CORE wordmark top-left, level with Settings, drawn sharp from its outlines with no glow, and it fades out with the buttons when you move down into the rows. Title logos stay in their box, a film's page is one screen (no cast photos, About or file below; the file's numbers are in Picture details) with one line of names under the story: Director (Created by for a show), Music and three of the cast, only "Episodes" peeks under a show's page, and on Home the rows rise into the buttons' place once you move down. The background fade is darker on the words' side and the film grain is gone.

### To test
1. Home: Up and Down move between the buttons and the two rows, and the hero follows the focused card after a short pause.
2. A film and a show open their title pages; Down on a show reaches Episodes. Under the story, a film shows Director, Music and Starring, a show Created by, Music and Starring; a title missing a credit just leaves that column out.
3. Play and Resume fade to black, the player starts as before, and Back fades back to the same place.
4. Settings (rows a little shorter now, to fit the new Look section) changes values with Left and Right and shows the connection to mint. The player doesn't use these values yet.
5. Unplug mint's network briefly: "mint is offline" appears and goes away by itself.
6. Down on a film does nothing; Down on a show reaches its episodes. On Home, moving down into the rows leaves no empty band above them.
7. Home: the wordmark sits top-left, centred on Settings' height, with clear space above the title's logo; it fades away when you move down into the rows and comes back when you return to the buttons.
8. Back from the player (any way: Back, or the film ending) lands on the title page or Home it came from, never the TV's launcher. The app used to crash right there.
9. Back on Home: from the rows it first goes up to Play; from the top it asks "Leave Media Core?" with Stay focused. Back or Stay keeps you in, Leave quits.
10. From Recently added, Up to Continue watching: the row appears once the page has slid, without crossing the title's story.
11. A show's Episodes: seasons are plain words (Season 1, its episode count), white when focused. An episode reads "Episode 2", or for a named one its name under a small EPISODE 2, never "2 Episode 2".
12. Settings, Look: Accent colour steps through Blue, White, Ember and Red. The focused Play, progress bars and Settings' headings change at once and stay changed after a restart; the wordmark's core stays blue. With White, a focused Play has dark words.
13. Home's rows sit tighter: 16 px between cards, the progress bar close under its card, and a card that has slid past the left edge fades away instead of peeking in half-cut. A focused card's border still clears its neighbours.
14. A title's year line and story (Home and title page) read clearly over bright art: near-white with a soft shadow.
15. Opening the app: no round icon. The dark screen shows the Media Core wordmark, centred, for a beat (about half a second, longer only while mint answers), then it fades into Home.
16. Moving between screens (Home, a title, Settings), changing the hero on Home and changing season: the old words fade out first and the new ones fade in, never two sets overlapping. A title's logo fades in rather than popping, and a show's "1 Season" is added once known instead of replacing "8 episodes".
17. A short row (fewer cards than fit across) stays put while you move along it; only a long row slides, and never past its last card.
18. Episodes: a part-watched one has a progress bar along its still's foot; a watched one says "Watched" on the right.
19. Settings' section headings are a light shade of the accent, easy to read.
20. The type is Source Sans 3 everywhere (one width, a little larger and never lighter than medium). Nothing is cut off or overlapping on Home, a title, Episodes, Settings or the quit question.
21. Continue watching: the progress is a thin bar inside the picture's foot, over a soft dark fade, and it lifts with the card; the same on episode stills and downloading posters.
22. Up and Down between rows land on the card straight above or below on screen, not where that row was last.
23. A downloading title's page keeps its progress moving and turns into the film's page when done; "mint is offline" goes away by itself on any screen.
24. Press Play then Back straight away: the screen comes back from black and Play works again.
25. Background art changes without a hitch at the start of its fade; a picture that failed to load is tried again next time.
26. Continue watching's progress bar is clearly visible on bright pictures (6 px, the accent on a dark track over a darker fade), Recently added sits well clear of the line above it, and all type is a size larger than 0933.
27. A more relaxed look: rounder corners (12 px) on every picture and button, a little more room between lines of the story and between the story, credits and buttons, and between cards.
28. A film opens its page instantly (from what Home already has), with no empty moment.
29. Focus moves feel as quick as before or quicker (cards no longer redraw their layout on every frame of the lift), and download bars move smoothly.
30. From the sofa, every screen reads comfortably, nothing is cut off at the edges or bottom, and the focused thing is clear from its border alone.

## The Netflix-style TV player (branch `redesign`, TV only)

Built once as 2026.11.15-2129 with a made-up layout; now rebuilt to match a photo of Netflix's standard TV player. Bare icons top left (Back, Start over, Next episode, Episodes; the focused one names itself underneath), the title and episode top right, a round white Play/Pause at the bottom left with the time watched, the bar and the time left, and under the bar the likely audio and subtitle pairs as grey pills ("Hindi", "Hindi with English Subtitles"), then Other… and a gear for subtitle size. A tap of Left or Right now just jumps the film 10 s at once, as Netflix does: no bar, no pictures, only the turning 10 s mark (taps add up to 20, 30). Each jump goes to the nearest keyframe, so it doesn't freeze. Holding still brings up the bar with pictures. Speed and chapters are gone. Not compiled yet.

Also: Chumbak's (any show's) episode list lost its place when moving Up from the up-next episode, so earlier episodes couldn't be reached. Fixed.

**Redrawn again (2026-11-15), from mockups agreed with the user and a second designer's advice.** No Back icon (the remote's Back leaves), so the top left holds only Start over, plus Next episode and Episodes for a show; they line up with Play's left edge, and the focused one fills a white disc and names itself. The title top right is larger. Play is a 92 px disc that's white only while focused (at rest just its glyph); it shows the state it's in: two bars while paused, the triangle while playing, the bar is a steady 8 px with a white knob and no buffered stretch, the times are larger. Under the bar, centred: small solid grey buttons for the likely audio and subtitle pairs, then one icon for the Audio & subtitles page ("Other…" and the gear are gone). Scrubbing shows a smaller picture with a white edge; its time is the clock left of the bar. The Audio & subtitles page is three plain columns over the darkened film, with larger rows, and "5.1 surround" only when it tells tracks apart.

### To test
1. Starting a title: dimmed art, a turning ring in the accent and the title, then the picture.
2. Tap Right a few times while playing: the film jumps 10 s each time and keeps playing, with only the turning arrow on the right counting 10, 20, 30. No bar, no pictures, no freeze. Left does the same backwards.
3. Hold Right: the film waits and the playhead runs, faster the longer you hold. OK lands, Back returns.
4. Pause: icons top left, title top right, Play/Pause focused at the bottom. Up reaches the icons, Down the pills; Left and Right on Play/Pause skip.
5. A pill switches audio and subtitles in one press and gets the tick. Other… opens Audio, Subtitles and Subtitle size; the gear opens it on size. The film plays on behind it; Back returns to the pill.
6. Up from the remote with nothing on screen shows the controls on Play/Pause; Down shows them on the pills.
7. Start over goes back to the beginning; Next episode and Episodes work; Back (the icon) leaves.
8. Skip intro, and in the credits Watch credits / Next episode (filling white over 10 s), as before.
9. Chumbak: Down into Episodes, then Up from episode 5 walks back through 4, 3, 2, 1 and then up to the buttons.

## Home smoothness and the card line (branch `redesign`, TV only)

Measured on the TV: a frame on Home took 20 ms of GPU time, and 33 ms while the hero changed, so Home ran near 40 frames a second. Two causes. Fading things (the hero's words, rows going up, the cards slid past the edge) were drawn whole into hidden full-size pictures first, some wider than the screen; they now fade part by part. And every frame painted the whole screen three times before the art (the window, Root's fill, then the picture); now once. The Continue watching card for a next episode not started read "S1 E8 ·" with nothing after it; it now shows the episode's length. Not compiled yet.

### To test
- Move around Home quickly: the hero change and going down into the rows should feel smooth.
- The words, rows and buttons still fade as before, with no flash of black at app start or behind the art.
- A next-up episode in Continue watching reads "S1 E8 · 24 m".

## Picture details and a show's Episodes heading (branch `redesign`, TV only)

Picture details no longer has a big "Picture details" heading; just the title's name, then the numbers. Units sit on their number's baseline ("27.3 Mb/s", "2.35 : 1"); they sat low before. Subtitle tracks with no language aren't listed. On a show's page, "Episodes" is just the word, without the arrow beside it, and "Season 1" lines up under it on the same left edge. Not compiled yet.

### To test
- Picture details on a film: no heading, units level with the big numbers, no "Unknown" subtitle line.
- A show's page: "Episodes" at the foot with no arrow; Down slides up and the seasons line up under the heading.
- Home: from Recently added, Up to Continue watching: the first card's left side should come back whole (it was cut while fading in; the fades no longer draw into a clipped hidden picture).

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
