package main

import (
	"context"
	"encoding/binary"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func TestStreamForSingleFlightAndNoGlobalBlock(t *testing.T) {
	oldKF, oldProbe := streamKeyframes, streamProbe
	defer func() {
		streamKeyframes, streamProbe = oldKF, oldProbe
	}()

	startedA := make(chan struct{})
	releaseA := make(chan struct{})
	var callsA atomic.Int32

	streamKeyframes = func(path string) ([]float64, error) {
		return []float64{0, 2, 4, 6}, nil
	}
	streamProbe = func(ctx context.Context, path string) ([]probeStream, error) {
		if path == "/fake/A.mkv" {
			if callsA.Add(1) == 1 {
				close(startedA)
			}
			<-releaseA
		}
		return []probeStream{{Index: 0, Type: "video", Codec: "hevc"}}, nil
	}

	info := &MInfo{Dur: 10, Mbps: 10}
	defer func() {
		dropStream("/fake/A.mkv")
		dropStream("/fake/B.mkv")
	}()

	var wg sync.WaitGroup
	resA := make([]*liveStream, 2)
	errA := make([]error, 2)
	wg.Add(1)
	go func() {
		defer wg.Done()
		resA[0], errA[0] = streamFor("/fake/A.mkv", info)
	}()

	<-startedA

	// Second concurrent call for A should single-flight onto the first.
	wg.Add(1)
	go func() {
		defer wg.Done()
		resA[1], errA[1] = streamFor("/fake/A.mkv", info)
	}()

	// File B must not be blocked by file A's slow init.
	doneB := make(chan struct{})
	var sB *liveStream
	var errB error
	go func() {
		sB, errB = streamFor("/fake/B.mkv", info)
		close(doneB)
	}()

	select {
	case <-doneB:
	case <-time.After(2 * time.Second):
		close(releaseA)
		t.Fatal("streamFor(B) blocked while streamFor(A) was initializing")
	}
	if errB != nil || sB == nil {
		close(releaseA)
		t.Fatalf("streamFor(B) failed: s=%v err=%v", sB, errB)
	}

	close(releaseA)
	wg.Wait()
	if errA[0] != nil || errA[1] != nil {
		t.Fatalf("streamFor(A) errors: %v, %v", errA[0], errA[1])
	}
	if resA[0] == nil || resA[0] != resA[1] {
		t.Fatalf("expected same single-flight stream for A, got %p vs %p", resA[0], resA[1])
	}
	if got := callsA.Load(); got != 1 {
		t.Fatalf("expected 1 probe call for A, got %d", got)
	}
}

func TestDropStreamStopsSIGSTOPpedProcess(t *testing.T) {
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot start sleep: %v", err)
	}
	if err := syscall.Kill(cmd.Process.Pid, syscall.SIGSTOP); err != nil {
		cmd.Process.Kill()
		cmd.Wait()
		t.Fatalf("SIGSTOP failed: %v", err)
	}

	file := "/fake/stopped.mkv"
	s := &liveStream{file: file, tracks: map[string]*liveTrack{}}
	tr := &liveTrack{s: s, name: "v", run: &liveRun{cmd: cmd, held: true}}
	tr.cond = sync.NewCond(&s.mu)
	s.tracks["v"] = tr

	streamsMu.Lock()
	streams[file] = s
	streamsMu.Unlock()

	dropStream(file)

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		syscall.Kill(cmd.Process.Pid, syscall.SIGCONT)
		cmd.Process.Kill()
		t.Fatal("SIGSTOPped process did not exit after dropStream")
	}
}

func firstMoofTime(t *testing.T, initData, pieceData []byte) float64 {
	t.Helper()
	var scale uint32
	for b := initData; len(b) >= 8; {
		sz := int(binary.BigEndian.Uint32(b[:4]))
		if string(b[4:8]) == "moov" {
			scale = moovTimescale(b[8:sz])
			break
		}
		b = b[sz:]
	}
	if scale == 0 {
		t.Fatalf("no timescale in init")
	}
	for b := pieceData; len(b) >= 8; {
		sz := int(binary.BigEndian.Uint32(b[:4]))
		if string(b[4:8]) == "moof" {
			return moofTime(b[8:sz], scale) - streamBase
		}
		b = b[sz:]
	}
	t.Fatalf("no moof in piece")
	return 0
}

func TestLiveTrackCloseKeyframes(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("no ffmpeg")
	}
	for _, tc := range []struct {
		name string
		ext  string
		bf   string
	}{
		{"mkv_bf0", "mkv", "0"},
		{"mkv_bf2", "mkv", "2"},
		{"mp4_bf0", "mp4", "0"},
		{"mp4_bf2", "mp4", "2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "clip."+tc.ext)
			args := []string{
				"-v", "error", "-y",
				"-f", "lavfi", "-i", "testsrc2=size=160x90:rate=24",
				"-t", "13",
				"-c:v", "libx264", "-preset", "ultrafast", "-bf", tc.bf, "-pix_fmt", "yuv420p",
				"-force_key_frames", "0,4,8,8.083333",
				path,
			}
			if out, err := exec.Command("ffmpeg", args...).CombinedOutput(); err != nil {
				t.Skipf("ffmpeg: %s", out)
			}
			info := probeMedia(path)
			if info == nil || info.Dur <= 0 {
				t.Fatalf("probeMedia failed: %+v", info)
			}
			keys, err := keyframes(path)
			if err != nil {
				t.Fatalf("keyframes: %v", err)
			}
			t.Logf("keys: %v", keys)

			s, err := streamFor(path, info)
			if err != nil {
				t.Fatalf("streamFor: %v", err)
			}
			defer dropStream(path)
			tr := s.track("v")
			if tr == nil || tr.count() < 3 {
				t.Fatalf("unexpected track/count: %v", tr)
			}

			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			p2, err := tr.piece(ctx, 2)
			if err != nil {
				t.Fatalf("piece 2: %v", err)
			}
			initData, err := tr.header(ctx)
			if err != nil {
				t.Fatalf("header: %v", err)
			}
			got := firstMoofTime(t, initData, p2)
			wantStart, _ := tr.span(2)
			t.Logf("piece 2 starts at %.6f (want %.6f)", got, wantStart)
			if got > wantStart+0.03 {
				t.Fatalf("piece 2 started at %.6f, missed intended keyframe %.6f", got, wantStart)
			}
		})
	}
}

func TestLiveTrackConcurrentFarPieces(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("no ffmpeg")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "long.mkv")
	// 40s video with keyframes every 2s -> 20 pieces (piece 1 and piece 18 are far apart)
	args := []string{
		"-v", "error", "-y",
		"-f", "lavfi", "-i", "testsrc2=size=160x90:rate=24",
		"-t", "40",
		"-c:v", "libx264", "-preset", "ultrafast", "-g", "48", "-keyint_min", "48", "-sc_threshold", "0", "-pix_fmt", "yuv420p",
		path,
	}
	if out, err := exec.Command("ffmpeg", args...).CombinedOutput(); err != nil {
		t.Skipf("ffmpeg: %s", out)
	}
	info := probeMedia(path)
	if info == nil {
		t.Fatal("probeMedia failed")
	}
	info.Mbps = 20 // 2s pieces so 40s yields ~20 pieces
	s, err := streamFor(path, info)
	if err != nil {
		t.Fatalf("streamFor: %v", err)
	}
	defer dropStream(path)
	tr := s.track("v")
	if tr.count() < 16 {
		t.Fatalf("too few pieces: %d", tr.count())
	}

	// Request piece 1 and piece 15 concurrently (far apart: 15 > 1 + near).
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	errs := make([]error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, errs[0] = tr.piece(ctx, 1)
	}()
	// Small yield so the first request starts its run before the second arrives.
	time.Sleep(15 * time.Millisecond)
	go func() {
		defer wg.Done()
		_, errs[1] = tr.piece(ctx, 15)
	}()
	wg.Wait()
	if errs[0] != nil || errs[1] != nil {
		t.Fatalf("concurrent far piece requests failed: p1=%v, p15=%v", errs[0], errs[1])
	}
}
