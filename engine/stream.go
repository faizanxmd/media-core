package main

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Repackaging while a title plays (play.go, "repack"): the file's picture is
// copied untouched into short MP4 pieces that a browser takes as an HLS
// stream, and each sound track is copied or made AAC. Nothing is written to
// disk and the file is never changed.
//
// One ffmpeg per track writes a fragmented MP4 to a pipe. The pieces are cut
// here, not by ffmpeg: every fragment is filed under the piece its time falls
// in, so a piece is the same bytes wherever the run started, and the playlist
// (made from the file's keyframes, keyframes.go) is exact. A jump to a piece
// that isn't there stops the run and starts one at that piece.
//
// Pieces are kept in memory, a window around what's playing. ffmpeg is held
// (SIGSTOP) once it's well ahead, so it neither fills memory nor reads the
// whole film off the disk.

const (
	streamBase   = 10.0            // added to every timestamp, so none is negative
	audioPiece   = 2.048           // seconds: 96 AAC frames at 48 kHz, so pieces from different runs line up
	audioFrame   = 1024.0 / 48000  // one AAC frame
	videoBudget  = 128 << 20       // bytes of picture kept per title (the engine's unit allows it 800M in all)
	audioBudget  = 16 << 20        // bytes of sound kept per track
	aheadSeconds = 45.0            // how far past the player ffmpeg may run,
	aheadBytes   = 64 << 20        // or this much picture, whichever is less
	streamIdle   = 2 * time.Minute // no requests: stop ffmpeg
	streamForget = 15 * time.Minute
)

// cutTimes groups keyframes into pieces at least minLen long. Piece i runs
// from cuts[i] to cuts[i+1]; the last one ends at dur.
func cutTimes(keys []float64, dur, minLen float64) []float64 {
	var cuts []float64
	for _, k := range keys {
		if k >= dur-0.5 && len(cuts) > 0 { // no sliver at the very end
			break
		}
		if len(cuts) == 0 || k-cuts[len(cuts)-1] >= minLen-0.001 {
			cuts = append(cuts, k)
		}
	}
	return cuts
}

type liveStream struct {
	file   string
	dur    float64
	cuts   []float64     // picture pieces' start times
	audio  []probeStream // the file's sound tracks, in its order
	video  int           // the picture's stream index
	vcodec string        // ffprobe's name
	dv5    bool          // Dolby Vision 5: no HDR10 underneath, so it's labelled as Dolby Vision
	ahead  float64       // seconds ffmpeg may run past the player

	mu     sync.Mutex
	tracks map[string]*liveTrack
	last   time.Time
}

type liveTrack struct {
	s      *liveStream
	name   string // "v", or "a<n>c" (copied) / "a<n>t" (made AAC)
	cond   *sync.Cond
	init   []byte
	pieces map[int][]byte
	bytes  int
	want   int // the piece asked for last
	run    *liveRun
}

type liveRun struct {
	cmd    *exec.Cmd
	from   int  // the piece it was started for
	at     int  // the piece it's filling now
	held   bool // SIGSTOPped
	done   bool
	failed string
}

var (
	streamsMu sync.Mutex
	streams   = map[string]*liveStream{} // by file path
)

var streamDebug = os.Getenv("STREAM_DEBUG") != ""

// streamFor returns the file's stream, making it on first use.
// errKeysPending: the file is still being read for its keyframes.
func streamFor(file string, info *MInfo) (*liveStream, error) {
	streamsMu.Lock()
	defer streamsMu.Unlock()
	if s := streams[file]; s != nil {
		s.touch()
		return s, nil
	}
	if info == nil || info.Dur <= 0 {
		return nil, errors.New("the file hasn't been read yet")
	}
	keys, err := keyframes(file)
	if err != nil {
		return nil, err
	}
	if len(keys) < 2 {
		return nil, errors.New("this file has no keyframes to cut at")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	probed, err := probeStreams(ctx, file)
	if err != nil {
		return nil, err
	}
	minLen := 4.0
	if info.Mbps > 12 { // big pictures in small pieces, so a jump fetches less
		minLen = 2.0
	}
	s := &liveStream{file: file, dur: info.Dur, dv5: strings.HasPrefix(info.HDR, "Dolby Vision 5"), cuts: cutTimes(keys, info.Dur, minLen), video: -1, tracks: map[string]*liveTrack{}, last: time.Now()}
	for _, p := range probed {
		switch {
		case p.Type == "video" && s.video < 0 && p.Disp.Pic == 0 && p.Codec != "mjpeg" && p.Codec != "png":
			s.video, s.vcodec = p.Index, p.Codec
		case p.Type == "audio":
			s.audio = append(s.audio, p)
		}
	}
	if s.video < 0 || len(s.cuts) == 0 {
		return nil, errors.New("no picture to stream")
	}
	s.ahead = aheadSeconds
	if perSec := info.Mbps * 1e6 / 8; perSec > 0 {
		s.ahead = math.Max(8, math.Min(aheadSeconds, aheadBytes/perSec))
	}
	if len(streams) == 0 {
		go streamJanitor()
	}
	streams[file] = s
	return s, nil
}

func (s *liveStream) touch() {
	s.mu.Lock()
	s.last = time.Now()
	s.mu.Unlock()
}

// streamJanitor stops ffmpeg for streams nobody is fetching, and forgets them later.
func streamJanitor() {
	for {
		time.Sleep(20 * time.Second)
		streamsMu.Lock()
		for file, s := range streams {
			s.mu.Lock()
			idle := time.Since(s.last)
			if idle > streamIdle {
				for _, t := range s.tracks {
					t.stop()
				}
			}
			s.mu.Unlock()
			if idle > streamForget {
				delete(streams, file)
			}
		}
		n := len(streams)
		streamsMu.Unlock()
		if n == 0 {
			return
		}
	}
}

// dropStream forgets a file's stream at once (the file was replaced or deleted).
func dropStream(file string) {
	streamsMu.Lock()
	s := streams[file]
	delete(streams, file)
	streamsMu.Unlock()
	if s != nil {
		s.mu.Lock()
		for _, t := range s.tracks {
			t.stop()
		}
		s.mu.Unlock()
	}
}

// track returns a named track, or nil when the name makes no sense for this file.
func (s *liveStream) track(name string) *liveTrack {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t := s.tracks[name]; t != nil {
		return t
	}
	if name != "v" {
		n, mode, ok := audioTrackName(name)
		if !ok || n >= len(s.audio) || mode == 'c' && !streamAudio[s.audio[n].Codec] {
			return nil
		}
	}
	t := &liveTrack{s: s, name: name, pieces: map[int][]byte{}}
	t.cond = sync.NewCond(&s.mu)
	s.tracks[name] = t
	return t
}

// audioTrackName reads "a3c" or "a0t": the track's number and whether it's copied or made AAC.
func audioTrackName(name string) (n int, mode byte, ok bool) {
	if len(name) < 3 || name[0] != 'a' {
		return
	}
	mode = name[len(name)-1]
	if mode != 'c' && mode != 't' {
		return
	}
	for _, c := range name[1 : len(name)-1] {
		if c < '0' || c > '9' {
			return
		}
		n = n*10 + int(c-'0')
	}
	return n, mode, true
}

// count: how many pieces the track has.
func (t *liveTrack) count() int {
	if t.name == "v" {
		return len(t.s.cuts)
	}
	return int(math.Ceil(t.s.dur / audioPiece))
}

// start and length of piece i, in the file's own time.
func (t *liveTrack) span(i int) (start, length float64) {
	if t.name != "v" {
		return float64(i) * audioPiece, math.Min(audioPiece, t.s.dur-float64(i)*audioPiece)
	}
	c := t.s.cuts
	end := t.s.dur
	if i+1 < len(c) {
		end = c[i+1]
	}
	return c[i], end - c[i]
}

// pieceAt: which piece a fragment showing at time at (the file's own time) belongs to.
func (t *liveTrack) pieceAt(at float64) int {
	if t.name != "v" {
		return max(0, min(t.count()-1, int(math.Floor((at+0.001)/audioPiece))))
	}
	c := t.s.cuts
	return max(0, sort.Search(len(c), func(i int) bool { return c[i] > at+0.02 })-1)
}

// stop ends the track's ffmpeg. Call with the stream locked.
func (t *liveTrack) stop() {
	if r := t.run; r != nil && !r.done {
		r.cmd.Process.Kill()
	}
	t.run = nil
}

// args builds the ffmpeg command that writes this track from piece i on.
//
// The timestamps have to come out the same whichever piece a run starts at, or
// pieces from different runs wouldn't fit together: -copyts keeps the file's
// own, frag_discont and use_editlist 0 stop the MP4 writer rebasing them to
// zero, and delay_moov lets it write Dolby sound's header. The header is then
// byte for byte the same for every run.
func (t *liveTrack) args(i int) []string {
	s := t.s
	start, _ := t.span(i)
	a := []string{"-nostdin", "-loglevel", "error", "-nostats"}
	seek := func(at float64) {
		if i > 0 && at > 0 {
			a = append(a, "-ss", fmt.Sprintf("%.6f", at))
		}
	}
	out := func(frag string) []string {
		return append(a, "-movflags", frag+"+delay_moov+default_base_moof+frag_discont", "-use_editlist", "0",
			"-avoid_negative_ts", "make_non_negative", "-output_ts_offset", fmt.Sprintf("%.3f", streamBase),
			"-map_metadata", "-1", "-map_chapters", "-1", "-f", "mp4", "pipe:1")
	}
	if t.name == "v" {
		seek(start + 0.14) // ffmpeg aims 0.13 s early for pictures out of order; this lands on the piece's own keyframe
		a = append(a, "-copyts", "-i", s.file, "-map", "0:"+itoa(s.video), "-c:v", "copy")
		if s.vcodec == "hevc" { // the names Apple looks for; unofficial keeps the Dolby Vision record
			tag := "hvc1"
			if s.dv5 {
				tag = "dvh1"
			}
			a = append(a, "-tag:v", tag, "-strict", "unofficial")
		}
		return out("frag_keyframe")
	}
	n, mode, _ := audioTrackName(t.name)
	in := []string{"-copyts", "-i", s.file, "-map", "0:" + itoa(s.audio[n].Index)}
	if mode == 'c' {
		seek(start - 0.2)
		a = append(append(a, in...), "-c:a", "copy")
		return out("frag_every_frame")
	}
	// Made AAC: every run's frames sit on one grid counted from zero, so pieces
	// from different runs meet exactly. A run starts a few frames early, and the
	// encoder's warm-up frame lands in the piece before, which is thrown away.
	seek(start - 3*audioFrame)
	if tr := s.audio[n]; tr.Codec == "dts" && !strings.Contains(tr.Profile, "Express") {
		// DTS-HD is a plain DTS track with a lossless layer on top. Decoding only the
		// plain part takes under half the time and CPU, and it's becoming 192k AAC anyway.
		a = append(a, "-core_only", "1")
	}
	a = append(a, in...)
	if i == 0 {
		a = append(a, "-af", "aresample=async=1:first_pts=0") // sound that starts late is padded back to zero
	}
	a = append(a, "-c:a", "aac", "-ac", "2", "-ar", "48000", "-b:a", "192k")
	return out("frag_every_frame")
}

// begin starts ffmpeg at piece i. Call with the stream locked.
func (t *liveTrack) begin(i int) {
	if streamDebug {
		log.Printf("stream %s: starting at piece %d (asked for %d)", t.name, i, t.want)
	}
	t.stop()
	cmd := exec.Command("ffmpeg", t.args(i)...)
	out, err := cmd.StdoutPipe()
	r := &liveRun{cmd: cmd, from: i, at: i}
	t.run = r
	if err == nil {
		var errOut strings.Builder
		cmd.Stderr = &tailWriter{b: &errOut}
		if err = cmd.Start(); err == nil {
			go t.read(r, out, &errOut)
			return
		}
	}
	r.done, r.failed = true, err.Error()
}

// tailWriter keeps the last of ffmpeg's complaints, for the log.
type tailWriter struct {
	mu sync.Mutex
	b  *strings.Builder
}

func (w *tailWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.b.Len() > 2000 {
		w.b.Reset()
	}
	w.b.Write(p)
	return len(p), nil
}

// read takes ffmpeg's output apart: the header boxes are the track's init
// piece, and each moof (with its mdat) is filed under the piece its time says.
func (t *liveTrack) read(r *liveRun, out io.Reader, errOut *strings.Builder) {
	s := t.s
	br := bufio.NewReaderSize(out, 1<<20)
	var head, cur []byte
	var scale uint32
	curPiece, first := -1, true
	whole := false // the piece being filled was seen from its start
	file := func() {
		// a piece counts once the next one has begun (or the film ended), and only if this run saw its start
		if curPiece >= 0 && whole && len(cur) > 0 {
			s.mu.Lock()
			if t.run == r {
				if _, have := t.pieces[curPiece]; !have {
					t.pieces[curPiece] = cur
					t.bytes += len(cur)
					t.trim()
				}
				t.cond.Broadcast()
			}
			s.mu.Unlock()
		}
		cur = nil
	}
	fail := ""
	for {
		var h [16]byte
		if _, err := io.ReadFull(br, h[:8]); err != nil {
			break
		}
		size, hl := int64(binary.BigEndian.Uint32(h[:4])), 8
		kind := string(h[4:8])
		if size == 1 {
			if _, err := io.ReadFull(br, h[8:16]); err != nil {
				break
			}
			size, hl = int64(binary.BigEndian.Uint64(h[8:16])), 16
		}
		if size < int64(hl) || size > 1<<30 {
			fail = "ffmpeg sent something that isn't MP4"
			break
		}
		box := make([]byte, size)
		copy(box, h[:hl])
		if _, err := io.ReadFull(br, box[hl:]); err != nil {
			break
		}
		switch kind {
		case "ftyp":
			head = append(head, box...)
		case "moov":
			head = append(head, box...)
			scale = moovTimescale(box[hl:])
			s.mu.Lock()
			if t.init == nil {
				t.init = head
			}
			t.cond.Broadcast()
			s.mu.Unlock()
		case "moof":
			if scale == 0 {
				fail = "no timescale in ffmpeg's header"
				break
			}
			at := moofTime(box[hl:], scale) - streamBase
			p := t.pieceAt(at)
			if p != curPiece {
				file()
				start, _ := t.span(p)
				// seen from its start: it begins here, or the run was already going when it began
				whole = !first || p >= r.from || at <= start+0.03
				if streamDebug {
					log.Printf("stream %s: run from %d now at piece %d (time %.3f, whole %v)", t.name, r.from, p, at, whole)
				}
				curPiece = p
				s.mu.Lock()
				stale := t.run != r
				if !stale {
					r.at = max(p, r.from) // a run may start a little before its piece
					t.hold(r)
				}
				s.mu.Unlock()
				if stale {
					return
				}
			}
			first = false
			cur = append(cur, box...)
		case "mfra":
		default:
			if curPiece >= 0 {
				cur = append(cur, box...)
			}
		}
		if fail != "" {
			break
		}
	}
	err := r.cmd.Wait()
	s.mu.Lock()
	killed := t.run != r
	s.mu.Unlock()
	if !killed {
		if fail == "" && err == nil {
			file() // the film ended: the last piece is complete
		} else if fail == "" {
			fail = strings.TrimSpace(errOut.String())
			if fail == "" {
				fail = err.Error()
			}
		}
	}
	s.mu.Lock()
	r.done = true
	if !killed && fail != "" {
		r.failed = fail
		log.Printf("stream %s %s: %s", baseName(s.file), t.name, fail)
	}
	t.cond.Broadcast()
	s.mu.Unlock()
}

// hold stops ffmpeg once it's far enough ahead of the player, and lets it go
// again when the player catches up. Call with the stream locked.
func (t *liveTrack) hold(r *liveRun) {
	if r.done {
		return
	}
	at, _ := t.span(r.at)
	want, _ := t.span(min(t.want, t.count()-1))
	ahead := at-want > t.s.ahead
	if ahead && !r.held {
		r.held = true
		syscall.Kill(r.cmd.Process.Pid, syscall.SIGSTOP)
	} else if !ahead && r.held {
		r.held = false
		syscall.Kill(r.cmd.Process.Pid, syscall.SIGCONT)
	}
}

// trim drops the pieces farthest from the player once the track holds too much.
func (t *liveTrack) trim() {
	budget := audioBudget
	if t.name == "v" {
		budget = videoBudget
	}
	for t.bytes > budget && len(t.pieces) > 2 {
		far, dist := -1, -1
		for i := range t.pieces {
			d := i - t.want
			if d < 0 {
				d = -d * 2 // behind the player goes first
			}
			if d > dist {
				far, dist = i, d
			}
		}
		t.bytes -= len(t.pieces[far])
		delete(t.pieces, far)
	}
}

// header waits for the track's init piece.
func (t *liveTrack) header(ctx context.Context) ([]byte, error) {
	return t.wait(ctx, -1)
}

// piece waits for piece i, starting or restarting ffmpeg when it isn't on its way.
func (t *liveTrack) piece(ctx context.Context, i int) ([]byte, error) {
	if i < 0 || i >= t.count() {
		return nil, errors.New("no such piece")
	}
	return t.wait(ctx, i)
}

func (t *liveTrack) wait(ctx context.Context, i int) ([]byte, error) {
	s := t.s
	stop := context.AfterFunc(ctx, func() { // a dropped request stops waiting
		s.mu.Lock()
		t.cond.Broadcast()
		s.mu.Unlock()
	})
	defer stop()
	deadline := time.Now().Add(40 * time.Second)
	timer := time.AfterFunc(40*time.Second, func() {
		s.mu.Lock()
		t.cond.Broadcast()
		s.mu.Unlock()
	})
	defer timer.Stop()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.last = time.Now()
	if i >= 0 {
		t.want = i
	}
	tries := 0
	for {
		if i < 0 && t.init != nil {
			return t.init, nil
		}
		if b, ok := t.pieces[i]; i >= 0 && ok {
			if r := t.run; r != nil {
				t.hold(r)
			}
			return b, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if time.Now().After(deadline) {
			return nil, errors.New("the stream took too long")
		}
		r := t.run
		switch {
		case r != nil && r.done && r.failed != "":
			if tries++; tries > 2 {
				return nil, errors.New(r.failed)
			}
			t.begin(max(i, 0))
		case i < 0:
			if r == nil || r.done {
				t.begin(t.want)
			}
		case r == nil || r.done || i < r.at || i > r.at+t.near():
			// not on its way: a jump, or a piece the run has passed that was since dropped
			if tries++; tries > 4 {
				return nil, errors.New("the stream couldn't make that piece")
			}
			t.begin(i)
		default:
			t.hold(r)
		}
		t.cond.Wait()
	}
}

// near: how many pieces past the one being filled still count as "on its way".
func (t *liveTrack) near() int {
	_, l := t.span(min(t.want, t.count()-1))
	return max(2, int(8/math.Max(l, 0.5))) // about 8 seconds of film
}

// ---------- reading ffmpeg's boxes ----------

// moovTimescale: the (only) track's clock, ticks per second.
func moovTimescale(moov []byte) uint32 {
	mdhd := mp4Find(moov, "trak", "mdia", "mdhd")
	if len(mdhd) < 20 {
		return 0
	}
	if mdhd[0] == 1 && len(mdhd) >= 32 {
		return binary.BigEndian.Uint32(mdhd[20:])
	}
	return binary.BigEndian.Uint32(mdhd[12:])
}

// moofTime: when a fragment's first sample shows, in seconds: its decode time
// (tfdt) plus that sample's own offset (trun), which is what a keyframe list holds.
func moofTime(moof []byte, scale uint32) float64 {
	traf := mp4Find(moof, "traf")
	var t int64
	if tfdt := mp4Find(traf, "tfdt"); len(tfdt) >= 8 {
		if tfdt[0] == 1 && len(tfdt) >= 12 {
			t = int64(binary.BigEndian.Uint64(tfdt[4:]))
		} else {
			t = int64(binary.BigEndian.Uint32(tfdt[4:]))
		}
	}
	if trun := mp4Find(traf, "trun"); len(trun) >= 8 {
		flags := binary.BigEndian.Uint32(trun) & 0xFFFFFF
		p := trun[8:]
		for _, f := range []uint32{0x1, 0x4, 0x100, 0x200, 0x400} { // what comes before the first sample's offset
			if flags&f != 0 {
				if len(p) < 4 {
					return float64(t) / float64(scale)
				}
				p = p[4:]
			}
		}
		if flags&0x800 != 0 && len(p) >= 4 {
			t += int64(int32(binary.BigEndian.Uint32(p)))
		}
	}
	return float64(t) / float64(scale)
}
