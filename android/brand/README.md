# Media Core brand

`wordmark.svg` is the logo and `mark.svg` its O, which is also the app icon. `make_logo.py <Montserrat[wght].ttf>` draws
both and the TV banner (`app/src/main/res/drawable-xhdpi/banner.png`), fitted to whole pixels at the launcher's 320 x 180.
The font isn't kept here; get it from google/fonts.

The app icon (`ic_foreground.xml`), the web icon (`web/icon.svg`) and the engine's PNG icons (`icon.go`) repeat the mark's
numbers by hand.

Third-party:
- Montserrat, SIL Open Font License 1.1, Copyright 2024 The Montserrat.Git Project Authors. Used only to draw the logo.
- Fluent UI System Icons, MIT License, Copyright (c) 2020 Microsoft Corporation. Every icon on the TV.
- Solar icons, CC BY 4.0, by 480 Design. Some of the phone app's icons.
- Phosphor Icons, MIT License, Copyright (c) 2023 Phosphor Icons. The phone app's outline icons.
