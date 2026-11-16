package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestDeleteFromLibraryChecksIntoAndDropsStream(t *testing.T) {
	oldM, oldJ := Media, JobsDir
	Media, JobsDir = t.TempDir(), t.TempDir()
	defer func() { Media, JobsDir = oldM, oldJ }()

	dir := filepath.Join(Media, "Movies", "Film (2020)")
	ldir := filepath.Join(dir, ".light")
	os.MkdirAll(ldir, 0o755)
	mainFile := filepath.Join(dir, "Film (2020).mkv")
	lightFile := filepath.Join(ldir, "Film (2020).mp4")
	os.WriteFile(mainFile, []byte("main"), 0o644)
	os.WriteFile(lightFile, []byte("light"), 0o644)

	e := testEngine(t)
	e.store.jobs["j1"] = &Job{
		ID:    "j1",
		State: Downloading,
		Plan:  Plan{Kind: "movie", Light: true, Into: rel(mainFile), Ext: "mp4"},
	}

	if _, err := e.DeleteFromLibrary(rel(mainFile)); err == nil {
		t.Fatal("DeleteFromLibrary succeeded despite busy Light job targeting Plan.Into")
	}

	e.store.jobs["j1"].State = Done

	streamsMu.Lock()
	streams[mainFile] = &liveStream{file: mainFile, tracks: map[string]*liveTrack{}}
	streams[lightFile] = &liveStream{file: lightFile, tracks: map[string]*liveTrack{}}
	streamsMu.Unlock()
	defer func() {
		streamsMu.Lock()
		delete(streams, mainFile)
		delete(streams, lightFile)
		streamsMu.Unlock()
	}()

	if _, err := e.DeleteFromLibrary(rel(dir)); err != nil {
		t.Fatalf("DeleteFromLibrary(folder) failed: %v", err)
	}

	streamsMu.Lock()
	_, hasMain := streams[mainFile]
	_, hasLight := streams[lightFile]
	streamsMu.Unlock()
	if hasMain || hasLight {
		t.Fatalf("streams not dropped: main=%v light=%v", hasMain, hasLight)
	}
}

func TestLightDeleteChecksRemoveErrorAndDropsStream(t *testing.T) {
	oldM, oldJ := Media, JobsDir
	Media, JobsDir = t.TempDir(), t.TempDir()
	defer func() { Media, JobsDir = oldM, oldJ }()

	dir := filepath.Join(Media, "Movies", "Film (2020)")
	ldir := filepath.Join(dir, ".light")
	os.MkdirAll(ldir, 0o755)
	mainFile := filepath.Join(dir, "Film (2020).mkv")
	lightFile := filepath.Join(ldir, "Film (2020).mp4")
	os.WriteFile(mainFile, []byte("main"), 0o644)
	os.WriteFile(lightFile, []byte("light"), 0o644)

	it := &Item{ID: "m1", Type: "movie", Title: "Film", file: mainFile, Light: &Light{Size: 5, file: lightFile}}
	cat := &Catalog{items: map[string]*Item{"m1": it}}
	srv := &Server{e: testEngine(t), p: &Player{c: cat, w: newTestWatch(), t: &TMDB{}}}
	h := srv.routes()

	streamsMu.Lock()
	streams[lightFile] = &liveStream{file: lightFile, tracks: map[string]*liveTrack{}}
	streamsMu.Unlock()
	defer func() {
		streamsMu.Lock()
		delete(streams, lightFile)
		streamsMu.Unlock()
	}()

	req := httptest.NewRequest("POST", "/api/manager/light/delete", bytes.NewBufferString(`{"id":"m1"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("delete light status = %d: %s", rec.Code, rec.Body.String())
	}

	streamsMu.Lock()
	_, still := streams[lightFile]
	streamsMu.Unlock()
	if still {
		t.Fatal("POST /api/manager/light/delete did not drop light stream")
	}

	// Second delete when file is already gone on disk must report the os.Remove error.
	req2 := httptest.NewRequest("POST", "/api/manager/light/delete", bytes.NewBufferString(`{"id":"m1"}`))
	req2.Header.Set("Content-Type", "application/json")
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code == 200 {
		t.Fatalf("expected error when os.Remove fails, got 200: %s", rec2.Body.String())
	}
}

func TestLightPostRefusesBusyAndFillsPlanFields(t *testing.T) {
	oldM, oldJ := Media, JobsDir
	Media, JobsDir = t.TempDir(), t.TempDir()
	defer func() { Media, JobsDir = oldM, oldJ }()

	movDir := filepath.Join(Media, "Movies", "Film (2020)")
	epDir := filepath.Join(Media, "TV Shows", "The Bear", "Season 02")
	os.MkdirAll(movDir, 0o755)
	os.MkdirAll(epDir, 0o755)
	movFile := filepath.Join(movDir, "Film (2020).mkv")
	epFile := filepath.Join(epDir, "The Bear - S02E05.mkv")
	os.WriteFile(movFile, []byte("m"), 0o644)
	os.WriteFile(epFile, []byte("e"), 0o644)

	mov := &Item{ID: "m1", Type: "movie", Title: "Film", Year: 2020, file: movFile, Info: &MInfo{Res: "4K", Dur: 100}}
	ep := &Item{ID: "e1", Type: "episode", Title: "Pop", ShowID: "s1", Show: "The Bear", Season: 2, Episode: 5, file: epFile, Info: &MInfo{Res: "4K", Dur: 100}}
	sh := &Item{ID: "s1", Type: "show", Title: "The Bear"}
	cat := &Catalog{items: map[string]*Item{"m1": mov, "e1": ep, "s1": sh}}

	e := testEngine(t)
	e.aria = &Aria2{
		endpoint: "http://mock/jsonrpc",
		hc: &http.Client{Transport: rtFunc(func(r *http.Request) (*http.Response, error) {
			return jsonResp(`{"jsonrpc":"2.0","id":"m","result":"gid1"}`), nil
		})},
	}
	// Busy job targeting movFile.
	e.store.jobs["busy1"] = &Job{
		ID:    "busy1",
		State: Downloading,
		Plan:  Plan{Kind: "movie", Light: true, Into: rel(movFile), Ext: "mp4"},
	}

	srv := &Server{e: e, p: &Player{c: cat, w: newTestWatch(), t: &TMDB{}}}
	h := srv.routes()

	probeBody := func(id string) string {
		return `{"id":"` + id + `","probe":{"url":"https://example.com/light.mp4","plan":{"ext":"mp4"},"media":{"vcodec":"H.264","res":"1080p","dur":100}}}`
	}

	req := httptest.NewRequest("POST", "/api/manager/light", bytes.NewBufferString(probeBody("m1")))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code == 200 {
		t.Fatalf("expected busy refusal for m1, got 200: %s", rec.Body.String())
	}

	// Clear busy job and test movie lightPlan Year.
	e.store.jobs["busy1"].State = Done
	req2 := httptest.NewRequest("POST", "/api/manager/light", bytes.NewBufferString(probeBody("m1")))
	req2.Header.Set("Content-Type", "application/json")
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != 200 {
		t.Fatalf("movie light status = %d: %s", rec2.Code, rec2.Body.String())
	}
	var mj Job
	json.Unmarshal(rec2.Body.Bytes(), &mj)
	if mj.Plan.Title != "Film" || mj.Plan.Year != "2020" {
		t.Fatalf("movie light plan = %+v, want Title=Film Year=2020", mj.Plan)
	}

	// Test episode lightPlan Show/Season/Episode.
	req3 := httptest.NewRequest("POST", "/api/manager/light", bytes.NewBufferString(probeBody("e1")))
	req3.Header.Set("Content-Type", "application/json")
	rec3 := httptest.NewRecorder()
	h.ServeHTTP(rec3, req3)
	if rec3.Code != 200 {
		t.Fatalf("ep light status = %d: %s", rec3.Code, rec3.Body.String())
	}
	var ej Job
	json.Unmarshal(rec3.Body.Bytes(), &ej)
	if ej.Plan.Title != "The Bear" || ej.Plan.Season != "2" || ej.Plan.Episode != "5" {
		t.Fatalf("episode light plan = %+v, want Title='The Bear' Season=2 Episode=5", ej.Plan)
	}
}
