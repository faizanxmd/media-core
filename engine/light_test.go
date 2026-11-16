package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestLightRetryAfterSameCutReject(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("no ffmpeg")
	}
	oldM, oldJ := Media, JobsDir
	Media, JobsDir = t.TempDir(), t.TempDir()
	defer func() { Media, JobsDir = oldM, oldJ }()

	var addCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Method string `json:"method"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "application/json")
		switch req.Method {
		case "aria2.getSessionInfo":
			w.Write([]byte(`{"jsonrpc":"2.0","id":"m","result":{"sessionId":"s1"}}`))
		case "aria2.changeGlobalOption":
			w.Write([]byte(`{"jsonrpc":"2.0","id":"m","result":"OK"}`))
		case "aria2.tellActive", "aria2.tellWaiting", "aria2.tellStopped":
			w.Write([]byte(`{"jsonrpc":"2.0","id":"m","result":[]}`))
		case "aria2.getGlobalStat":
			w.Write([]byte(`{"jsonrpc":"2.0","id":"m","result":{"downloadSpeed":"0"}}`))
		case "aria2.addUri":
			addCalls.Add(1)
			w.Write([]byte(`{"jsonrpc":"2.0","id":"m","result":"gid-light-retry"}`))
		default:
			w.Write([]byte(`{"jsonrpc":"2.0","id":"m","result":{}}`))
		}
	}))
	defer srv.Close()

	st := &Store{jobs: map[string]*Job{}, dirty: map[string]bool{}, subs: map[chan struct{}]bool{}}
	a := &Aria2{endpoint: srv.URL, token: "tok", hc: srv.Client()}
	e := NewEngine(st, a, &settingsStore{s: Settings{MaxConcurrent: 1}})

	dir := filepath.Join(Media, "Movies", "Clip (2020)")
	os.MkdirAll(dir, 0o755)
	mainFile := filepath.Join(dir, "Clip (2020).mkv")
	makeClip(t, mainFile, "4")

	lightDir := filepath.Join(dir, ".light")
	os.MkdirAll(lightDir, 0o755)
	lightFile := filepath.Join(lightDir, "Clip (2020).mkv")
	makeClip(t, lightFile, "9")
	sidecar := filepath.Join(lightDir, "Clip (2020).en.srt")
	os.WriteFile(sidecar, []byte("1\n00:00:00,000 --> 00:00:01,000\nHi\n"), 0o644)

	lj := &Job{
		ID:         "lretry1",
		State:      Processing,
		Downloaded: true,
		URL:        "http://example.com/light.mkv",
		Filename:   "light.mkv",
		Plan:       Plan{Kind: "movie", Title: "Clip", Year: "2020", Light: true, Into: rel(dir), Ext: "mkv", MP4: true},
		Log:        []string{},
	}
	st.Add(lj)

	err := e.post(context.Background(), lj.ID)
	if err == nil {
		t.Fatal("expected sameCut error, got nil")
	}
	e.fail(lj.ID, err.Error())

	if exists(lightFile) {
		t.Fatalf("rejected light file %q was not removed", lightFile)
	}
	if exists(sidecar) {
		t.Fatalf("rejected light sidecar %q was not removed", sidecar)
	}
	gotJob, _ := st.Get(lj.ID)
	if gotJob.Placed {
		t.Fatalf("rejected light job should not have Placed=true")
	}

	retried, err := e.Action(lj.ID, "retry", "", false)
	if err != nil {
		t.Fatalf("retry failed: %v", err)
	}
	if addCalls.Load() == 0 {
		t.Fatalf("retry did not re-submit to aria2 (state=%s)", retried.State)
	}
}

func TestWaitForFilmCancelledWithPartialReturnsError(t *testing.T) {
	oldM, oldJ := Media, JobsDir
	Media, JobsDir = t.TempDir(), t.TempDir()
	defer func() { Media, JobsDir = oldM, oldJ }()

	e := testEngine(t)
	filmPlan := Plan{Kind: "movie", Title: "Film", Year: "2020", Ext: "mkv"}
	filmJob := &Job{
		ID:       "film1",
		State:    Downloading,
		Filename: "Film.2020.mkv",
		Plan:     filmPlan,
		Log:      []string{},
	}
	e.store.Add(filmJob)

	tp, err := targetPath(*filmJob)
	if err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(filepath.Dir(tp), 0o755)
	os.WriteFile(tp, []byte("preallocated-partial"), 0o644)
	os.WriteFile(tp+".aria2", []byte("ctrl"), 0o644)

	lightJob := &Job{
		ID:       "light1",
		State:    Processing,
		Filename: "light.mkv",
		Plan:     Plan{Kind: "movie", Title: "Film", Year: "2020", Light: true, Into: "Movies/Film (2020)/Film (2020).mkv", Ext: "mkv"},
		Log:      []string{},
	}
	e.store.Add(lightJob)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- e.waitForFilm(ctx, lightJob.ID)
	}()

	time.Sleep(50 * time.Millisecond)
	e.store.Update(filmJob.ID, true, func(j *Job) { j.State = Cancelled })

	err = <-errCh
	if err == nil {
		t.Fatal("waitForFilm returned nil on cancelled film with partial file")
	}
	if err == context.DeadlineExceeded {
		t.Fatalf("waitForFilm timed out instead of failing promptly when film was cancelled")
	}
}

func TestWaitForFilmRemovedReturnsPromptError(t *testing.T) {
	oldM, oldJ := Media, JobsDir
	Media, JobsDir = t.TempDir(), t.TempDir()
	defer func() { Media, JobsDir = oldM, oldJ }()

	e := testEngine(t)
	filmJob := &Job{
		ID:       "film2",
		State:    Downloading,
		Filename: "Film.2020.zip",
		Plan:     Plan{Kind: "movie", Title: "Film", Year: "2020", Archive: true},
		Log:      []string{},
	}
	e.store.Add(filmJob)

	lightJob := &Job{
		ID:       "light2",
		State:    Processing,
		Filename: "light.mkv",
		Plan:     Plan{Kind: "movie", Title: "Film", Year: "2020", Light: true, Into: "Movies/Film (2020)", Ext: "mkv"},
		Log:      []string{},
	}
	e.store.Add(lightJob)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- e.waitForFilm(ctx, lightJob.ID)
	}()

	time.Sleep(50 * time.Millisecond)
	e.store.Remove(filmJob.ID)

	err := <-errCh
	if err == nil {
		t.Fatal("waitForFilm returned nil when film job was removed")
	}
	if err == context.DeadlineExceeded {
		t.Fatalf("waitForFilm timed out instead of failing promptly when film job was removed")
	}
}
