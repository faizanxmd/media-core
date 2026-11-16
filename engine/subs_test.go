package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func makeMKVWithTwoSubs(t *testing.T, path string) {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("no ffmpeg")
	}
	dir := t.TempDir()
	srt1 := filepath.Join(dir, "s1.srt")
	srt2 := filepath.Join(dir, "s2.srt")
	os.WriteFile(srt1, []byte("1\n00:00:00,200 --> 00:00:01,000\nFirst\n"), 0o644)
	os.WriteFile(srt2, []byte("1\n00:00:00,200 --> 00:00:01,000\nSecond\n"), 0o644)
	args := []string{
		"-v", "error", "-y",
		"-f", "lavfi", "-i", "testsrc2=size=160x90:rate=12",
		"-i", srt1, "-i", srt2,
		"-t", "2",
		"-map", "0:v", "-map", "1:s", "-map", "2:s",
		"-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p",
		"-c:s", "srt",
		"-metadata:s:s:0", "language=eng",
		"-metadata:s:s:1", "language=spa",
		path,
	}
	if out, err := exec.Command("ffmpeg", args...).CombinedOutput(); err != nil {
		t.Skipf("ffmpeg: %s", out)
	}
}

func TestSubsUnsettledNoFailedMarker(t *testing.T) {
	oldData, oldMedia := DataDir, Media
	oldRun, oldProbe := subsRunFFmpeg, subsProbe
	DataDir, Media = t.TempDir(), t.TempDir()
	defer func() {
		DataDir, Media = oldData, oldMedia
		subsRunFFmpeg, subsProbe = oldRun, oldProbe
	}()

	partial := filepath.Join(Media, "Movies", "Copy (2025)", "Copy (2025).mkv")
	os.MkdirAll(filepath.Dir(partial), 0o755)
	if err := os.WriteFile(partial, []byte("partial"), 0o644); err != nil {
		t.Fatal(err)
	}

	subsProbe = func(ctx context.Context, p string) ([]probeStream, error) {
		return []probeStream{{Index: 1, Type: "subtitle", Codec: "subrip"}}, nil
	}
	subsRunFFmpeg = func(args []string) (string, error) {
		return "corrupt stream", errors.New("exit status 1")
	}

	// File is brand new (< thumbSettle): extractSubs must NOT write a permanent "failed" marker.
	extractSubs(partial, false)
	dir := subsDir(partial)
	if exists(filepath.Join(dir, "failed")) {
		t.Fatalf("wrote permanent failed marker for unsettled file (%s)", dir)
	}

	// Once settled (>= thumbSettle), a failing extraction records "failed".
	old := time.Now().Add(-time.Hour)
	os.Chtimes(partial, old, old)
	extractSubs(partial, false)
	dir = subsDir(partial)
	if !exists(filepath.Join(dir, "failed")) {
		t.Fatalf("did not write failed marker for settled file (%s)", dir)
	}
}

func TestSubsFillRecomputesKeep(t *testing.T) {
	oldData, oldMedia := DataDir, Media
	DataDir, Media = t.TempDir(), t.TempDir()
	defer func() { DataDir, Media = oldData, oldMedia }()

	vid1 := filepath.Join(Media, "Movies", "Old (2020)", "Old (2020).mkv")
	vid2 := filepath.Join(Media, "Movies", "New (2025)", "New (2025).mkv")
	os.MkdirAll(filepath.Dir(vid1), 0o755)
	os.MkdirAll(filepath.Dir(vid2), 0o755)
	makeMKVWithTwoSubs(t, vid1)
	makeMKVWithTwoSubs(t, vid2)
	old := time.Now().Add(-time.Hour)
	os.Chtimes(vid1, old, old)
	os.Chtimes(vid2, old, old)

	it1 := &Item{ID: "1", file: vid1, Added: 1, Info: &MInfo{Subs: []string{"English"}}}
	it2 := &Item{ID: "2", file: vid2, Added: 2, Info: &MInfo{Subs: []string{"English"}}}

	staleDir := filepath.Join(DataDir, "subs", "stale-1")
	os.MkdirAll(staleDir, 0o755)
	os.WriteFile(filepath.Join(staleDir, "list"), []byte(""), 0o644)

	items1 := map[string]*Item{"1": it1}
	items2 := map[string]*Item{"1": it1, "2": it2}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		fillSubs(items1)
	}()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !subsFill.TryLock() {
			break
		}
		subsFill.Unlock()
		time.Sleep(5 * time.Millisecond)
	}
	fillSubs(items2)
	dir2 := subsDir(vid2)
	os.MkdirAll(dir2, 0o755)
	os.WriteFile(filepath.Join(dir2, "list"), []byte("1\tvtt\teng\t0\t\n"), 0o644)

	wg.Wait()

	if !exists(filepath.Join(dir2, "list")) {
		t.Fatalf("fillSubs deleted subtitles for file added during the run (%s)", dir2)
	}
	if exists(staleDir) {
		t.Fatalf("fillSubs did not prune stale directory %s", staleDir)
	}
}

func waitSubsIdle(t *testing.T) {
	t.Helper()
	dl := time.Now().Add(5 * time.Second)
	for time.Now().Before(dl) {
		subsMu.Lock()
		busy := subsWorking || len(subsBusy) > 0
		subsMu.Unlock()
		if !busy {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("subsWorker did not finish")
}

func TestSubsOnDemandSingleWorkerAndBadStream(t *testing.T) {
	oldData, oldMedia := DataDir, Media
	oldRun, oldProbe := subsRunFFmpeg, subsProbe
	DataDir, Media = t.TempDir(), t.TempDir()
	defer func() {
		waitSubsIdle(t)
		DataDir, Media = oldData, oldMedia
		subsRunFFmpeg, subsProbe = oldRun, oldProbe
	}()

	vid1 := filepath.Join(Media, "Movies", "GoodBad (2025)", "GoodBad (2025).mkv")
	vid2 := filepath.Join(Media, "Movies", "Other (2025)", "Other (2025).mkv")
	os.MkdirAll(filepath.Dir(vid1), 0o755)
	os.MkdirAll(filepath.Dir(vid2), 0o755)
	os.WriteFile(vid1, []byte("v1"), 0o644)
	os.WriteFile(vid2, []byte("v2"), 0o644)
	old := time.Now().Add(-time.Hour)
	os.Chtimes(vid1, old, old)
	os.Chtimes(vid2, old, old)

	subsProbe = func(ctx context.Context, p string) ([]probeStream, error) {
		s1 := probeStream{Index: 1, Type: "subtitle", Codec: "subrip"}
		s1.Tags.Language = "eng"
		s2 := probeStream{Index: 2, Type: "subtitle", Codec: "subrip"}
		s2.Tags.Language = "spa"
		return []probeStream{s1, s2}, nil
	}

	var active, maxActive atomic.Int32
	var vid1Calls atomic.Int32
	subsRunFFmpeg = func(args []string) (string, error) {
		cur := active.Add(1)
		defer active.Add(-1)
		for {
			prev := maxActive.Load()
			if cur <= prev || maxActive.CompareAndSwap(prev, cur) {
				break
			}
		}
		time.Sleep(25 * time.Millisecond)

		joined := strings.Join(args, " ")
		if strings.Contains(joined, vid1) {
			vid1Calls.Add(1)
		}
		// Combined pass fails if both stream 1 and stream 2 are mapped; stream 2 alone fails.
		has1 := strings.Contains(joined, "0:1")
		has2 := strings.Contains(joined, "0:2")
		if has2 {
			return "bad stream 2", errors.New("exit status 1")
		}
		if has1 {
			outPath := args[len(args)-1]
			os.WriteFile(outPath, []byte("WEBVTT\n\n00:00:00.000 --> 00:00:01.000\nHi\n"), 0o644)
			return "", nil
		}
		return "", errors.New("unexpected args")
	}

	it1 := &Item{ID: "gb", file: vid1, Info: &MInfo{Subs: []string{"English", "Spanish"}}}
	it2 := &Item{ID: "ot", file: vid2, Info: &MInfo{Subs: []string{"English", "Spanish"}}}

	// Enqueue it1 repeatedly (should deduplicate) and it2 concurrently.
	for range 5 {
		_, pending := embeddedSubs(it1)
		if !pending {
			t.Fatal("expected pending=true for it1")
		}
	}
	if _, pending := embeddedSubs(it2); !pending {
		t.Fatal("expected pending=true for it2")
	}

	deadline := time.Now().Add(5 * time.Second)
	var ready1, ready2 []embSub
	for time.Now().Before(deadline) {
		r1, p1 := embeddedSubs(it1)
		r2, p2 := embeddedSubs(it2)
		if !p1 && !p2 {
			ready1, ready2 = r1, r2
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if maxActive.Load() != 1 {
		t.Fatalf("expected max concurrency 1, got %d", maxActive.Load())
	}
	// vid1 should only have been processed once (1 combined + 2 fallback = 3 calls, not 5x).
	if got := vid1Calls.Load(); got != 3 {
		t.Fatalf("expected 3 ffmpeg invocations (1 combined + 2 single-stream) for deduplicated vid1, got %d", got)
	}
	if len(ready1) != 1 || ready1[0].Index != 1 || ready1[0].Lang != "eng" {
		t.Fatalf("expected good stream 1 rescued for vid1, got %+v", ready1)
	}
	if len(ready2) != 1 || ready2[0].Index != 1 {
		t.Fatalf("expected good stream 1 rescued for vid2, got %+v", ready2)
	}
}

func TestSubsRealFFmpegTwoStreams(t *testing.T) {
	oldData, oldMedia := DataDir, Media
	DataDir, Media = t.TempDir(), t.TempDir()
	defer func() {
		waitSubsIdle(t)
		DataDir, Media = oldData, oldMedia
	}()

	vid := filepath.Join(Media, "Movies", "TwoSubs (2025)", "TwoSubs (2025).mkv")
	os.MkdirAll(filepath.Dir(vid), 0o755)
	makeMKVWithTwoSubs(t, vid)
	old := time.Now().Add(-time.Hour)
	os.Chtimes(vid, old, old)

	it := &Item{ID: "ts", file: vid, Info: &MInfo{Subs: []string{"English", "Spanish"}}}
	_, pending := embeddedSubs(it)
	if !pending {
		t.Fatal("expected pending=true")
	}
	deadline := time.Now().Add(15 * time.Second)
	var ready []embSub
	for time.Now().Before(deadline) {
		r, p := embeddedSubs(it)
		if !p {
			ready = r
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	if len(ready) != 2 || ready[0].Lang != "eng" || ready[1].Lang != "spa" {
		t.Fatalf("expected 2 subtitles (eng, spa), got %+v", ready)
	}
}
