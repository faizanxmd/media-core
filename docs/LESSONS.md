# Lessons from building Media Core

Running notes on how the user and Claude work best on this project. At the end of the current bug-fixing stretch these become skills (TV screenshots, design from a reference, build and release, safe engine deploy, regression check).

## Builds
- Never build after each feature or small fix. Commit, add it to docs/PENDING.md, and build once, only when the user asks for a batch.
- A build costs the user an install and an evening of testing, so it must be worth it.

## Design
- Never design "like Netflix / Apple" from descriptions. Find real screenshots first, show them, agree on them, then build.
- When the user gives a screenshot, measure it and compare ours side by side before saying it matches.
- Copying isn't the goal either: take what works and make it ours, but say plainly where and why we differ.

## Seeing the TV
- The user shouldn't be Claude's only eyes. Keep network debugging on the TV (Settings, System, Developer options) so mint can take screenshots and read frame timings without interrupting playback.
- Check a screen on the TV before calling it done.

## Working together
- A photo or screenshot of a problem beats a description.
- Talk before changing the player.
- Audits by a second model (a quick Sonnet pass) caught real bugs before a build; worth it for larger changes when the user wants one.
