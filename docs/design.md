# Design

UI principles across Media Core's three clients. Exact colours, typography, spacing and motion tokens live in the code — read those files rather than duplicating numbers here:

- **TV (`android/app`):** [Design.kt](../android/app/src/main/java/app/mediacore/Design.kt)
- **Phone (`android/phone`):** [Theme.kt](../android/phone/src/main/java/app/mediacore/phone/Theme.kt)
- **Web (`engine/web`):** [app.css](../engine/web/app.css)

## Shared principles

- **Dark, flat, quiet:** One near-black canvas, one corner radius per platform, no neon glows, no clutter. Nothing on screen while a film plays.
- **Minimal metadata:** Clean audio and subtitle names (no raw filenames or codec dumps); state each fact once. Technical details live in Details.
- **Icons:** Filled Microsoft Fluent System Icons (`AppIcons.kt` / web SVGs), plus stroked UI vectors on TV (`Design.kt`).
- **Original or Light:** Clients measure connection speed against `/api/v1/speedtest` and switch to a Light copy when the link is too slow, or offer a one-tap/one-click switch after repeated stalls.
- **No chapters** in the TV or phone players.

## TV (`android/app`)

- **Fixed 1920 × 1080 board grid:** Screens use `.px` and `.fs` (`Design.kt`) scaled from `wide / 1920` (with system font scale divided out) so layout stays identical on 1080p and 4K TVs.
- **Legibility at 10 feet:** Source Sans 3 at weight `>= 500` scaled up slightly for 4K upscaling; backdrops sit behind dark fades, vignette and pre-baked off-UI-thread shadows (`Art.kt`) — no `Popup` windows or live blurs.
- **Focus & accent:** Selectable accent colour in Settings (default Blue) used sparingly for focused Play/Resume, progress bars and Settings headings. Cards and episode stills scale slightly (`1.05`) with a crisp light border on focus; other buttons do not scale.
- **Four screens (`Root.kt`):** `Home`, `Detail`, `Pending`, `Settings` — no top tabs, grids or Search. `Continue watching` puts title and time left under wide art; `Recently added` posters carry no text below.

## Phone (`android/phone`)

- **Thumb-first, portrait browse / landscape player:** Minimum `48 dp` touch targets, no hover/focus state (controls dim when pressed, no ripple), and a fixed bottom bar (`Home`, `Films`, `Shows`, `Search`).
- **Blue for Play only:** Because there is no D-pad focus, Accent blue marks Play and nothing else.
- **Offline downloads:** One Download button on title pages for what Play would start; finished downloads appear under `On This Phone` on Home.
- **Gestures:** Tap toggles controls, double-tap sides skips 10 s, horizontal drag scrubs with the thumbnail strip, and track/episode menus open as a right-hand list over the video.

## Web (`engine/web`)

- **Own CSS tokens (`app.css`):** Fixed `rem` scale so wider browser windows show more content rather than bigger text; fixed top bar (`Home`, `Films`, `Shows`, `Search`, `Manager`) that turns solid on scroll.
- **Hover as focus:** Arrow keys also move focus; cards scale slightly with a light border on hover; unplayable titles dim with a clear compatibility badge (`This browser`, `TV`, `Phone`).
- **Manager (`manager.js`):** Paste a link to inspect quality/compatibility before downloading, plus `Downloads`, `Library` and `System`.
