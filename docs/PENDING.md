# Waiting for the next build

Changes committed but not yet built or tested. Cleared after each build the user has tested.

Last published builds: TV 2026.10.30-2319, phone 2026.10.31-0019.

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
