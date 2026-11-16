package main

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// Scrub thumbnails: one small picture every 10 s of each video, so holding
// Left/Right on the remote shows where you are. One file per video (like
// Roku's BIF): the app downloads it once and reads pictures out of it locally,
// so scrubbing never waits on the network. Only keyframes are decoded, at the
// lowest priority, and the job pauses while anything is streaming.
//
// File: "MCT1", uint32 seconds between pictures, uint32 count, count+1 uint64
// offsets from the start of the file, then the JPEGs. All big-endian.

const (
	thumbEvery   = 10
	thumbWidth   = 480
	thumbVersion = 1
	thumbSettle  = 10 * time.Minute // a file still being written waits for the next scan
)

// Thumbs makes and finds the thumbnail files.
type Thumbs struct {
	dir    string
	run    sync.Mutex
	mu     sync.Mutex
	counts map[string]int // thumbnail file -> pictures in it
	failed map[string]bool
}

func NewThumbs() *Thumbs {
	t := &Thumbs{dir: filepath.Join(DataDir, "thumbs"), counts: map[string]int{}, failed: map[string]bool{}}
	os.MkdirAll(t.dir, 0o755)
	t.cleanTmp()
	return t
}

func (t *Thumbs) cleanTmp() {
	ents, _ := os.ReadDir(t.dir)
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), ".tmp-") {
			os.RemoveAll(filepath.Join(t.dir, e.Name()))
		}
	}
}

// path is keyed on the video's size and date, so a replaced file gets new pictures.
func (t *Thumbs) path(video string) (string, bool) {
	size, mtime, ok := fileKey(video)
	if !ok {
		return "", false
	}
	return filepath.Join(t.dir, fmt.Sprintf("%s-%d-%d-v%d.bin", pathID(rel(video)), size, mtime, thumbVersion)), true
}

// Count is how many pictures a video has (0: none yet).
func (t *Thumbs) Count(video string) int {
	p, ok := t.path(video)
	if !ok {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if n, ok := t.counts[p]; ok {
		return n
	}
	f, err := os.Open(p)
	if err != nil {
		return 0
	}
	defer f.Close()
	hdr := make([]byte, 12)
	if _, err := io.ReadFull(f, hdr); err != nil || string(hdr[:4]) != "MCT1" {
		return 0
	}
	n := int(binary.BigEndian.Uint32(hdr[8:]))
	t.counts[p] = n
	return n
}

// File is the thumbnail file of a video, if it has one.
func (t *Thumbs) File(video string) (string, bool) {
	if t.Count(video) == 0 {
		return "", false
	}
	return t.path(video)
}

func (t *Thumbs) due(it *Item) bool {
	if it.file == "" || it.Info == nil || it.Info.Dur <= 0 || t.Count(it.file) > 0 {
		return false
	}
	p, ok := t.path(it.file)
	if !ok {
		return false
	}
	fi, err := os.Stat(it.file)
	if err != nil || time.Since(fi.ModTime()) < thumbSettle {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return !t.failed[p]
}

// Pending reports whether any video still needs pictures.
func (t *Thumbs) Pending(items map[string]*Item) bool {
	for _, it := range items {
		if t.due(it) {
			return true
		}
	}
	return false
}

// Generate makes what's missing, newest video first, one at a time.
// onDone runs after each finished video.
func (t *Thumbs) Generate(items map[string]*Item, onDone func()) {
	if !t.run.TryLock() {
		return
	}
	defer t.run.Unlock()
	t.cleanTmp()
	var todo []*Item
	want := map[string]bool{}
	wantPaths := map[string]bool{}
	for _, it := range items {
		if it.file == "" {
			continue
		}
		if p, ok := t.path(it.file); ok {
			want[filepath.Base(p)] = true
			wantPaths[p] = true
		}
		if t.due(it) {
			todo = append(todo, it)
		}
	}
	sort.Slice(todo, func(a, b int) bool { return todo[a].Added > todo[b].Added })
	for _, it := range todo {
		start := time.Now()
		if err := t.make(it); err != nil {
			log.Printf("thumbs: %s: %v", rel(it.file), err)
			if p, ok := t.path(it.file); ok {
				t.mu.Lock()
				t.failed[p] = true
				t.mu.Unlock()
			}
			continue
		}
		log.Printf("thumbs: %s: %d pictures (%s)", rel(it.file), t.Count(it.file), time.Since(start).Round(time.Second))
		onDone()
	}
	if len(want) == 0 {
		return // library not mounted? keep everything
	}
	ents, _ := os.ReadDir(t.dir)
	for _, e := range ents { // pictures of videos that are gone or were replaced
		if strings.HasSuffix(e.Name(), ".bin") && !want[e.Name()] {
			os.Remove(filepath.Join(t.dir, e.Name()))
		}
	}
	t.mu.Lock()
	for p := range t.counts {
		if !wantPaths[p] {
			delete(t.counts, p)
		}
	}
	for p := range t.failed {
		if !wantPaths[p] {
			delete(t.failed, p)
		}
	}
	t.mu.Unlock()
}

func (t *Thumbs) make(it *Item) error {
	waitForNoStreams()
	p, ok := t.path(it.file)
	if !ok {
		return fmt.Errorf("file is gone")
	}
	tmp, err := os.MkdirTemp(t.dir, ".tmp-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	vf := fmt.Sprintf("fps=1/%d,scale=%d:-2:flags=bilinear", thumbEvery, thumbWidth)
	if it.Info.HDR != "" { // PQ/HLG pictures look washed out unless mapped to normal range
		vf += ",zscale=t=linear:npl=100,format=gbrpf32le,zscale=p=bt709,tonemap=hable:desat=0,zscale=t=bt709:m=bt709:r=tv,format=yuv420p"
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := exec.CommandContext(ctx, "ionice", "-c3", "nice", "-n", "19", "ffmpeg", "-nostdin", "-v", "error",
		"-skip_frame", "nokey", "-i", it.file, "-map", "0:v:0", "-an", "-sn", "-dn", "-fps_mode", "passthrough",
		"-vf", vf, "-q:v", "5", filepath.Join(tmp, "%06d.jpg"))
	var errOut strings.Builder
	cmd.Stderr = &errOut
	if err := cmd.Start(); err != nil {
		return err
	}
	stop := make(chan struct{})
	defer close(stop)
	go pauseWithTimeout(cmd.Process.Pid, stop, 3*time.Hour, cancel)
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("%v: %s", err, strings.TrimSpace(errOut.String()))
	}
	frames, _ := filepath.Glob(filepath.Join(tmp, "*.jpg"))
	if len(frames) == 0 {
		return fmt.Errorf("no pictures came out")
	}
	sort.Strings(frames)
	return packThumbs(p, tmp, frames)
}

// packThumbs joins the pictures into the one file the app downloads.
func packThumbs(dest, tmp string, frames []string) error {
	out := filepath.Join(tmp, "pack")
	f, err := os.Create(out)
	if err != nil {
		return err
	}
	defer f.Close()
	head := make([]byte, 12+8*(len(frames)+1))
	copy(head, "MCT1")
	binary.BigEndian.PutUint32(head[4:], thumbEvery)
	binary.BigEndian.PutUint32(head[8:], uint32(len(frames)))
	if _, err := f.Write(head); err != nil {
		return err
	}
	off := uint64(len(head))
	for i, fr := range frames {
		binary.BigEndian.PutUint64(head[12+8*i:], off)
		b, err := os.ReadFile(fr)
		if err != nil {
			return err
		}
		if _, err := f.Write(b); err != nil {
			return err
		}
		off += uint64(len(b))
	}
	binary.BigEndian.PutUint64(head[12+8*len(frames):], off)
	if _, err := f.WriteAt(head, 0); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(out, dest)
}

func waitForNoStreams() {
	for playingNow() > 0 {
		time.Sleep(10 * time.Second)
	}
}

var pauseTick atomic.Int64 // ns between checks; a var so tests can shorten it

func init() { pauseTick.Store(int64(3 * time.Second)) }

// pauseWithTimeout freezes pid while streaming and cancels after activeLimit of
// unpaused runtime (when > 0), so paused time doesn't count toward timeouts.
func pauseWithTimeout(pid int, stop <-chan struct{}, activeLimit time.Duration, cancel context.CancelFunc) {
	frozen := false
	var active time.Duration
	last := time.Now()
	tick := time.NewTicker(time.Duration(pauseTick.Load()))
	defer tick.Stop()
	for {
		select {
		case <-stop:
			if frozen {
				syscall.Kill(pid, syscall.SIGCONT)
			}
			return
		case <-tick.C:
			now := time.Now()
			if !frozen {
				active += now.Sub(last)
			}
			last = now
			if activeLimit > 0 && active >= activeLimit && cancel != nil {
				if frozen {
					syscall.Kill(pid, syscall.SIGCONT)
					frozen = false
				}
				cancel()
				return
			}
			busy := playingNow() > 0
			if busy != frozen {
				sig := syscall.SIGCONT
				if busy {
					sig = syscall.SIGSTOP
				}
				syscall.Kill(pid, sig)
				frozen = busy
			}
		}
	}
}
