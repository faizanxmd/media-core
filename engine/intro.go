package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"log"
	"math/bits"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Skip Intro: the opening of every episode in a season is
// audio-fingerprinted (ffmpeg's chromaprint muxer), and the stretch two
// episodes share near their start is the intro. Same idea as Jellyfin's
// intro-skipper plugin. Runs niced in the background after a scan, a few
// seconds of CPU per episode; results are cached forever per file.

const (
	introScanSecs = 600    // look at the first 10 minutes
	fpItem        = 0.1238 // seconds per chromaprint item (4096-sample frames, 1/3 overlap @ 11025 Hz)
	fpMaxBits     = 6      // items differing in <= 6 of 32 bits count as the same sound
	fpMaxGap      = 28     // tolerate short mismatches (a dialogue line over the theme)
	introMin      = 15.0
	introMax      = 150.0
	introVersion  = 1
)

// Span is a stretch of an episode in seconds (intro, credits).
type Span struct {
	Start float64 `json:"start"`
	End   float64 `json:"end"`
}

type introEntry struct {
	Size   int64   `json:"size"`
	Mtime  int64   `json:"mtime"`
	Intro  *Span   `json:"intro,omitempty"` // nil: analysed, no intro found
	Failed bool    `json:"failed,omitempty"`
	V      int     `json:"v"`
	At     float64 `json:"at"`
}

type Intros struct {
	mu    sync.Mutex
	m     map[string]*introEntry // episode file -> result
	path  string
	fpDir string
	run   sync.Mutex
}

var introsPath = filepath.Join(DataDir, "intros.json")

func NewIntros() *Intros {
	fpDir := filepath.Join(DataDir, "fp")
	in := &Intros{m: map[string]*introEntry{}, path: introsPath, fpDir: fpDir}
	readJSONFile(in.path, &in.m)
	if in.m == nil {
		in.m = map[string]*introEntry{}
	}
	os.MkdirAll(fpDir, 0o755)
	return in
}

func (in *Intros) getFPDir() string {
	if in.fpDir != "" {
		return in.fpDir
	}
	if in.path != "" {
		return filepath.Join(filepath.Dir(in.path), "fp")
	}
	return ""
}

func fileKey(p string) (int64, int64, bool) {
	fi, err := os.Stat(p)
	if err != nil {
		return 0, 0, false
	}
	return fi.Size(), fi.ModTime().Unix(), true
}

// Get returns the cached intro for an episode file (nil if none/unknown).
func (in *Intros) Get(file string) *Span {
	in.mu.Lock()
	defer in.mu.Unlock()
	e := in.m[file]
	if e == nil || e.Intro == nil {
		return nil
	}
	cp := *e.Intro
	return &cp
}

func fileSettled(p string) (int64, int64, bool) {
	fi, err := os.Stat(p)
	if err != nil || time.Since(fi.ModTime()) < thumbSettle {
		return 0, 0, false
	}
	return fi.Size(), fi.ModTime().Unix(), true
}

func (in *Intros) known(file string) bool {
	size, mtime, ok := fileKey(file)
	in.mu.Lock()
	defer in.mu.Unlock()
	e := in.m[file]
	return ok && e != nil && e.V == introVersion && e.Size == size && e.Mtime == mtime
}

func (in *Intros) isFailed(file string) bool {
	size, mtime, ok := fileKey(file)
	in.mu.Lock()
	defer in.mu.Unlock()
	e := in.m[file]
	return ok && e != nil && e.V == introVersion && e.Size == size && e.Mtime == mtime && e.Failed
}

// pruneLocked removes intros.json and fp/*.fp entries for files no longer in shows.
// Call with in.run held.
func (in *Intros) pruneLocked(shows []*Item) {
	seen := map[string]bool{}
	wantFP := map[string]bool{}
	for _, sh := range shows {
		for _, sn := range sh.Seasons {
			for _, ep := range sn.Eps {
				if ep.file == "" {
					continue
				}
				seen[ep.file] = true
				wantFP[pathID(rel(ep.file))+".fp"] = true
			}
		}
	}
	if len(seen) == 0 {
		return // library not mounted? keep everything
	}
	in.mu.Lock()
	pruned := false
	for f := range in.m {
		if !seen[f] {
			delete(in.m, f)
			pruned = true
		}
	}
	if pruned && in.path != "" {
		writeJSONFile(in.path, in.m)
	}
	in.mu.Unlock()
	if fpDir := in.getFPDir(); fpDir != "" {
		ents, _ := os.ReadDir(fpDir)
		for _, e := range ents {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".fp") && !wantFP[e.Name()] {
				os.Remove(filepath.Join(fpDir, e.Name()))
			}
		}
	}
}

// Pending reports whether any season has settled episodes not analysed yet,
// and prunes entries for deleted files.
func (in *Intros) Pending(shows []*Item) bool {
	if in.run.TryLock() {
		in.pruneLocked(shows)
		in.run.Unlock()
	}
	for _, sh := range shows {
		for _, sn := range sh.Seasons {
			if len(sn.Eps) < 2 {
				continue
			}
			for _, ep := range sn.Eps {
				if !in.known(ep.file) {
					if _, _, ok := fileSettled(ep.file); ok {
						return true
					}
				}
			}
		}
	}
	return false
}

// Analyse fingerprints new episodes and matches each against its neighbours
// in the season. Returns true when something new was learned.
func (in *Intros) Analyse(shows []*Item) bool {
	if !in.run.TryLock() {
		return false
	}
	defer in.run.Unlock()
	in.pruneLocked(shows)
	changed := false
	for _, sh := range shows {
		for _, sn := range sh.Seasons {
			eps := sn.Eps
			if len(eps) < 2 {
				continue
			}
			todo := false
			for _, ep := range eps {
				if !in.known(ep.file) {
					if _, _, ok := fileSettled(ep.file); ok {
						todo = true
					}
				}
			}
			if !todo {
				continue
			}
			start := time.Now()
			fps := make([][]uint32, len(eps))
			for i, ep := range eps {
				if !in.isFailed(ep.file) {
					fps[i] = fingerprint(ep.file)
				}
			}
			found := 0
			for i, ep := range eps {
				if in.known(ep.file) {
					continue
				}
				size, mtime, settled := fileSettled(ep.file)
				if !settled {
					continue
				}
				if fps[i] == nil {
					in.mu.Lock()
					in.m[ep.file] = &introEntry{Size: size, Mtime: mtime, Failed: true, V: introVersion, At: now()}
					in.mu.Unlock()
					changed = true
					continue
				}
				// best match against up to two neighbours on each side
				var best *Span
				for _, j := range []int{i + 1, i - 1, i + 2, i - 2} {
					if j < 0 || j >= len(eps) || fps[j] == nil {
						continue
					}
					if a, _ := sharedRun(fps[i], fps[j]); a != nil && (best == nil || a.End-a.Start > best.End-best.Start) {
						best = a
					}
				}
				in.mu.Lock()
				in.m[ep.file] = &introEntry{Size: size, Mtime: mtime, Intro: best, V: introVersion, At: now()}
				in.mu.Unlock()
				if best != nil {
					found++
				}
				changed = true
			}
			log.Printf("intros: %s %s: intro found in %d/%d episodes (%s)", sh.Title, sn.Title, found, len(eps),
				time.Since(start).Round(time.Second))
		}
	}
	if changed && in.path != "" {
		in.mu.Lock()
		writeJSONFile(in.path, in.m)
		in.mu.Unlock()
	}
	return changed
}

// fingerprint returns the chromaprint of a file's first minutes, cached on disk.
func fingerprint(file string) []uint32 {
	size, mtime, ok := fileSettled(file)
	if !ok {
		return nil
	}
	cache := filepath.Join(DataDir, "fp", pathID(rel(file))+".fp")
	if b, err := os.ReadFile(cache); err == nil && len(b) > 16 &&
		int64(binary.LittleEndian.Uint64(b)) == size && int64(binary.LittleEndian.Uint64(b[8:])) == mtime {
		return decodeFP(b[16:])
	}
	waitForNoStreams()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := lowPrio(ctx, "ffmpeg", "-nostdin", "-v", "error", "-t", itoa(introScanSecs), "-i", file,
		"-map", "0:a:0", "-ac", "1", "-f", "chromaprint", "-fp_format", "raw", "-")
	var outBuf bytes.Buffer
	cmd.Stdout = &outBuf
	if err := cmd.Start(); err != nil {
		log.Printf("intros: fingerprint %s: %v", rel(file), err)
		return nil
	}
	stop := make(chan struct{})
	go pauseWithTimeout(cmd.Process, stop, 4*time.Minute, cancel)
	err := cmd.Wait()
	close(stop)
	out := outBuf.Bytes()
	if err != nil || len(out) < 4*100 {
		log.Printf("intros: fingerprint %s: %v", rel(file), err)
		return nil
	}
	hdr := make([]byte, 16)
	binary.LittleEndian.PutUint64(hdr, uint64(size))
	binary.LittleEndian.PutUint64(hdr[8:], uint64(mtime))
	os.WriteFile(cache, append(hdr, out...), 0o644)
	return decodeFP(out)
}

func decodeFP(b []byte) []uint32 {
	out := make([]uint32, len(b)/4)
	for i := range out {
		out[i] = binary.LittleEndian.Uint32(b[i*4:])
	}
	return out
}

// sharedRun finds the longest stretch of near-identical audio between two
// fingerprints at any alignment, and returns where it sits in a (and b).
func sharedRun(a, b []uint32) (inA, inB *Span) {
	// Candidate alignments from exact-matching items (an inverted index), so
	// we only score shifts that can matter instead of all n*m.
	idx := map[uint32][]int32{}
	for j, v := range b {
		idx[v] = append(idx[v], int32(j))
	}
	votes := map[int]int{}
	for i, v := range a {
		for _, j := range idx[v] {
			votes[i-int(j)]++
		}
	}
	type cand struct{ shift, n int }
	var cands []cand
	for s, n := range votes {
		if n >= 3 {
			cands = append(cands, cand{s, n})
		}
	}
	sort.Slice(cands, func(x, y int) bool { return cands[x].n > cands[y].n })
	if len(cands) > 40 {
		cands = cands[:40]
	}
	bestLen, bestStart, bestShift := 0, 0, 0
	for _, c := range cands {
		for d := -2; d <= 2; d++ { // neighbouring shifts too: the frame grid rarely lines up exactly
			shift := c.shift + d
			runStart, last, gap := -1, -1, 0
			for i := max(0, shift); i < len(a) && i-shift < len(b); i++ {
				if bits.OnesCount32(a[i]^b[i-shift]) <= fpMaxBits {
					if runStart < 0 {
						runStart = i
					}
					last, gap = i, 0
					continue
				}
				if runStart >= 0 {
					gap++
					if gap > fpMaxGap {
						if l := last - runStart; l > bestLen {
							bestLen, bestStart, bestShift = l, runStart, shift
						}
						runStart, gap = -1, 0
					}
				}
			}
			if runStart >= 0 {
				if l := last - runStart; l > bestLen {
					bestLen, bestStart, bestShift = l, runStart, shift
				}
			}
		}
	}
	secs := float64(bestLen) * fpItem
	if secs < introMin || secs > introMax {
		return nil, nil
	}
	start := float64(bestStart) * fpItem
	if start > introScanSecs-introMin {
		return nil, nil
	}
	// a cold open shorter than ~2 s is noise: snap the intro to 0
	if start < 2 {
		start = 0
	}
	inA = &Span{round1(start), round1(start + secs)}
	inB = &Span{round1(max(0, float64(bestStart-bestShift)*fpItem)), round1(float64(bestStart-bestShift)*fpItem + secs)}
	return inA, inB
}

// chapterCredits finds an end-credits chapter (starts the Up Next prompt early).
func chapterCredits(chs []Chapter) *Span {
	var runtime float64
	for _, c := range chs {
		if c.End > runtime {
			runtime = c.End
		}
	}
	for _, c := range chs {
		t := strings.ToLower(c.Title)
		if strings.Contains(t, "opening") {
			continue
		}
		if runtime > 0 && c.Start < runtime*0.25 {
			continue
		}
		if strings.Contains(t, "credits") || t == "ed" || strings.Contains(t, "ending") {
			return &Span{c.Start, c.End}
		}
	}
	return nil
}

// chapterIntro uses an "Intro"/"Opening" chapter when the file has one
// (better than any guess).
func chapterIntro(chs []Chapter) *Span {
	for _, c := range chs {
		t := strings.ToLower(c.Title)
		if (strings.Contains(t, "intro") || strings.Contains(t, "opening") || t == "op") && c.End-c.Start >= 5 && c.End-c.Start <= 240 {
			return &Span{c.Start, c.End}
		}
	}
	return nil
}
