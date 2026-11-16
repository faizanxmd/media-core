package main

import (
	"context"
	"encoding/binary"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func TestCutTimes(t *testing.T) {
	// keyframes every 2.002 s after two at the very start, as a 4K WEB-DL has them
	keys := []float64{0, 0.083}
	for k := 2.085; k < 60; k += 2.002 {
		keys = append(keys, k)
	}
	cuts := cutTimes(keys, 60, 2)
	if cuts[0] != 0 || cuts[1] != 2.085 {
		t.Errorf("starts %v: the keyframe at 0.083 should fold into the first piece", cuts[:2])
	}
	for i := 1; i < len(cuts); i++ {
		if d := cuts[i] - cuts[i-1]; d < 1.999 {
			t.Errorf("piece %d is %.3f s, shorter than asked", i-1, d)
		}
	}
	if last := cuts[len(cuts)-1]; last >= 59.5 {
		t.Errorf("last piece starts at %.3f: a sliver at the end", last)
	}
	// long gaps stay whole, short ones are grouped
	cuts = cutTimes([]float64{0, 1, 2, 3, 13, 13.5, 20}, 30, 4)
	if want := []float64{0, 13, 20}; len(cuts) != len(want) || cuts[1] != 13 || cuts[2] != 20 {
		t.Errorf("cuts %v, want %v", cuts, want)
	}
}

func TestPieceAt(t *testing.T) {
	s := &liveStream{dur: 30, cuts: []float64{0, 2.085, 4.087, 6.089}}
	v := &liveTrack{s: s, name: "v"}
	for at, want := range map[float64]int{0: 0, 0.083: 0, 2.084: 1, 2.085: 1, 4.0: 1, 4.087: 2, 29: 3, -0.5: 0} {
		if got := v.pieceAt(at); got != want {
			t.Errorf("picture at %.3f: piece %d, want %d", at, got, want)
		}
	}
	a := &liveTrack{s: s, name: "a0t"}
	for at, want := range map[float64]int{-0.021: 0, 0: 0, 2.046: 0, 2.0475: 1, 2.048: 1, 4.0955: 2, 100: 14} {
		if got := a.pieceAt(at); got != want {
			t.Errorf("sound at %.4f: piece %d, want %d", at, got, want)
		}
	}
	if n := a.count(); n != 15 {
		t.Errorf("30 s of sound: %d pieces, want 15", n)
	}
	if _, l := v.span(3); l < 23.9 || l > 23.92 {
		t.Errorf("last picture piece runs %.3f s, want to the end of the film", l)
	}
}

func TestAudioTrackName(t *testing.T) {
	for name, want := range map[string]string{"a0c": "0c", "a12t": "12t", "v": "", "a0": "", "a0x": "", "ac": "", "a-1t": ""} {
		n, mode, ok := audioTrackName(name)
		got := ""
		if ok {
			got = itoa(n) + string(mode)
		}
		if got != want {
			t.Errorf("%q: %q, want %q", name, got, want)
		}
	}
}

func TestMasterPlaylist(t *testing.T) {
	tr := func(codec, lang, title string, ch, def int) probeStream {
		p := probeStream{Type: "audio", Codec: codec, Channels: ch}
		p.Tags.Language, p.Tags.Title, p.Disp.Default = lang, title, def
		return p
	}
	// a foreign release: the dub first and marked default, English second, a commentary third
	s := &liveStream{vcodec: "hevc", audio: []probeStream{tr("eac3", "hin", "", 2, 1), tr("dts", "eng", "", 6, 0), tr("ac3", "eng", "Director's commentary", 2, 0)}}
	m := &MInfo{Res: "4K", VBits: 10, HDR: "Dolby Vision 8.1", Width: 3840, Height: 2160, FPS: 23.976, Mbps: 29.2}
	safari := s.masterPlaylist(m, parseCaps("hevc,hdr,dv,aac,ac3,eac3,switch"))
	for _, want := range []string{`NAME="Hindi",LANGUAGE="hin",DEFAULT=NO`, `URI="a0c.m3u8"`, `NAME="English",LANGUAGE="eng",DEFAULT=YES`, `URI="a1t.m3u8"`,
		`NAME="English commentary"`, `URI="a2c.m3u8"`, `CODECS="hvc1.2.4.L153.B0,ec-3,mp4a.40.2,ac-3"`, "VIDEO-RANGE=PQ", "RESOLUTION=3840x2160"} {
		if !strings.Contains(safari, want) {
			t.Errorf("Safari's playlist lacks %s:\n%s", want, safari)
		}
	}
	chrome := s.masterPlaylist(m, parseCaps("hevc,hdr,aac,opus,flac"))
	if strings.Contains(chrome, "ec-3") || strings.Contains(chrome, "ac-3") || !strings.Contains(chrome, `URI="a0t.m3u8"`) || !strings.Contains(chrome, `URI="a2t.m3u8"`) {
		t.Errorf("Chrome should get every track as AAC:\n%s", chrome)
	}
	if got := videoCodecName(&MInfo{HDR: "Dolby Vision 5", VBits: 10}, "hevc"); got != "dvh1.05.06" {
		t.Errorf("Dolby Vision 5 is named %q", got)
	}
}

func TestEBMLHead(t *testing.T) {
	// a Cues id (4 bytes) with a 2-byte size of 0x1234, then an unknown-size Segment
	id, size, n, ok := ebmlHead([]byte{0x1C, 0x53, 0xBB, 0x6B, 0x52, 0x34, 0, 0})
	if !ok || id != mkvCues || size != 0x1234 || n != 6 {
		t.Errorf("cues head: id %x size %x n %d ok %v", id, size, n, ok)
	}
	if _, size, _, ok := ebmlHead([]byte{0x18, 0x53, 0x80, 0x67, 0x01, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}); !ok || size != -1 {
		t.Errorf("unknown size: %d, %v", size, ok)
	}
}

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
