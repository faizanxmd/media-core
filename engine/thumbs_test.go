package main

import (
	"context"
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestPauseWithTimeoutIgnoresPausedTime(t *testing.T) {
	oldTick := pauseTick.Load()
	pauseTick.Store(int64(20 * time.Millisecond))
	defer func() { pauseTick.Store(oldTick) }()

	playMu.Lock()
	playing["test/stream"] = time.Now()
	playMu.Unlock()
	defer func() {
		playMu.Lock()
		delete(playing, "test/stream")
		playMu.Unlock()
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := exec.CommandContext(ctx, "sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot start sleep: %v", err)
	}
	doneWait := make(chan error, 1)
	go func() { doneWait <- cmd.Wait() }()

	stop := make(chan struct{})
	defer close(stop)
	go pauseWithTimeout(cmd.Process.Pid, stop, 100*time.Millisecond, cancel)

	// Wait 3x longer than activeLimit while streaming is active: must NOT time out.
	time.Sleep(300 * time.Millisecond)
	if ctx.Err() != nil {
		t.Fatalf("timed out while paused for streaming: %v", ctx.Err())
	}

	// Stop streaming: active timer resumes and cancels within ~200ms.
	playMu.Lock()
	delete(playing, "test/stream")
	playMu.Unlock()

	select {
	case <-ctx.Done():
	case <-time.After(1 * time.Second):
		t.Fatal("did not time out after streaming stopped")
	}
	select {
	case <-doneWait:
	case <-time.After(2 * time.Second):
		t.Fatal("process did not exit after timeout cancel")
	}
}

func TestThumbsCleanTmpAndPruneMaps(t *testing.T) {
	oldData := DataDir
	DataDir = t.TempDir()
	defer func() { DataDir = oldData }()

	thumbDir := filepath.Join(DataDir, "thumbs")
	if err := os.MkdirAll(filepath.Join(thumbDir, ".tmp-old1"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(thumbDir, ".tmp-old1", "000001.jpg"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	th := NewThumbs()
	if _, err := os.Stat(filepath.Join(thumbDir, ".tmp-old1")); !os.IsNotExist(err) {
		t.Fatalf("NewThumbs did not remove leftover .tmp-old1")
	}

	// Create a settled video with an existing valid thumbnail .bin file so Generate doesn't invoke ffmpeg.
	mediaDir := t.TempDir()
	vid := filepath.Join(mediaDir, "movie.mkv")
	if err := os.WriteFile(vid, []byte("vid"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	os.Chtimes(vid, old, old)

	binPath, ok := th.path(vid)
	if !ok {
		t.Fatal("th.path failed")
	}
	hdr := make([]byte, 12)
	copy(hdr, "MCT1")
	binary.BigEndian.PutUint32(hdr[4:], thumbEvery)
	binary.BigEndian.PutUint32(hdr[8:], 3)
	if err := os.WriteFile(binPath, hdr, 0o644); err != nil {
		t.Fatal(err)
	}

	// Add another leftover .tmp-* dir and stale entries in counts/failed.
	if err := os.MkdirAll(filepath.Join(thumbDir, ".tmp-old2"), 0o755); err != nil {
		t.Fatal(err)
	}
	staleBin := filepath.Join(thumbDir, "gone-1-1-v1.bin")
	th.mu.Lock()
	th.counts[binPath] = 3
	th.counts[staleBin] = 5
	th.failed[staleBin] = true
	th.mu.Unlock()

	items := map[string]*Item{
		"1": {ID: "1", file: vid, Info: &MInfo{Dur: 30}},
	}
	th.Generate(items, func() {})

	if _, err := os.Stat(filepath.Join(thumbDir, ".tmp-old2")); !os.IsNotExist(err) {
		t.Fatalf("Generate did not remove leftover .tmp-old2")
	}
	th.mu.Lock()
	defer th.mu.Unlock()
	if th.counts[binPath] != 3 {
		t.Fatalf("valid counts entry lost: %v", th.counts)
	}
	if _, exists := th.counts[staleBin]; exists {
		t.Fatalf("stale counts entry was not pruned")
	}
	if _, exists := th.failed[staleBin]; exists {
		t.Fatalf("stale failed entry was not pruned")
	}
}
