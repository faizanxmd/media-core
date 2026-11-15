# The Netflix TV player, moment by moment, and ours beside it

Written 2026-11-15, to start the TV player redesign. The user rates Netflix's TV player above all the others. It's the model here, apart from the mistakes listed at the end.

Sources:
- Fable's breakdown of the Netflix player on Android TV.
- Reports of Netflix's new TV player test, which began 29 July 2026 ([What's on Netflix](https://www.whats-on-netflix.com/news/first-look-netflix-new-tv-video-player-redesign/)).
- Reports of Netflix's custom player on Apple TV, which lost features users relied on ([9to5Mac](https://9to5mac.com/2026/04/15/netflix-ruined-its-apple-tv-app-by-switching-to-a-custom-video-player/)).

Timings are estimates. Check them against the real Netflix app on the TCL before fixing any number.

"Ours" means the current TV player: `PlayerActivity.kt`, `PlayerUi.kt` and `Ui.kt`.

## Moment by moment

**1. Press play until the first frame appears.**
Netflix shows a black screen, or the dimmed backdrop, with a thin spinning ring and a percentage counting up inside it. The picture cuts in. The title and the progress bar show for 3 to 5 s, then fade. A resume starts where you left off, with no question asked.
Ours shows a quiet spinner that appears only if the wait is long. There's no title at the start.

**2. Playing, nothing on screen.**
On Netflix:

| Button | What it does |
|---|---|
| OK or Play/Pause | pauses and shows the controls |
| Up or Down | shows the controls without pausing |
| Left or Right | skips 10 s |
| Back | hides the controls first, then leaves the player |

The controls hide again after 3 to 5 s.
Ours works much the same.

**3. Tapping Left or Right.**
On Netflix, each tap moves the playhead 10 s at once, and taps add up. The progress bar appears with a thumbnail of where you'll land and that time. The video itself seeks only once, after you stop pressing.
Ours seeks the video on the first tap, then again 350 ms after the last tap. All you see is a small arrow mark. Both seeks are exact, so the TV decodes forward from an earlier keyframe, and **this is the freeze you see on a skip**.

**4. Holding Left or Right.**
On Netflix, the playhead runs along the bar with thumbnails. The longer you hold, the faster it goes, as a smooth ramp with no "4x" badge. The time sits with the thumbnail. Letting go lands exactly on the frame shown. OK commits at once, and Back cancels.
Ours dims the film and shows a large centred thumbnail with its neighbours and the time. It's close to Netflix's older strip.

**5. Pausing.**
On Netflix, a dark gradient rises from the bottom. The title and "S1:E3 Name" sit on the left, with a large progress bar and the time left. Focus starts on the bar. In the 2026 test there are no on-screen Play or Back buttons; everything sits at the bottom left above the bar. If you stay paused for a while, a quiet "You're watching" screen with the synopsis takes over.
Ours shows the title, the bar and a button row. Chapters still appear in the button, in cuts on the bar and in the text, although you asked on 2026-10-30 for them to go. The buttons sit on the left, where you wanted them on the right.

**6. The button row.**
On Netflix the buttons are Next Episode, Episodes and Audio & Subtitles, plus sometimes speed. Each is an icon with a short word. The focused one turns brighter and slightly larger, with no boxes.
Ours uses icons only, with the focused icon turning blue.

**7. Audio and subtitles.**
In Netflix's 2026 test this is one panel at the bottom, holding audio, subtitles and subtitle appearance together. **Playback keeps going behind it.** OK applies a choice at once.
Ours opens separate lists above their buttons. The apps choose and name tracks themselves instead of following the engine, so English isn't preferred and two tracks can look identical.

**8. Episodes.**
On Netflix, a panel lists the seasons and the episodes, each with a still, its name, length, story and progress. It opens on the current episode.
Ours shows a row of stills along the bottom.

**9. Skip intro and skip recap.**
On Netflix a worded button appears when the intro starts and takes focus, so OK skips. The test moves it to the bottom left. It disappears too soon.
Ours has a "Skip intro" button that lights up while it's what OK would do.

**10. End of an episode.**
On Netflix, a Next Episode button appears in the credits. It fills up as a 5 to 10 s countdown, then the next episode starts. "Watch credits" stops the countdown.
Ours shows a card with a still, the title and a countdown ring.

**11. End of a film.**
On Netflix the credits shrink into a corner, and the rest of the screen offers other titles or plays a trailer.
Ours has nothing to offer here yet.

**12. Back, and resuming.**
On Netflix, Back leaves at once with no question, and progress is saved. Resume goes straight back in.
Ours works the same.

**13. Buffering and errors.**
On Netflix the same ring appears, but only after a real stall. Errors are a plain screen with a code.
Ours has a delayed spinner and an error card.

**14. Subtitles.**
On Netflix they're white sans text with a soft shadow and no box, at most two lines, and they lift above the controls when those show.
Ours uses ExoPlayer's built-in subtitle view. There's no size setting, and picture-based subtitles (PGS) are lost after a file is converted to MP4.

## What makes Netflix feel modern, most important first

1. A press shows on screen the same frame you press, even before the video is ready.
2. Scrubbing moves through pictures, never the video, so it can't stutter.
3. The screen is empty by default. Controls come only when asked for and leave by themselves.
4. OK does the obvious thing for the moment: skip the intro, play the next episode, or pause.
5. There's one seek per gesture, and it lands on the frame the thumbnail showed.
6. It starts fast, straight into the resume point.
7. There are few buttons, each with a word, and one clear focus.
8. It only fades and slides, over about 150 to 250 ms, with no bounce.
9. At the end, the next thing is one press away.
10. The type is calm: white on a gradient, no boxes.

## How the feel is achieved

- **Thumbnail packs.** Netflix uses its own trick-play image archives, with one picture every 10 s, fetched when playback starts. We already make thumbnails (the TV's `ThumbStore`). Check their spacing, size and how early they load.
- **No seeking while scrubbing.** Only the pictures move. Taps add up, then there's one seek after about 500 ms of quiet, or on OK.
- **Keyframe seeks for skips.** Seek to the nearest keyframe instead of the exact second. The engine can tell the app where the keyframes are, so the thumbnail matches the frame it lands on.
- **Speed that ramps with holding.** For example, 10 s steps at first, bigger steps after 1 s, bigger again after 3 s.
- **A warm start.** Fetch the stream details, the resume point and the thumbnails while the title page is open.
- **The next episode ready in the credits.** Open its address early, but don't start a second decoder: the TV's weak chip has one good one.
- **A delayed spinner.** It shows only after about 500 ms of stalling.
- **Cheap animation.** Animate only fading and position, use a gradient rather than blur, and keep the bar's redraws apart from the rest of the screen.

## Netflix's mistakes, which we won't copy

- On Apple TV in 2026, a single left or right press stopped skipping 10 s. It paused and opened the frame picker instead, so a short rewind took two or three presses. Users hated it.
- Skip Intro disappears after about 3 s.
- The next episode or a trailer starts too quickly, and a film's credits get shrunk into a corner.
- Skips are fixed at 10 s, with no finer step.
- "Are you still watching?" has no purpose on a personal server.
- Error codes come with no plain reason.
- The layout differs from one device to another.
