# Media Core design system (phone)

The TV's rules (`design-system.md`), moved to a thumb. Sizes are in dp on the Galaxy A55's 412 x 915 layout.
A value that isn't here doesn't get used, and the code is `android/phone`, whose screens share nothing with the TV's.

## Same as the TV

One flat canvas, one corner, no glow, shadow, pills or circles. Near-black base, Inter, Microsoft's filled Fluent icons,
white square-ended progress flush on the picture. Names under wide pictures; posters carry their own. Each fact once.
The numbers stay in Details. Nothing on screen while a film plays.

## Different, because there's no focus and no remote

- **Blue:** on the TV it marks what's selected; a phone has no selection, so Play is always blue and nothing else is.
- **Bars:** a fixed bottom bar (Home, Films, Shows, Search), the one exception to the TV's "no bars". The current tab is white, the others grey.
- **Pressed** is the phone's focus: a control dims while it's held. No ripple.
- **Size:** nothing a thumb hits is under 48.
- **Orientation:** pages are portrait, the player is landscape.

## Tokens

| Colour | Value | Use |
|---|---|---|
| Base | #0E1014 | every page |
| Raised | #181B21 | panels, empty cards |
| Overlay | #262A32 | controls at rest, the search box, hairlines |
| Ink | #F4F5F8 | titles, progress, a state that's on |
| Muted | #A4A8B0 | everything secondary |
| Accent | #1E3AB8 | Play |

Judge the blue on the phone. It's the TV's small-thing blue, because the TV's large-area blue is close to black on an OLED.

| Type (Inter) | Size / line | Weight | Use |
|---|---|---|---|
| Display | 28 / 32 | Bold | a title with no logo art |
| Title | 20 / 24 | SemiBold | headline figures, the scrub time |
| Heading | 16 / 20 | SemiBold | row titles, the player's title |
| Body | 14 / 20 | 450 | story, menu text |
| Label | 14 / 20 | SemiBold | words you act on, card names |
| Meta | 12 / 16 | Medium | facts |
| Caption | 11 / 14 | SemiBold | tab names, small headings |

Shape: 6 everywhere. Space: 4, 8, 12, 16, 24. Side margin 16, row title to cards 12, between rows 24, cards 12, buttons 8.

## Screens

- **Top of Home and title pages:** the same block. A 4:3 picture under the status bar fading to Base, then logo art climbing 72 into it.
- **Home:** the featured title with Play and Details, then Continue Watching, On This Phone, Recently Added. A title appears once, so Films and Shows rows only hold what's left.
- **Title pages:** logo, kind and genres, episode name, three lines of story, length and formats as plain text, then Play with the icons beside it: Start over, Watched, Download, Details. Films end with the cast, shows with seasons as words and an episode list: number, name and length.
- **Details:** a page of its own: headline figures, then Picture, Languages and File.
- **Films and Shows:** three posters a row, nothing else. **Search:** the box, then the whole library until you type.
- **Downloads:** one button on the title page, for what Play would start. Whole films, so they wait for Wi-Fi. Finished ones show on Home as On This Phone, and the same button removes them.

## Player

- Original or Light, and the slow-connection card, work as on the TV (docs/design-system.md, Player). On the phone the card is the same Ink capsule as Skip intro; a tap switches.
- A tap shows the title, the bar and the controls; another hides them. They leave by themselves 3 s after play resumes.
- A double tap on either side skips 10 s with a small mark, and each further tap adds to it.
- A sideways drag anywhere moves through the film (the whole screen is 6 minutes). The bar is for long jumps: a finger on it goes straight there, with no knob.
- While moving, the film holds still and the TV's picture strip takes over. Lifting the finger lands and plays.
- Speed, Audio, Subtitles and Episodes open as a list down the right, so the picture stays in view. A tap beside it closes it.
- Over Tailscale it waits for more buffer before starting (2.5 s, and 6 s after running dry) than it does at home (0.5 s, 2 s).
- No chapters, as on the TV.
