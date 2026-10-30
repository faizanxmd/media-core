# Media Core design system (TV)

The rules for the app. Every screen follows them, and a value that isn't here doesn't get used.
Sizes are in dp and sp on the TV's 960 x 540 layout (1 dp = 2 px at 1080p).

## The idea: industrial, flat, exact

Media Core is a private cinema, and a polished, high-quality streaming platform. It's built like a piece of good
hardware: one flat canvas, one corner, one white, and blue used so rarely that it means something. Nothing glows,
nothing casts a shadow, and nothing is there unless it does a job. We don't copy other apps.

## Sharpness

This TV draws every app's UI at 1080p and stretches it exactly 2x to its 4K panel (`max_graphics_width=1920`,
`osd_size=1080p`). Only video gets the full 4K. So sharpness comes from how things are drawn:

- Icons are Microsoft's Fluent System Icons, filled, always 24 dp: 48 px, 2 px to a grid unit. One set, one weight.
- Buttons never scale. Only picture cards do, because a picture is already being resampled.
- No text shadows, and no text over photos unless it's over the player's scrim. Names sit under pictures.
- Body text is weight 450, not 400, because thin strokes soften first.
- Whole-pixel layout: six 125 dp posters and 26 dp gaps fill the grid's 880 dp exactly.
- Title pages use the full-size backdrop, not the 1280 px one.

## Tokens

### Colour

| Token | Value | Use |
|---|---|---|
| Base | #0E1014 | every page |
| Raised | #181B21 | menus, panels, empty cards |
| Overlay | #262A32 | controls at rest, the search box |
| Ink | #F4F5F8 | titles, progress, every focused control and border |
| Muted | #A4A8B0 | everything secondary |
| Accent | #0F1E66 | a large area: Resume or Play while selected |
| AccentWord | #1E3AB8 | a small thing: the current tab, a selected player icon, the logo's core |

Small things look darker than large areas of the same colour, so the two blues are meant to read as one. Judge blue only
on the TV, which shows it much lighter than a laptop does. Scrims use Base with alpha.

### Type (Inter)

| Token | Size / line | Weight | Use |
|---|---|---|---|
| Display | 32 / 36 | Bold | a title with no logo art |
| Title | 24 / 28 | SemiBold | focused title, headline figures, the scrub time |
| Heading | 18 / 22 | SemiBold | row titles, the player's title without logo art |
| Body | 14.5 / 20 | 450 | plot text, menu rows |
| Label | 14.5 / 20 | SemiBold | words you act on: tabs, buttons, values (the current tab is ExtraBold) |
| Meta | 13 / 16 | Medium | year, length, card names |
| Caption | 11.5 / 14 | SemiBold | chips, small headings |

Values that change or stack (times, figures) use tabular digits: `Type.x.figures()`.

### Shape

Everything: 6. Chip: 4. No pills or circles, including cast photos.

### Space

4, 8, 12, 16, 24, 32, 40, 64. These gaps are the same on every screen:

| Gap | Size |
|---|---|
| Page side margin | 40 |
| Top of the tabs | 20 |
| Tabs' height | 60 |
| Top of the words on title pages | 56 |
| Row title to its cards | 16 |
| Bottom of a row to the next title | 24 |
| Buttons to the section below | 24 |
| Cards in a row | 16 |
| Buttons in a row | 8 |
| Logo to facts; facts to episode name | 12 |
| Episode name to story | 4 |
| Story to length | 8 |
| Length to buttons | 16 |
| Picture to its name | 8 |

Text on every screen starts on the same left edge. Inside menus, the heading starts where the names do.

### Focus

| Thing | At rest | Selected |
|---|---|---|
| Resume / Play | Overlay, white icon and word | Accent, white icon and word |
| Any other button | Overlay, Ink icon | Ink fill, Base icon |
| Menu row, worded button | clear or Overlay | Ink fill, Base text |
| Picture card | 82% brightness | full brightness, scale 1.04 from the top, a 2 dp Ink border on its edge with no gap |
| Tab | Muted word | Ink word and a 2 dp line under it |
| Current tab | AccentWord, ExtraBold | the same, with the 2 dp line |
| Player icon | Ink | AccentWord, nothing behind it |
| Season heading | Ink word | the 2 dp line |

Progress is always white with square ends: the player's bar and the bars on cards, which sit flush on the picture's bottom edge.

### Motion

The `Motion` timings stay. A page fades up from Base in 160 ms, in one piece: a title page waits up to 300 ms for its
picture first, and Home decodes the picture of a focused title ahead of time. The Details sheet fades in 160 ms and out in 200 ms.
Nothing new is drawn in a separate window (no `Popup`), and nothing blurs live: the sheet's picture is blurred once, off the UI thread.

## Screens

- **One canvas:** there are no bars. The tabs are the top of the page and scroll away with it. Title pages have none.
- **Home:** the tabs, then the featured title with its framed picture. A focused row rises to the top of the screen. Back returns to the top before it leaves the app. No text about a focused poster: the poster carries it.
- **Title pages:** a full still banner fading to Base behind the words and cards. Episode cards show number and name under the picture; the selected episode's details show above, by the buttons.
- **Details sheet:** over the page's blurred picture: headline figures, then Picture, Languages and File. Left, Right, OK or Back close it. Subtitles show as English only.
- **Films and Shows:** the tabs and a poster grid, nothing else.
- **Search:** our own keyboard on the left under the tabs (A-Z, 0-9, Space, Delete; hold Delete to clear), no system keyboard. On the right, posters scroll past beside the tabs: the whole library until you type, then what matches. No subtitles.
- **Labels:** clean audio and subtitle names, no raw file text. Each fact appears once.

## Player

- Nothing on screen while playing. Pause or Up/Down shows the title, bar and controls.
- A tap of Left/Right skips to the exact time and shows one small mark on that side: two arrows and the seconds, adding up with each tap. It fades a second after the last tap.
- Smaller title (40 dp logo), 24 dp icons on the right, flat white bar, no shadows.
- No chapters on the TV: no button, row, cuts in the bar or names.
- The thumbnail strip stays up after letting go: Left/Right step, hold scrubs, OK lands and plays, Back cancels. Don't disturb its smoothness or look.
- Subtitles never run across the bar, including image-based ones.
- Menus don't cover the title.
