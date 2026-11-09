package main

import (
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// WState is how far into a film/episode we are and whether it's been seen.
// This server is the one record every app reads. An item is in exactly one of
// three states: not started (pos 0), part-way (pos > 0, not watched) or
// finished (pos 0, watched). A rewatch is part-way again; Plays keeps the history.
type WState struct {
	Pos     int64   `json:"pos"`           // ms; 0 when finished or never started
	Dur     int64   `json:"dur,omitempty"` // ms, as the player reported it
	Watched bool    `json:"watched"`
	Plays   int     `json:"plays,omitempty"`
	At      float64 `json:"at"` // last activity (unix seconds)
}

type Watch struct {
	mu    sync.Mutex
	m     map[string]*WState
	path  string
	dirty bool
}

var watchPath = filepath.Join(DataDir, "watch.json")

func NewWatch() *Watch {
	w := &Watch{m: map[string]*WState{}, path: watchPath}
	readJSONFile(w.path, &w.m)
	w.repair()
	go func() { // progress ticks arrive every few seconds; write at most every 15 s
		for range time.Tick(15 * time.Second) {
			w.Flush()
		}
	}()
	return w
}

// repair fixes entries saved before the three states were enforced: watched
// with a position was a rewatch left part-way, so it's part-way.
func (w *Watch) repair() {
	for _, s := range w.m {
		if s.Watched && s.Pos > 0 {
			s.Watched = false
			w.dirty = true
		}
	}
}

func (w *Watch) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.dirty {
		writeJSONFile(w.path, w.m)
		w.dirty = false
	}
}

// Move hands an item's progress to its new id, when its file was renamed.
func (w *Watch) Move(from, to string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if s := w.m[from]; s != nil && from != to {
		if w.m[to] == nil {
			w.m[to] = s
		}
		delete(w.m, from)
		w.dirty = true
	}
}

func (w *Watch) Get(id string) *WState {
	w.mu.Lock()
	defer w.mu.Unlock()
	if s := w.m[id]; s != nil {
		cp := *s
		return &cp
	}
	return nil
}

// Progress records a position report from the player. Past 90% (or a
// reported end) counts as watched. at is when it happened, for a report saved
// offline and sent later (0 means now); one older than what we know is dropped,
// so a phone coming back online can't undo what was watched elsewhere since.
func (w *Watch) Progress(id string, pos, dur int64, ended bool, at float64) *WState {
	w.mu.Lock()
	defer w.mu.Unlock()
	s := w.m[id]
	if s == nil {
		s = &WState{}
		w.m[id] = s
	}
	if at > 0 && at < s.At {
		cp := *s
		return &cp
	}
	if at <= 0 {
		at = now()
	}
	s.Dur = max(dur, s.Dur)
	switch {
	case ended || s.Dur > 0 && pos >= s.Dur*9/10:
		if !s.Watched { // count a play once per viewing
			s.Plays++
		}
		s.Watched, s.Pos, s.At = true, 0, at
	case pos < 20_000 && s.Pos == 0:
		// opened and closed straight away: nothing changes, so it stays off Continue Watching
	default:
		s.Watched, s.Pos, s.At = false, pos, at
	}
	w.dirty = true
	cp := *s
	return &cp
}

func (w *Watch) SetWatched(ids []string, watched bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, id := range ids {
		s := w.m[id]
		if s == nil {
			s = &WState{}
			w.m[id] = s
		}
		if watched && !s.Watched {
			s.Plays++
		}
		s.Watched, s.Pos, s.At = watched, 0, now()
	}
	w.dirty = true
}

// decorate returns a copy of it with watch state filled in (and for shows /
// seasons, the unwatched count), recursing into seasons and episodes.
func (w *Watch) decorate(it *Item, deep bool) *Item {
	cp := *it
	cp.Watch = w.Get(it.ID)
	if it.Type == "show" || it.Type == "season" {
		eps := episodesOf(it)
		cp.Unseen = 0
		for _, e := range eps {
			if s := w.Get(e.ID); s == nil || !s.Watched {
				cp.Unseen++
			}
		}
		if !deep {
			cp.Seasons, cp.Eps = nil, nil
			return &cp
		}
		cp.Seasons = nil
		for _, s := range it.Seasons {
			cp.Seasons = append(cp.Seasons, w.decorate(s, true))
		}
		cp.Eps = nil
		for _, e := range it.Eps {
			cp.Eps = append(cp.Eps, w.decorate(e, false))
		}
	}
	return &cp
}

// continueRow is Plex's "Continue Watching": things part-way through, plus
// the next episode of any show you're working through, newest activity first.
func (w *Watch) continueRow(movies, shows []*Item) []*Item {
	type entry struct {
		it *Item
		at float64
	}
	var out []entry
	for _, m := range movies {
		if s := w.Get(m.ID); s != nil && s.Pos > 0 && !s.Watched {
			out = append(out, entry{m, s.At})
		}
	}
	for _, sh := range shows {
		ep, last := w.carryOn(episodesOf(sh))
		if ep == nil || now()-last > 120*86400 { // abandoned for 4 months: drop it
			continue
		}
		out = append(out, entry{ep, last})
	}
	sort.Slice(out, func(a, b int) bool { return out[a].at > out[b].at })
	items := make([]*Item, len(out))
	for i, e := range out {
		items[i] = e.it
	}
	return items
}

// carryOn is where you left off in these episodes, and when: the one played
// last if it's part-way, else the next one not seen yet. nil if there's none.
func (w *Watch) carryOn(eps []*Item) (*Item, float64) {
	var last float64
	lastIdx := -1
	for i, e := range eps {
		if s := w.Get(e.ID); s != nil && (s.Watched || s.Pos > 0) && s.At >= last {
			last, lastIdx = s.At, i
		}
	}
	if lastIdx < 0 {
		return nil, 0
	}
	if s := w.Get(eps[lastIdx].ID); s.Pos > 0 {
		return eps[lastIdx], last
	}
	for _, e := range eps[lastIdx+1:] {
		if s := w.Get(e.ID); s == nil || !s.Watched {
			return e, last
		}
	}
	return nil, 0
}

// nextEpisode is what plays after ep (for autoplay), or nil.
func nextEpisode(c *Catalog, ep *Item) *Item {
	show := c.Get(ep.ShowID)
	if show == nil {
		return nil
	}
	eps := episodesOf(show)
	for i, e := range eps {
		if e.ID == ep.ID && i+1 < len(eps) {
			return eps[i+1]
		}
	}
	return nil
}
