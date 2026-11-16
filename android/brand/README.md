# Media Core brand

`wordmark.svg` is the logo and `mark.svg` its O, which is also the app icon. `make_logo.py <Montserrat[wght].ttf>` draws
both and the TV banner (`app/src/main/res/drawable-xhdpi/banner.png`), fitted to whole pixels at the launcher's 320 x 180.
The font isn't kept here; get it from google/fonts.

The app icon (`ic_foreground.xml`, one copy in each app), the web icon (`web/icon.svg`) and the engine's PNG icons (`icon.go`) repeat the mark's
numbers by hand. Only the TV app's copy has the refined mark so far: Montserrat's own ring (thinner at the top and bottom) and the lit
core with its glow. The phone, web and engine copies still draw the older mark.

Third-party:
- Source Sans 3, SIL Open Font License 1.1, Copyright 2010-2020 Adobe, with Reserved Font Name 'Source'. The TV app's type (`res/font/source_sans.ttf`, the variable font, weights 200-900); licence text in `android/app/brand-fonts-OFL.txt`.
- Inter, SIL Open Font License 1.1, Copyright 2016 The Inter Project Authors. The player's and the phone's type, and the site's (`res/font/inter.ttf`, `engine/web/fonts/inter.woff2`).
- Montserrat, SIL Open Font License 1.1, Copyright 2024 The Montserrat.Git Project Authors. Used only to draw the logo.
- Fluent UI System Icons, MIT License, Copyright (c) 2020 Microsoft Corporation. The TV's icons, and every icon on the phone and the web.
- hls.js, Apache License 2.0 (`engine/web/vendor/hls.LICENSE`). The web player's streaming engine.
