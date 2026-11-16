# Media Core design system (TV)

Rules for the TV app (`android/app`). Every screen follows them; a value not listed here isn't used.
Sizes are in board pixels on a 1920 x 1080 grid via `.px` and `.fs` (`Design.kt`), scaled to `wide / 1920` (`0.5 dp` per board px on a `960 dp` 1080p TV). `.fs` divides out system font scale so sizes stay fixed.

## The idea

One dark base (`#090B0E`), one shared backdrop layer (`ArtLayer`), one corner (`12`), soft shadows over art, and a selectable accent used sparingly for focused Play/Resume, progress bars and Settings headings.

## Sharpness

TV UI draws on the 1920 x 1080 grid and scales up on 4K panels, while video plays at full resolution:

- UI icons (`Design.kt`) are stroked vectors on 24 x 24 (filled `Play` on 13 x 15); player icons (`AppIcons.kt`) are filled Microsoft Fluent System Icons on 24 x 24 plus two stroked icons (`NextEp`, `Captions`). `Wordmark` draws from SVG outlines at any height.
- Cards and episode stills scale `1.05` on focus; `Next episode` scales `1.04` when armed or focused. Other buttons never scale.
- Text uses Source Sans 3 at weight `500` or heavier, drawn 14% larger than nominal so thin strokes survive upscaling.
- Text sits over backdrops backed by `Base` fades, vignette and top shade (`Art.kt`), `WordShadow` on facts/overview, a drop shadow on fallback titles, and `ChromeShadow` on player chrome.
- `Continue watching` puts title and time left under the image; `Recently added` posters carry no text below (text sits inside only when poster art is missing).
- Home and title pages fetch `original` backdrops; poster fallbacks and Settings use `w780`, episode stills `w1280`, wide cards `w500`, posters `w342`.

## Tokens

### Colour

| Token | Value | Use |
|---|---|---|
| BaseDeep | #040507 | fade ends, vignette corners, shadows |
| Base | #090B0E | every screen |
| Raised | #14161B | empty cards, offline input at rest, error card, scrub box |
| Glass | #D124272E | controls over art (`#24272E` at 82%), focused offline input, player note |
| Ink | #F2F3F7 | titles, focused fills, focus borders |
| InkSoft | #D5D7DD | descriptions, secondary values |
| Muted | #A5A9B2 | secondary text |
| Label | #8B909A | small-caps labels |
| Faint | #6B707A | middle dots, dates, technical details |
| RowTitle | #C9CCD3 | row titles when focus is in rows |
| OnInk | #0B0D11 | text/icons on a focused `Ink` control |
| OnInkSoft | #4A4F5A | secondary text/arrows on a focused `Ink` control |

Player combo pills at rest use `#EB484A50`. `Wordmark` uses `#F4F5F8` with a brand-blue radial core (`#7FA2FF` to `#3A5CF8` to `#1D39F3`). `Ui.kt` keeps `Base` `#0E1014` and `Ink` `#F4F5F8` on `AppTheme`.

Accent choices (`AccentColor`, set in Settings; `Blue` is default):

| Choice | Accent (`main`) | Deep (`deep`) | AccentText (`text`) |
|---|---|---|---|
| Blue | #1D39F3 | #1F2F8C | #7C95FF |
| White | #F2F3F7 | #F2F3F7 | #F2F3F7 |
| Ember | #D9622B | #8C3415 | #F2945F |
| Red | #E50914 | #A8070F | #FF6B70 |

`Accent` fills progress bars and the spinner; `Deep` fills focused Play/Resume (`OnDeep` is `OnInk` for `White`, `Ink` else); `AccentText` styles Settings headings and `"Now playing"`. Judge `Blue` on a TV panel, which shows it lighter than a laptop does.

### Type (Source Sans 3)

Variable font (`source_sans.ttf`, weights 500, 600, 650, 700, 800) via `ts()`. `ts()` scales `size` by `1.14`, keeps `line`, clamps weight `>= 500` and tracking `>= -0.01` em, and sets `"tnum"` when `tabular = true`. `Ui.kt` keeps `Inter` (`450`–`800`) on `AppTheme`'s fallback, while screens and `PlayerUi.kt` call `ts()`; subtitles use Source Sans `600`.

| Size / line | Weight | Use |
|---|---|---|
| 84/88 · 68/72 | 800 | fallback title on Detail/Pending (84/88) and Home (68/72) |
| 80/84 · 76/80 | 650 (`tnum`) | Settings readings (80/84), Details figures (76/80) |
| 60/68 · 44/52 · 40/48 | 700 · 800 · 700 | Settings/Offline/Leave (60/68), player title (44/52), error title (40/48) |
| 36/42 · 36/40 · 34/42 · 34/40 | 500–700 | track rows (36/42), skip s (36/40, `tnum`), Details title (34/42), row/Episodes titles, clocks (`tnum`), start title, units (34/40) |
| 30/40 · 30/36 | 500–700 | player sub (30/40), track headers (30/36) |
| 29/44 · 29/40 · 29/36 | 500–700 | overview (29/44), offline/empty (29/40), buttons/credits/seasons/episodes/Settings/download (29/36) |
| 27/36 · 27/34 · 26/34 · 26/32 | 500–700 | error body (27/36), offers (27/34), wide titles/Details rows (26/34), facts/detail/empty poster/player ep (26/32) |
| 25/32 · 25/30 · 24/30 | 500–700 | Settings/Update/start sub (25/32), ep length/Settings label/Back/note (25/30), player icon labels/pills/track detail/Next-up (24/30) |
| 23/31 · 23/30 · 23/28 | 500–700 | ep overview (23/31), wide meta (23/30, `tnum`), icon label/season count/badges/dates/TMDB (23/28) |
| 22/26 · 22/28 · 21/28 | 500–600 | `CapsLabel` `+0.08` em (22/26), player ep detail (22/28), error detail (21/28) |

### Shape and shadows

`CORNER = 12` for cards, buttons, inputs, menu rows, player cards and panels. Progress tracks use pill ends (`3` px radius on `6` px card bars; `4` px on `8` px player bar). Combo pills use `6` px radius (`48` tall). Top icons (`80`), Play/Pause (`92`) and scrub knob (`13` radius) are circles. No cast photos on TV.

Soft `BaseDeep` (`#040507`) shadows bake off the UI thread (`Art.kt`): `Card` `(dy 18, blur 40, .72) + (2, 6, .55)`, `Small` `(14, 30, .60)`, `Button` `(12, 28, .50)`, logos `(6, 18, .70)`, fallback text `(6, 24, .60)`, `WordShadow` `(2, 14, .75)`, player `ChromeShadow` black `(1, 3, .50)`.

### Space

`EDGE = 80` left/right, `TOP = 60` top/bottom on 1920 x 1080 (`Design.kt`).

| Element | Size / position |
|---|---|
| Buttons | `76` tall (`SettingsGlass`/`Update` `60`), `16` gap; `IconButton` label `14` below |
| Home header & hero | `Wordmark` `44` at `(80, 68)`; logo `600 x 154` at `y = 144`, facts `316`, story `364` (`w = 900`), buttons `482` |
| Home rows & cards | row 0 `y = 616` (focused `500`), `20` to cards, `56` gap; wide `400 x 225`, poster `200 x 300`, `20` gap |
| Title hero & episodes | logo `560 x 220` (show) / `500 x 240` (film), facts `500`, story `552` (`w = 940`), credits `680` (`56` gap), buttons `808`; `"Episodes"` `1024` (to `110`), seasons `w = 340` (`76`/`88` pitch), episodes `x = 480` (`320 x 180`, `180`/`204` pitch) |
| Settings | title `(80, 110)`; Look `(56, 210)` `w = 940` (`68` rows); Connection `(1180, 210)` `w = 660` |
| Player | `SIDE = 112`; `80` top icons (`24` gap, `y = 72`); `92` Play & `8` bar at `y = 896`; `48` pills (`14` gap) at `966`; scrub `352 x 198`; Tracks `520 / 520 / 400` (`72` gap, `72` rows) |

### Focus

| Control | At rest | Focused |
|---|---|---|
| `PlayButton` | `Glass`, `Ink`, `InkSoft` detail | `Deep`, `OnDeep` (`OnInk` for White) |
| `GlassButton`, `IconButton` | `Glass`, `Ink` | `Ink`, `OnInk` (`IconButton` shows label) |
| Setting, season, track rows | clear (`Ink` / `Muted` / `InkSoft`) | `Ink`, `OnInk`, `OnInkSoft` arrows/detail |
| Cards, episode stills | full brightness, `Corner` (`12`), shadow | `1.05` scale (`150 ms`), `3` px `Ink` outside border |
| Player top icons, Play/Pause | clear (`Ink` glyph) | `Ink` circle, `OnInk` glyph (top icons show label) |
| Player pills & offers | `#EB484A50` (pills) / `Glass` (offers) | `Ink`, `OnInk` (`NextUp` fills `10 s`, scales `1.04`) |
| Offline address box | `Raised`, no border | `Glass`, `3` px `Ink` border |

Progress bars use rounded ends and `Accent` fill: `6` px in cards (`White` 28% over `90` px gradient), `5` px in `DownloadBlock` (`Ink` 10%), `8` px in player (`Ink` 28%, `13` px-radius `Ink` knob).

### Motion

`Move` uses `Ease = CubicBezierEasing(0.2, 0, 0, 1)`: `Focus = 150`, `Screen = 250` (slides, screen art, offline, subtitle lift), `Through = 300` (35% out, 65% in), `Settle = 300`, `Art = 400`, `Black = 200`, `Video = 300`. Reduced motion (`ANIMATOR_DURATION_SCALE == 0`) snaps slides and fades in `100 ms`. No `Popup` windows or live blurs: art, shadows and logos bake/cache off the UI thread (`Art`, `Shadows`, `Logos`), and fades use `fadeBy` (`ModulateAlpha`).

## Screens

- **Four screens (`Root.kt`):** `Home`, `Detail`, `Pending`, `Settings` (no top tabs, Films/Shows grids, or Search). Screens swap via `Move.through` over `ArtLayer`.
- **Intro and Offline (`Root.kt`):** Launch holds `Wordmark` (`120` px) on `Base` for `600 ms`–`2.5 s` until Home loads. When unreachable, `Offline` shows over `Base` + grain with a focused `720` px address field, polling every `5 s` and trapping focus/Back.
- **Home (`HomeScreen.kt`):** `Wordmark` top-left, `Settings` (and `Update`) top-right, left hero, `Continue watching` (wide) and `Recently added` (posters). In rows, wordmark/buttons hide, upper rows fade, and the focused row's title slides to `y = 500`; long rows keep the focused card at left. `Continue watching` plays at once; `Recently added` opens `Detail` or `Pending`. `Back` returns to the hero before `"Leave Media Core?"`.
- **Title pages (`DetailScreen.kt`):** `original` backdrop (or blurred `w780` poster), `Settings`, logo (or `84/88`), facts, 2-line overview, credits (`CREATED BY`/`DIRECTOR`/`DIRECTORS`, `MUSIC`, `STARRING`), and `Play`/`Resume`, `Start over`, `Picture details`. On shows, `Down` slides to `"Episodes"` (seasons left, episodes right) and fades to the episode's blurred `w1280` still.
- **Pending (`DetailScreen.kt`):** `Base` + grain, `Settings`, title, focus-free `DownloadBlock`, opening `Detail` when done.
- **Picture details (`DetailScreen.kt`):** Over `ArtStyle.Blurred`: title, 4 figures (`Resolution`, `Dynamic range`, `Bitrate`, `Aspect ratio`), `PICTURE`, `SOUND` (up to 5), `SUBTITLES` (up to 8 with `×N`), `FILE`. Only `Back` closes it.
- **Settings (`SettingsScreen.kt`):** Over blurred Home art: `Look` (`Accent colour`) left, live `Connection to <server>` (`16 MB` speed test in `Mb/s`, `Delay` in `ms`, verdict) right, TMDB notice bottom-left.
- **Labels:** clean audio/subtitle names, no raw file text; each fact once.

## Player

- **Starting and idle (`PlayerUi.kt`, `PlayerActivity.kt`):** Dimmed cover (`BaseDeep` 72%), `76` px spinner, title and sub until first frame. Nothing on screen while playing; Pause or Up/Down/Menu shows controls, auto-hiding `5 s` after play resumes.
- **Skip and scrub:** Left/Right tap skips `10 s` to `CLOSEST_SYNC` while playing with a `150` px turning arrow (`±420` px) and cumulative seconds (`900 ms` timeout). Hold pauses, hides subtitles, scrubs at 1/3/8 min/s, and shows one `352 x 198` preview above the bar; stays up after release (Left/Right step `10 s`, hold scrubs, OK plays, Back cancels).
- **Controls:** Top-left bare `48` px icons (`Start over`, `Next episode`, `Episodes` in `80` px discs; no Back); top-right title (`44/52`) and sub (`30/40`); bottom `92` px Play/Pause disc (shows Pause while playing, Play while paused), clocks (`34/40`), `8` px accent bar with `13` px-radius knob; centred `48` px combo pills (up to 4) + captions button.
- **Panels and chapters:** `TracksPanel` (`Audio`, `Subtitles`, `Subtitle size`: Small/Medium/Large) over `BaseDeep` 86%, saving languages per title/show. `EpisodesPanel` is a bottom row of `400` px 16:9 cards. No chapters on TV.
- **Subtitles:** Source Sans 600 with drop shadow, `9%` bottom padding, lifted (`14%` seek, `22%` full, `40%` sheet) for text and PGS.
- **Offers:** `Skip intro` in `64` px `OfferPill` (lit `Ink` while armed); `Next episode` in credits / last `40 s` beside `"Watch credits"`, filling `Ink` over `10 s`.
- **Original or Light (`core/Speed.kt`):** Plays Light when speed `< 1.3x` bitrate or unmeasured (`4 s` after launch, `3 s` after network change, or on start if `> 10 min` old).
- **Slow connection (`StallWatch`):** `15 s` buffering or three `>= 1 s` stalls in `2 min` (after load/skip) while slower than bitrate shows `"Slow connection · Switch to Light"` in `OfferPill` (no icon) for `30 s`, once per film; OK switches, Back keeps Original.

## On the web

The site on `:8780` (`engine/web`) uses its own CSS tokens (`app.css`: Base `#0E1014`, Raised `#181B21`, Overlay `#262A32`, Ink `#F4F5F8`, Muted `#A4A8B0`, Inter `450`, `6`/`4` px corners) and top-bar pages (`Home`, `Films`, `Shows`, `Search`, `Manager`). Hover acts like focus; arrow keys move focus. What differs:

- **Fixed scale:** `1 rem` is `10 px` above `760 px` (`11 px` on `<= 760 px`): `14.5 px` body, `40 px` controls, `12.5 px` `.caption`. Wider windows show more, not bigger.
- **Blue:** Fixed Accent `#1B36A8`, hover `#2443C4`, AccentWord `#5B78FF`, Trouble `#F4B4AE`.
- **Bar:** Fixed `56 px` (`Home`, `Films`, `Shows` left; `Search` icon and `Manager` with busy dot right). Clear over hero, solid Base with hairline when scrolled (`> 24 px`). Title pages light no tab and add Back (`Esc` goes back).
- **Hero:** Full-width backdrop up to `82%` height (min `440 px`), darkened left and fading to Base. Foot-left words (max `560 px`): `360 x 130` logo, facts + marks, episode line, 3-line `15 px` story (clicks open on title pages), `44 px` buttons (glass tools).
- **Sizes:** Home posters `160 px`, wide `260 px`, `16 px` gap, `18 px` row titles; `Films`/`Shows`/`Search` use a `>= 164 px` grid with name and year below each poster.
- **Episodes:** Vertical list (`176 px` still, `24 px` number, title + 2-line story up to `680 px`, length/left/Watched at end); season tabs when > 1.
- **Under a title:** `132 px` 4:5 cast portraits, then `About` grid. Shows name the up-next episode and show the show's overview.
- **Manager (`manager.js`):** Paste box (checked links show Quality, Video, File, Audio, Length, Size, compatibility, and at most `"Add a Light copy"`), `Downloads`, `Library` (biggest first, title sheet actions), and `System` (disk, server health, settings).
- **Main action and cards:** Play and Download rest in `#1B36A8` (`#2443C4` hover). Cards rest at full brightness; hover adds `scale(1.04)` and `2 px` Ink border.
- **Home & panels:** Hero skips Continue Watching; Recently Added hides if it repeats the library. Overlays close on `hashchange`.
- **Manager & System:** One `1040 px` column; segmented controls (Overlay track, Ink selection). `System` shows disk free (`40 px`), bar + key, five server figures (Processor, Temperature, Memory free, Running, Last backup; Downloader only if stopped), and two columns (`Downloads`, `Apps`, `Playing now`, `Away from home`).
- **What plays where (`engine/play.go`):** Compatibility marks (`This browser`, `TV`, `Phone`) on links and sheets; browsing dims unplayable posters (`#F4B4AE` cross). `Details` opens at once and loads `Plays on` from the server.
- **Search:** `520 px` text box over the poster grid.
- **Player (`player.js`):** Mouse move shows controls; click toggles play (double-click fullscreens). `48 px` logo or heading, episode line + `Ends`, `4 px` bar (`8 px` hover with square marker and single preview; holding Left/Right shows 5-frame `.strip`), then `Play/Pause`, time, and four controls (`volume`, `Episodes`, `options`, `fullscreen`). Options opens a `360 px` right panel (`44 px` rows) for Audio, Subtitles, Speed, Quality, PiP, AirPlay; picks apply at once. Shortcuts: `Left`/`Right`/`J`/`L` skip, `N` next, `C` subs, `P` PiP, `M` mute, `F` fullscreen, `0–9` jump, `<`/`>` speed.
