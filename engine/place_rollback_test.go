package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func mockAriaServer(t *testing.T, onAdd func(opts map[string]string)) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var addCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "application/json")
		switch req.Method {
		case "aria2.getSessionInfo":
			w.Write([]byte(`{"jsonrpc":"2.0","id":"m","result":{"sessionId":"s1"}}`))
		case "aria2.changeGlobalOption", "aria2.forceRemove", "aria2.removeDownloadResult":
			w.Write([]byte(`{"jsonrpc":"2.0","id":"m","result":"OK"}`))
		case "aria2.tellActive", "aria2.tellWaiting", "aria2.tellStopped":
			w.Write([]byte(`{"jsonrpc":"2.0","id":"m","result":[]}`))
		case "aria2.getGlobalStat":
			w.Write([]byte(`{"jsonrpc":"2.0","id":"m","result":{"downloadSpeed":"0"}}`))
		case "aria2.addUri":
			addCalls.Add(1)
			var opts map[string]string
			if len(req.Params) >= 3 {
				json.Unmarshal(req.Params[2], &opts)
			}
			if onAdd != nil {
				onAdd(opts)
			}
			w.Write([]byte(`{"jsonrpc":"2.0","id":"m","result":"gid-retry"}`))
		default:
			w.Write([]byte(`{"jsonrpc":"2.0","id":"m","result":{}}`))
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &addCalls
}

func newTestEngineWithAria(t *testing.T, ariaURL string, client *http.Client) *Engine {
	t.Helper()
	st := &Store{jobs: map[string]*Job{}, dirty: map[string]bool{}, subs: map[chan struct{}]bool{}}
	a := &Aria2{endpoint: ariaURL, token: "tok", hc: client}
	return NewEngine(st, a, &settingsStore{s: Settings{MaxConcurrent: 1}})
}

func waitJobSettled(t *testing.T, e *Engine, id string) Job {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		j, ok := e.store.Get(id)
		if ok && !j.busy() {
			e.mu.Lock()
			_, posting := e.posts[id]
			e.mu.Unlock()
			if !posting {
				return j
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	j, _ := e.store.Get(id)
	t.Fatalf("job %s did not settle (state=%s, msg=%q)", id, j.State, j.Message)
	return Job{}
}

func TestPlaceFailurePartWayTVEpisodesAndSubtitles(t *testing.T) {
	t.Run("episode_move_fails_mid_batch", func(t *testing.T) {
		oldM, oldJ := Media, JobsDir
		Media, JobsDir = t.TempDir(), t.TempDir()
		defer func() { Media, JobsDir = oldM, oldJ }()

		srv, addCalls := mockAriaServer(t, nil)
		e := newTestEngineWithAria(t, srv.URL, srv.Client())

		p := Plan{Kind: "tv", Title: "Show", Season: "1"}
		j := &Job{
			ID:         "tvfail1",
			State:      Processing,
			URL:        "http://example.com/show.mkv",
			Filename:   "Show.S01E01.mkv",
			Plan:       p,
			Downloaded: true,
			Log:        []string{},
		}
		e.store.Add(j)

		dlDir, _, _, err := jobPaths(p, j.ID)
		if err != nil {
			t.Fatal(err)
		}
		os.MkdirAll(dlDir, 0o755)
		srcFiles := map[string]string{
			"Show.S01E01.mkv":    "ep1-data",
			"Show.S01E02.mkv":    "ep2-data",
			"Show.S02E01.mkv":    "ep3-data",
			"Show.S01E01.en.srt": "sub1-data",
			"Show.S01E02.en.srt": "sub2-data",
		}
		for name, body := range srcFiles {
			if err := os.WriteFile(filepath.Join(dlDir, name), []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
		}

		// Block Season 02 so episodes 1-2 move to Season 01 and episode 3 fails.
		season2 := filepath.Join(Media, "TV Shows", "Show", "Season 02")
		os.MkdirAll(season2, 0o755)
		if err := os.Chmod(season2, 0o555); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Chmod(season2, 0o755) })

		err = e.post(context.Background(), j.ID)
		if err == nil {
			t.Fatal("expected post to fail when Season 02 is read-only")
		}
		e.fail(j.ID, err.Error())

		cur, _ := e.store.Get(j.ID)
		if cur.Placed {
			t.Fatalf("failed job should have Placed=false")
		}

		season1 := filepath.Join(Media, "TV Shows", "Show", "Season 01")
		if ents, _ := os.ReadDir(season1); len(ents) != 1 || ents[0].Name() != ".incoming" {
			t.Fatalf("Season 01 should contain only .incoming after partial failure, got %v", ents)
		}

		for name, want := range srcFiles {
			b, err := os.ReadFile(filepath.Join(dlDir, name))
			if err != nil || string(b) != want {
				t.Fatalf("source file %s not restored after rollback: err=%v got=%q", name, err, string(b))
			}
		}

		// Fix Season 02 permissions and verify Retry places everything without re-downloading.
		if err := os.Chmod(season2, 0o755); err != nil {
			t.Fatal(err)
		}
		if _, err := e.Action(j.ID, "retry", "", false); err != nil {
			t.Fatalf("retry failed: %v", err)
		}
		finalJob := waitJobSettled(t, e, j.ID)
		if finalJob.State != Done || !finalJob.Placed {
			t.Fatalf("expected retry to finish Done & Placed=true, got state=%s placed=%v msg=%q", finalJob.State, finalJob.Placed, finalJob.Message)
		}
		if addCalls.Load() != 0 {
			t.Fatalf("retry re-downloaded instead of placing rolled-back files (%d addUri calls)", addCalls.Load())
		}
		for _, wantPath := range []string{
			filepath.Join(season1, "Show - S01E01.mkv"),
			filepath.Join(season1, "Show - S01E02.mkv"),
			filepath.Join(season1, "Show - S01E01.en.srt"),
			filepath.Join(season1, "Show - S01E02.en.srt"),
			filepath.Join(season2, "Show - S02E01.mkv"),
		} {
			if !exists(wantPath) {
				t.Fatalf("expected placed file %s after retry", wantPath)
			}
		}
		if exists(dlDir) {
			t.Fatalf("incoming dir %s should be cleaned up after retry succeeded", dlDir)
		}
	})

	t.Run("subtitle_move_fails_after_episodes_moved", func(t *testing.T) {
		oldM, oldJ := Media, JobsDir
		Media, JobsDir = t.TempDir(), t.TempDir()
		defer func() { Media, JobsDir = oldM, oldJ }()

		srv, addCalls := mockAriaServer(t, nil)
		e := newTestEngineWithAria(t, srv.URL, srv.Client())

		p := Plan{Kind: "tv", Title: "Show", Season: "1"}
		j := &Job{
			ID:         "tvsubfail",
			State:      Processing,
			URL:        "http://example.com/show.mkv",
			Filename:   "Show.S01E01.mkv",
			Plan:       p,
			Downloaded: true,
			Log:        []string{},
		}
		e.store.Add(j)

		dlDir, _, _, err := jobPaths(p, j.ID)
		if err != nil {
			t.Fatal(err)
		}
		lockedDir := filepath.Join(dlDir, "locked")
		os.MkdirAll(lockedDir, 0o755)
		ep1 := filepath.Join(dlDir, "Show.S01E01.mkv")
		ep2 := filepath.Join(dlDir, "Show.S01E02.mkv")
		sub1 := filepath.Join(dlDir, "Show.S01E01.en.srt")
		sub2 := filepath.Join(lockedDir, "Show.S01E02.en.srt")
		os.WriteFile(ep1, []byte("ep1"), 0o644)
		os.WriteFile(ep2, []byte("ep2"), 0o644)
		os.WriteFile(sub1, []byte("sub1"), 0o644)
		os.WriteFile(sub2, []byte("sub2"), 0o644)

		// Read-only source parent dir causes os.Remove(src) to fail after linking sub2.
		if err := os.Chmod(lockedDir, 0o555); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Chmod(lockedDir, 0o755) })

		err = e.post(context.Background(), j.ID)
		if err == nil {
			t.Fatal("expected post to fail when subtitle source cannot be unlinked")
		}
		e.fail(j.ID, err.Error())

		season1 := filepath.Join(Media, "TV Shows", "Show", "Season 01")
		if ents, _ := os.ReadDir(season1); len(ents) != 1 || ents[0].Name() != ".incoming" {
			t.Fatalf("Season 01 should contain only .incoming after subtitle rollback, got %v", ents)
		}
		for _, srcFile := range []string{ep1, ep2, sub1, sub2} {
			if !exists(srcFile) {
				t.Fatalf("source file %s missing after rollback", srcFile)
			}
		}

		if err := os.Chmod(lockedDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if _, err := e.Action(j.ID, "retry", "", false); err != nil {
			t.Fatalf("retry failed: %v", err)
		}
		finalJob := waitJobSettled(t, e, j.ID)
		if finalJob.State != Done || !finalJob.Placed || addCalls.Load() != 0 {
			t.Fatalf("unexpected retry result: state=%s placed=%v addCalls=%d msg=%q", finalJob.State, finalJob.Placed, addCalls.Load(), finalJob.Message)
		}
		for _, wantPath := range []string{
			filepath.Join(season1, "Show - S01E01.mkv"),
			filepath.Join(season1, "Show - S01E02.mkv"),
			filepath.Join(season1, "Show - S01E01.en.srt"),
			filepath.Join(season1, "Show - S01E02.en.srt"),
		} {
			if !exists(wantPath) {
				t.Fatalf("expected placed file %s after retry", wantPath)
			}
		}
	})
}

func TestPlaceFailurePartWayMovieWithSubtitles(t *testing.T) {
	oldM, oldJ := Media, JobsDir
	Media, JobsDir = t.TempDir(), t.TempDir()
	defer func() { Media, JobsDir = oldM, oldJ }()

	srv, addCalls := mockAriaServer(t, nil)
	e := newTestEngineWithAria(t, srv.URL, srv.Client())

	p := Plan{Kind: "movie", Title: "Film", Year: "2020", Archive: true}
	j := &Job{
		ID:         "movsubfail",
		State:      Processing,
		URL:        "http://example.com/film.zip",
		Filename:   "film.zip",
		Plan:       p,
		Downloaded: true,
		Log:        []string{},
	}
	e.store.Add(j)

	dlDir, _, final, err := jobPaths(p, j.ID)
	if err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(dlDir, "x")
	lockedDir := filepath.Join(src, "locked")
	os.MkdirAll(lockedDir, 0o755)
	vid := filepath.Join(src, "Film.mkv")
	sub1 := filepath.Join(src, "Film.en.srt")
	sub2 := filepath.Join(lockedDir, "Film.es.srt")
	os.WriteFile(vid, []byte("movie-bytes"), 0o644)
	os.WriteFile(sub1, []byte("en-sub"), 0o644)
	os.WriteFile(sub2, []byte("es-sub"), 0o644)

	if err := os.Chmod(lockedDir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(lockedDir, 0o755) })

	if _, err := place(jobCtx{e, j.ID}, p, final, src); err == nil {
		t.Fatal("expected place to fail when second subtitle cannot be removed from locked dir")
	}

	for _, libFile := range []string{
		filepath.Join(final, "Film (2020).mkv"),
		filepath.Join(final, "Film (2020).en.srt"),
		filepath.Join(final, "Film (2020).es.srt"),
	} {
		if exists(libFile) {
			t.Fatalf("library file %s remained after movie subtitle failure", libFile)
		}
	}
	for _, srcFile := range []string{vid, sub1, sub2} {
		if !exists(srcFile) {
			t.Fatalf("source file %s missing after rollback", srcFile)
		}
	}

	// Retry path using direct non-archive place via post/Retry: unlock and verify place succeeds cleanly.
	if err := os.Chmod(lockedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Make it a non-archive post or supply a zip via fake 7z to verify full post+retry.
	binDir := t.TempDir()
	os.WriteFile(filepath.Join(binDir, "7z"), []byte("#!/bin/sh\nexit 0\n"), 0o755)
	t.Setenv("PATH", binDir+":"+os.Getenv("PATH"))
	os.WriteFile(filepath.Join(dlDir, "film.zip"), []byte("zip"), 0o644)
	e.fail(j.ID, "place failed")

	if _, err := e.Action(j.ID, "retry", "", false); err != nil {
		t.Fatalf("retry failed: %v", err)
	}
	finalJob := waitJobSettled(t, e, j.ID)
	if finalJob.State != Done || !finalJob.Placed || addCalls.Load() != 0 {
		t.Fatalf("unexpected retry result: state=%s placed=%v addCalls=%d msg=%q", finalJob.State, finalJob.Placed, addCalls.Load(), finalJob.Message)
	}
	for _, libFile := range []string{
		filepath.Join(final, "Film (2020).mkv"),
		filepath.Join(final, "Film (2020).en.srt"),
		filepath.Join(final, "Film (2020).es.srt"),
	} {
		if !exists(libFile) {
			t.Fatalf("expected library file %s after retry", libFile)
		}
	}
}

func TestPlaceFailurePartWayMusicMultipleAlbums(t *testing.T) {
	writeFakeFFprobe(t, `echo '{}'`)

	t.Run("colliding_albums_leave_library_untouched", func(t *testing.T) {
		oldM, oldJ := Media, JobsDir
		Media, JobsDir = t.TempDir(), t.TempDir()
		defer func() { Media, JobsDir = oldM, oldJ }()

		e := testEngine(t)
		p := Plan{Kind: "music", Title: "Artist"}
		j := &Job{ID: "mucol1", State: Processing, Plan: p, Downloaded: true, Log: []string{}}
		e.store.Add(j)

		dlDir, _, _, err := jobPaths(p, j.ID)
		if err != nil {
			t.Fatal(err)
		}
		p1 := filepath.Join(dlDir, "part1", "SharedAlbum")
		p2 := filepath.Join(dlDir, "part2", "SharedAlbum")
		os.MkdirAll(p1, 0o755)
		os.MkdirAll(p2, 0o755)
		os.WriteFile(filepath.Join(p1, "01.flac"), []byte("a1"), 0o644)
		os.WriteFile(filepath.Join(p1, "cover.jpg"), []byte("cov"), 0o644)
		os.WriteFile(filepath.Join(p2, "01.flac"), []byte("a2"), 0o644)
		os.WriteFile(filepath.Join(p2, "02.flac"), []byte("a3"), 0o644)

		if err := e.post(context.Background(), j.ID); err == nil {
			t.Fatal("expected collision across albums to fail")
		}

		libAlbum := filepath.Join(Media, "Music", "Artist", "SharedAlbum")
		if exists(libAlbum) {
			t.Fatalf("partial album files stayed in library at %s", libAlbum)
		}
		for _, f := range []string{
			filepath.Join(p1, "01.flac"),
			filepath.Join(p1, "cover.jpg"),
			filepath.Join(p2, "01.flac"),
			filepath.Join(p2, "02.flac"),
		} {
			if !exists(f) {
				t.Fatalf("source file %s missing after failed music place", f)
			}
		}
	})

	t.Run("second_album_move_failure_rolls_back_first_album_and_retries", func(t *testing.T) {
		oldM, oldJ := Media, JobsDir
		Media, JobsDir = t.TempDir(), t.TempDir()
		defer func() { Media, JobsDir = oldM, oldJ }()

		srv, addCalls := mockAriaServer(t, nil)
		e := newTestEngineWithAria(t, srv.URL, srv.Client())

		p := Plan{Kind: "music", Title: "Artist"}
		j := &Job{
			ID:         "murol1",
			State:      Processing,
			URL:        "http://example.com/albums",
			Filename:   "release",
			Plan:       p,
			Downloaded: true,
			Log:        []string{},
		}
		e.store.Add(j)

		dlDir, _, _, err := jobPaths(p, j.ID)
		if err != nil {
			t.Fatal(err)
		}
		a1 := filepath.Join(dlDir, "Album1")
		a2 := filepath.Join(dlDir, "Album2")
		os.MkdirAll(a1, 0o755)
		os.MkdirAll(a2, 0o755)
		files := map[string]string{
			filepath.Join(a1, "01.flac"):   "a1-t1",
			filepath.Join(a1, "cover.jpg"): "a1-cov",
			filepath.Join(a1, "01.lrc"):    "a1-lrc",
			filepath.Join(a2, "01.flac"):   "a2-t1",
			filepath.Join(a2, "02.flac"):   "a2-t2",
		}
		for fp, body := range files {
			os.WriteFile(fp, []byte(body), 0o644)
		}

		libA2 := filepath.Join(Media, "Music", "Artist", "Album2")
		os.MkdirAll(libA2, 0o755)
		if err := os.Chmod(libA2, 0o555); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Chmod(libA2, 0o755) })

		err = e.post(context.Background(), j.ID)
		if err == nil {
			t.Fatal("expected post to fail when Album2 destination is read-only")
		}
		e.fail(j.ID, err.Error())

		if cur, _ := e.store.Get(j.ID); cur.Placed {
			t.Fatal("failed music job must have Placed=false")
		}
		libA1 := filepath.Join(Media, "Music", "Artist", "Album1")
		if exists(libA1) {
			t.Fatalf("Album1 remained in library at %s after Album2 failed", libA1)
		}
		for fp, want := range files {
			b, err := os.ReadFile(fp)
			if err != nil || string(b) != want {
				t.Fatalf("music source file %s missing or altered after rollback: err=%v", fp, err)
			}
		}

		if err := os.Chmod(libA2, 0o755); err != nil {
			t.Fatal(err)
		}
		if _, err := e.Action(j.ID, "retry", "", false); err != nil {
			t.Fatalf("retry failed: %v", err)
		}
		finalJob := waitJobSettled(t, e, j.ID)
		if finalJob.State != Done || !finalJob.Placed || addCalls.Load() != 0 {
			t.Fatalf("unexpected retry result: state=%s placed=%v addCalls=%d msg=%q", finalJob.State, finalJob.Placed, addCalls.Load(), finalJob.Message)
		}
		for _, wantPath := range []string{
			filepath.Join(libA1, "01.flac"),
			filepath.Join(libA1, "cover.jpg"),
			filepath.Join(libA1, "01.lrc"),
			filepath.Join(libA2, "01.flac"),
			filepath.Join(libA2, "02.flac"),
		} {
			if !exists(wantPath) {
				t.Fatalf("expected placed music file %s after retry", wantPath)
			}
		}
		if exists(dlDir) {
			t.Fatalf("incoming dir %s should be cleaned up after retry succeeded", dlDir)
		}
	})
}

func TestCancelRightAfterPlaceTVAndRetry(t *testing.T) {
	oldM, oldJ := Media, JobsDir
	Media, JobsDir = t.TempDir(), t.TempDir()
	defer func() { Media, JobsDir = oldM, oldJ }()

	var addCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "application/json")
		switch req.Method {
		case "aria2.getSessionInfo":
			w.Write([]byte(`{"jsonrpc":"2.0","id":"m","result":{"sessionId":"s1"}}`))
		case "aria2.changeGlobalOption", "aria2.forceRemove", "aria2.removeDownloadResult":
			w.Write([]byte(`{"jsonrpc":"2.0","id":"m","result":"OK"}`))
		case "aria2.tellActive", "aria2.tellWaiting", "aria2.tellStopped":
			w.Write([]byte(`{"jsonrpc":"2.0","id":"m","result":[]}`))
		case "aria2.getGlobalStat":
			w.Write([]byte(`{"jsonrpc":"2.0","id":"m","result":{"downloadSpeed":"0"}}`))
		case "aria2.addUri":
			addCalls.Add(1)
			var opts map[string]string
			if len(req.Params) >= 3 {
				json.Unmarshal(req.Params[2], &opts)
			}
			p := filepath.Join(opts["dir"], opts["out"])
			os.MkdirAll(opts["dir"], 0o755)
			os.WriteFile(p, []byte("redownloaded"), 0o644)
			w.Write([]byte(`{"jsonrpc":"2.0","id":"m","result":"gid-redownload"}`))
		default:
			w.Write([]byte(`{"jsonrpc":"2.0","id":"m","result":{}}`))
		}
	}))
	defer srv.Close()

	e := newTestEngineWithAria(t, srv.URL, srv.Client())
	p := Plan{Kind: "tv", Title: "Show", Season: "1"}
	j := &Job{
		ID:         "tvcancel1",
		State:      Processing,
		URL:        "http://example.com/show.mkv",
		Filename:   "Show.S01E01.mkv",
		Plan:       p,
		Downloaded: true,
		Log:        []string{},
	}
	e.store.Add(j)

	dlDir, _, final, err := jobPaths(p, j.ID)
	if err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(dlDir, 0o755)
	os.WriteFile(filepath.Join(dlDir, "Show.S01E01.mkv"), []byte("ep1"), 0o644)
	os.WriteFile(filepath.Join(dlDir, "Show.S01E02.mkv"), []byte("ep2"), 0o644)
	os.WriteFile(filepath.Join(dlDir, "Show.S01E01.en.srt"), []byte("sub1"), 0o644)
	os.WriteFile(filepath.Join(dlDir, "Show.S01E02.en.srt"), []byte("sub2"), 0o644)

	// Pad dlDir with non-media files so os.RemoveAll(dlDir) takes long enough
	// for the store subscriber to cancel right after place() finishes.
	for d := range 20 {
		sub := filepath.Join(dlDir, "junk", string(rune('a'+d)))
		os.MkdirAll(sub, 0o755)
		for f := range 150 {
			os.WriteFile(filepath.Join(sub, string(rune('0'+f%10))+"_"+string(rune('a'+f/10))+".nfo"), nil, 0o644)
		}
	}

	ch := e.store.Subscribe()
	defer e.store.Unsubscribe(ch)

	placedCh := make(chan struct{})
	go func() {
		for range ch {
			cur, ok := e.store.Get(j.ID)
			if ok && strings.HasPrefix(cur.Message, "Placed ") {
				close(placedCh)
				return
			}
		}
	}()

	e.startPost(j.ID)

	select {
	case <-placedCh:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for place to log completion")
	}

	if _, err := e.Action(j.ID, "cancel", "", false); err != nil {
		t.Fatalf("cancel failed: %v", err)
	}
	cancelledJob := waitJobSettled(t, e, j.ID)

	libFiles := []string{
		filepath.Join(final, "Show - S01E01.mkv"),
		filepath.Join(final, "Show - S01E02.mkv"),
		filepath.Join(final, "Show - S01E01.en.srt"),
		filepath.Join(final, "Show - S01E02.en.srt"),
	}
	srcFiles := []string{
		filepath.Join(dlDir, "Show.S01E01.mkv"),
		filepath.Join(dlDir, "Show.S01E02.mkv"),
		filepath.Join(dlDir, "Show.S01E01.en.srt"),
		filepath.Join(dlDir, "Show.S01E02.en.srt"),
	}
	if cancelledJob.Placed {
		for _, lf := range libFiles {
			if !exists(lf) {
				t.Fatalf("Placed=true after cancel, but library file %s is missing", lf)
			}
		}
	} else {
		for _, lf := range libFiles {
			if exists(lf) {
				t.Fatalf("Placed=false after cancel, but library file %s remained", lf)
			}
		}
		for _, sf := range srcFiles {
			if !exists(sf) {
				t.Fatalf("Placed=false after cancel, but source file %s was not rolled back", sf)
			}
		}
	}

	if _, err := e.Action(j.ID, "retry", "", false); err != nil {
		t.Fatalf("retry after cancel failed: %v", err)
	}
	finalJob := waitJobSettled(t, e, j.ID)
	if finalJob.State != Done || !finalJob.Placed {
		t.Fatalf("expected Done & Placed=true after retry, got state=%s placed=%v msg=%q", finalJob.State, finalJob.Placed, finalJob.Message)
	}
	if addCalls.Load() != 0 {
		t.Fatalf("retry triggered %d re-downloads instead of finishing placement", addCalls.Load())
	}
	for _, lf := range libFiles {
		if !exists(lf) {
			t.Fatalf("expected library file %s after retry", lf)
		}
	}
}

func TestCancelDuringMusicPlaceAndImmediateRetry(t *testing.T) {
	oldM, oldJ := Media, JobsDir
	Media, JobsDir = t.TempDir(), t.TempDir()
	defer func() { Media, JobsDir = oldM, oldJ }()

	srv, addCalls := mockAriaServer(t, nil)
	e := newTestEngineWithAria(t, srv.URL, srv.Client())

	p := Plan{Kind: "music", Title: "Artist"}
	j := &Job{
		ID:         "mucancel1",
		State:      Processing,
		URL:        "http://example.com/music",
		Filename:   "release",
		Plan:       p,
		Downloaded: true,
		Log:        []string{},
	}
	e.store.Add(j)

	dlDir, _, _, err := jobPaths(p, j.ID)
	if err != nil {
		t.Fatal(err)
	}
	a1 := filepath.Join(dlDir, "Album1")
	a2 := filepath.Join(dlDir, "Album2")
	os.MkdirAll(a1, 0o755)
	os.MkdirAll(a2, 0o755)
	os.WriteFile(filepath.Join(a1, "01.flac"), []byte("t1"), 0o644)
	os.WriteFile(filepath.Join(a2, "01.flac"), []byte("t2"), 0o644)

	var inProbeOnce sync.Once
	inProbe := make(chan struct{})
	releaseProbe := make(chan struct{})
	var blockProbe atomic.Bool
	blockProbe.Store(true)

	hookSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if blockProbe.Load() {
			inProbeOnce.Do(func() { close(inProbe) })
			select {
			case <-releaseProbe:
			case <-r.Context().Done():
			case <-time.After(2 * time.Second):
			}
		}
		w.Write([]byte(`{}`))
	}))
	defer hookSrv.Close()

	binDir := t.TempDir()
	os.WriteFile(filepath.Join(binDir, "ffprobe"), []byte("#!/bin/sh\ncurl -sS \""+hookSrv.URL+"\" || echo '{}'\n"), 0o755)
	t.Setenv("PATH", binDir+":"+os.Getenv("PATH"))

	e.startPost(j.ID)

	select {
	case <-inProbe:
	case <-time.After(5 * time.Second):
		t.Fatal("fake ffprobe was not reached")
	}

	if _, err := e.Action(j.ID, "cancel", "", false); err != nil {
		t.Fatalf("cancel failed: %v", err)
	}
	blockProbe.Store(false)
	close(releaseProbe)

	// Immediate Retry right after cancel must wait for cancelled post, not race or re-download.
	retried, err := e.Action(j.ID, "retry", "", false)
	if err != nil {
		t.Fatalf("immediate retry failed: %v", err)
	}
	if addCalls.Load() != 0 {
		t.Fatalf("immediate retry re-downloaded (%d addUri calls, state=%s)", addCalls.Load(), retried.State)
	}
	finalJob := waitJobSettled(t, e, j.ID)
	if finalJob.State != Done || !finalJob.Placed {
		t.Fatalf("expected Done & Placed=true after retry, got state=%s placed=%v msg=%q", finalJob.State, finalJob.Placed, finalJob.Message)
	}
	if addCalls.Load() != 0 {
		t.Fatalf("retry re-downloaded (%d addUri calls)", addCalls.Load())
	}
	for _, wantPath := range []string{
		filepath.Join(Media, "Music", "Artist", "Album1", "01.flac"),
		filepath.Join(Media, "Music", "Artist", "Album2", "01.flac"),
	} {
		if !exists(wantPath) {
			t.Fatalf("expected placed file %s", wantPath)
		}
	}
}
