package main

import "testing"

const testDur = int64(100 * 60_000) // 100 minutes

func newTestWatch() *Watch { return &Watch{m: map[string]*WState{}} }

func eps(ids ...string) []*Item {
	var out []*Item
	for _, id := range ids {
		out = append(out, &Item{ID: id, Type: "episode"})
	}
	return out
}

func TestPartWayShowsUpEverywhere(t *testing.T) {
	w := newTestWatch()
	movie := &Item{ID: "m", Type: "movie"}
	w.Progress("m", 30*60_000, testDur, false, 0)
	if s := w.Get("m"); s.Watched || s.Pos != 30*60_000 {
		t.Fatalf("part-way: %+v", s)
	}
	if row := w.continueRow([]*Item{movie}, nil); len(row) != 1 {
		t.Fatalf("not on Continue Watching: %v", row)
	}
}

func TestOpeningBrieflyChangesNothing(t *testing.T) {
	w := newTestWatch()
	w.Progress("new", 5_000, testDur, false, 0)
	if s := w.Get("new"); s.Pos != 0 || s.Watched || s.At != 0 {
		t.Fatalf("never-started item changed: %+v", s)
	}
	w.Progress("seen", 0, 0, true, 0)
	at := w.Get("seen").At
	w.Progress("seen", 5_000, testDur, false, 0)
	if s := w.Get("seen"); !s.Watched || s.Pos != 0 || s.At != at {
		t.Fatalf("finished item changed: %+v", s)
	}
}

func TestRewatchIsPartWayThenFinishedAgain(t *testing.T) {
	w := newTestWatch()
	movie := &Item{ID: "m", Type: "movie"}
	w.Progress("m", 0, 0, true, 0)
	w.Progress("m", 40*60_000, testDur, false, 0)
	if s := w.Get("m"); s.Watched || s.Pos != 40*60_000 || s.Plays != 1 {
		t.Fatalf("rewatch part-way: %+v", s)
	}
	if row := w.continueRow([]*Item{movie}, nil); len(row) != 1 {
		t.Fatal("a rewatch left part-way isn't on Continue Watching")
	}
	w.Progress("m", 95*60_000, testDur, false, 0)
	w.Progress("m", 0, 0, true, 0) // credits roll: the same viewing, not a third play
	if s := w.Get("m"); !s.Watched || s.Pos != 0 || s.Plays != 2 {
		t.Fatalf("rewatch finished: %+v", s)
	}
	if row := w.continueRow([]*Item{movie}, nil); len(row) != 0 {
		t.Fatal("a finished film is still on Continue Watching")
	}
}

func TestMarkingResetsThePosition(t *testing.T) {
	w := newTestWatch()
	w.Progress("m", 40*60_000, testDur, false, 0)
	w.SetWatched([]string{"m"}, true)
	if s := w.Get("m"); !s.Watched || s.Pos != 0 {
		t.Fatalf("marked watched: %+v", s)
	}
	w.SetWatched([]string{"m"}, false)
	if s := w.Get("m"); s.Watched || s.Pos != 0 {
		t.Fatalf("marked unwatched: %+v", s)
	}
}

func TestOldOfflineReportCantUndoNewerProgress(t *testing.T) {
	w := newTestWatch()
	w.Progress("m", 60*60_000, testDur, false, 0) // on the TV, just now
	w.Progress("m", 10*60_000, testDur, false, now()-3600)
	if s := w.Get("m"); s.Pos != 60*60_000 {
		t.Fatalf("an older report won: %+v", s)
	}
	at := now() + 1 // watched offline after the TV stopped
	w.Progress("m", 70*60_000, testDur, false, at)
	if s := w.Get("m"); s.Pos != 70*60_000 || s.At != at {
		t.Fatalf("a newer report lost: %+v", s)
	}
}

func TestRepairOldEntries(t *testing.T) {
	w := newTestWatch()
	w.m["rewatch"] = &WState{Pos: 4_000_000, Dur: 8_000_000, Watched: true, Plays: 3}
	w.m["seen"] = &WState{Watched: true, Plays: 1}
	w.repair()
	if s := w.Get("rewatch"); s.Watched || s.Pos != 4_000_000 || s.Plays != 3 {
		t.Fatalf("rewatch: %+v", s)
	}
	if s := w.Get("seen"); !s.Watched {
		t.Fatalf("finished: %+v", s)
	}
	if !w.dirty {
		t.Fatal("the repair wouldn't be saved")
	}
}

func TestCarryOn(t *testing.T) {
	w := newTestWatch()
	list := eps("e1", "e2", "e3")
	if ep, _ := w.carryOn(list); ep != nil {
		t.Fatalf("nothing played yet, got %s", ep.ID)
	}
	w.Progress("e1", 0, 0, true, 100)
	if ep, _ := w.carryOn(list); ep == nil || ep.ID != "e2" {
		t.Fatalf("after e1 finished: %+v", ep)
	}
	w.Progress("e3", 10*60_000, testDur, false, 200) // skipped ahead: the part-way one wins over unseen e2
	if ep, at := w.carryOn(list); ep == nil || ep.ID != "e3" || at != 200 {
		t.Fatalf("part-way e3: %+v", ep)
	}
	w.Progress("e1", 10*60_000, testDur, false, 300) // back to rewatch e1
	if ep, _ := w.carryOn(list); ep == nil || ep.ID != "e1" {
		t.Fatalf("rewatching e1: %+v", ep)
	}
	w.Progress("e3", 0, 0, true, 400)
	if ep, _ := w.carryOn(list); ep != nil {
		t.Fatalf("finished the last one played, got %s", ep.ID)
	}
}

func TestRepairHandlesNull(t *testing.T) {
	w := &Watch{}
	w.repair()
	if w.m == nil {
		t.Fatal("w.m is nil after repair")
	}

	w2 := &Watch{m: map[string]*WState{
		"bad": nil,
		"ok":  {Pos: 30000, At: 1},
	}}
	w2.repair()
	if _, ok := w2.m["bad"]; ok {
		t.Fatal("nil entry was not deleted")
	}
	if s := w2.m["ok"]; s == nil || s.Pos != 30000 || s.At != 1 {
		t.Fatalf("ok entry changed: %+v", s)
	}
	if !w2.dirty {
		t.Fatal("dirty is false after deleting nil entry")
	}
}

func TestNegativePositionIsNotStored(t *testing.T) {
	w := newTestWatch()
	w.Progress("m", 30*60_000, testDur, false, 0)
	w.Progress("m", -5000, testDur, false, 0)
	if s := w.Get("m"); s.Pos < 0 {
		t.Fatalf("negative position kept: %+v", s)
	}
}
