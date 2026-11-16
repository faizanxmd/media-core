package main

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Subtitles that sit inside a video file (an MKV's, mostly), for browsers. A
// browser can't read them there, and the file is never rewritten to get them
// out, so each file is read through once and its subtitles kept in
// DataDir/subs: text as WebVTT, pictures (PGS) as .sup, which the web player
// draws itself. The TV and phone read them from the file as before.
//
// They're scattered through the whole file, so the read takes minutes on
// mint's disk. It happens in the background after a file turns up, and at once
// when someone opens the title in a browser before that.

type embSub struct {
	Index  int // the stream's index in the file
	Lang   string
	Title  string
	Forced bool
	Kind   string // vtt | sup
}

var (
	subsMu        sync.Mutex
	subsBusy      = map[string]bool{}
	subsQueue     []string
	subsQueued    = map[string]bool{}
	subsActive    string
	subsWorking   bool
	subsFill      sync.Mutex // one background pass at a time
	subsRun       sync.Mutex // one ffmpeg extraction at a time across worker and fillSubs
	subsLatest    map[string]*Item
	subsExtracted = map[string]bool{}

	subsRunFFmpeg = runSubsFFmpeg
	subsProbe     = probeStreams
)

// subsDir: where a file's subtitles are kept. The file's change time is part
// of the name, so a replaced file starts again.
func subsDir(file string) string {
	_, mtime, _ := fileKey(file)
	return filepath.Join(DataDir, "subs", pathID(rel(file))+"-"+strconv.FormatInt(mtime, 36))
}

func hasEmbeddedSubs(it *Item) bool {
	return it != nil && it.file != "" && it.Info != nil && len(it.Info.Subs) > 0
}

// embeddedSubs lists a file's subtitles that are ready, and whether more are
// still being read out. It starts the read when nothing has been kept yet.
func embeddedSubs(it *Item) (ready []embSub, pending bool) {
	if !hasEmbeddedSubs(it) {
		return nil, false
	}
	dir := subsDir(it.file)
	b, err := os.ReadFile(filepath.Join(dir, "list"))
	if err != nil {
		if exists(filepath.Join(dir, "failed")) {
			return nil, false
		}
		enqueueSubs(it.file)
		return nil, true
	}
	for _, line := range strings.Split(strings.TrimRight(string(b), "\r\n"), "\n") {
		if line == "" {
			continue
		}
		f := strings.SplitN(line, "\t", 5)
		if len(f) < 5 {
			continue
		}
		i, _ := strconv.Atoi(f[0])
		ready = append(ready, embSub{Index: i, Kind: f[1], Lang: f[2], Forced: f[3] == "1", Title: f[4]})
	}
	return ready, false
}

func enqueueSubs(file string) {
	subsMu.Lock()
	defer subsMu.Unlock()
	if subsQueued[file] || subsBusy[file] || subsActive == file {
		return
	}
	subsQueued[file] = true
	subsQueue = append(subsQueue, file)
	if !subsWorking {
		subsWorking = true
		go subsWorker()
	}
}

func subsWorker() {
	for {
		subsMu.Lock()
		subsActive = ""
		if len(subsQueue) == 0 {
			subsWorking = false
			subsMu.Unlock()
			return
		}
		file := subsQueue[0]
		subsQueue = subsQueue[1:]
		delete(subsQueued, file)
		subsActive = file
		subsMu.Unlock()
		extractSubs(file, true)
	}
}

func runSubsFFmpeg(args []string) (string, error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := lowPrio(ctx, "ffmpeg", args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Start(); err != nil {
		return "", err
	}
	stop := make(chan struct{})
	go pauseWithTimeout(cmd.Process.Pid, stop, 2*time.Hour, cancel)
	err := cmd.Wait()
	close(stop)
	return out.String(), err
}

// extractSubs reads one file's subtitles out at low CPU/IO priority while pausing for playback.
func extractSubs(file string, _ bool) {
	subsMu.Lock()
	if subsBusy[file] {
		subsMu.Unlock()
		return
	}
	subsBusy[file] = true
	subsMu.Unlock()
	defer func() {
		subsMu.Lock()
		delete(subsBusy, file)
		subsMu.Unlock()
	}()
	subsRun.Lock()
	defer subsRun.Unlock()

	size0, mtime0, ok := fileKey(file)
	if !ok {
		return
	}
	dir := subsDir(file)
	if exists(filepath.Join(dir, "list")) || exists(filepath.Join(dir, "failed")) {
		return
	}
	start := time.Now()
	probeCtx, cancelProbe := context.WithTimeout(context.Background(), 2*time.Minute)
	streams, err := subsProbe(probeCtx, file)
	cancelProbe()
	if err != nil {
		return
	}
	tmp := dir + ".part"
	os.RemoveAll(tmp)
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		return
	}
	defer os.RemoveAll(tmp)

	type subSpec struct {
		e    embSub
		args []string
		out  string
	}
	var specs []subSpec
	for _, s := range streams {
		if s.Type != "subtitle" {
			continue
		}
		e := embSub{Index: s.Index, Lang: strings.ToLower(s.Tags.Language), Title: strings.Join(strings.Fields(s.Tags.Title), " "),
			Forced: s.Disp.Forced == 1 || strings.Contains(strings.ToLower(s.Tags.Title), "forced")}
		var sa []string
		var out string
		switch {
		case textSubs[s.Codec]:
			e.Kind = "vtt"
			out = filepath.Join(tmp, fmt.Sprintf("e%d.vtt", s.Index))
			sa = []string{"-map", "0:" + itoa(s.Index), "-c:s", "webvtt", "-f", "webvtt", out}
		case s.Codec == "hdmv_pgs_subtitle":
			e.Kind = "sup"
			out = filepath.Join(tmp, fmt.Sprintf("e%d.sup", s.Index))
			sa = []string{"-map", "0:" + itoa(s.Index), "-c:s", "copy", "-f", "sup", out}
		default:
			continue // VobSub and the like: no way to show them in a browser
		}
		specs = append(specs, subSpec{e: e, args: sa, out: out})
	}

	fail := func(why string) {
		log.Printf("subtitles: %s: %s", rel(file), why)
		fi, err := os.Stat(file)
		if err != nil || time.Since(fi.ModTime()) < thumbSettle || fi.Size() != size0 || fi.ModTime().Unix() != mtime0 {
			return // file gone, changed, or still settling: don't write a permanent failed marker
		}
		os.MkdirAll(dir, 0o755)
		os.WriteFile(filepath.Join(dir, "failed"), []byte(why), 0o644)
	}

	var subs []embSub
	if len(specs) == 0 {
		os.MkdirAll(tmp, 0o755)
	} else {
		baseArgs := []string{"-nostdin", "-loglevel", "error", "-nostats", "-y", "-i", file}
		allArgs := append([]string(nil), baseArgs...)
		for _, sp := range specs {
			allArgs = append(allArgs, sp.args...)
		}
		out, err := subsRunFFmpeg(allArgs)
		if err == nil {
			for _, sp := range specs {
				subs = append(subs, sp.e)
			}
		} else if len(specs) > 1 {
			// One broken subtitle stream should not lose the good streams in the same file.
			for _, sp := range specs {
				os.Remove(sp.out)
			}
			for _, sp := range specs {
				oneArgs := append(append([]string(nil), baseArgs...), sp.args...)
				if _, sErr := subsRunFFmpeg(oneArgs); sErr == nil && exists(sp.out) {
					subs = append(subs, sp.e)
				} else {
					os.Remove(sp.out)
					log.Printf("subtitles: %s: skipping stream %d (%v)", rel(file), sp.e.Index, sErr)
				}
			}
		}
		if len(subs) == 0 {
			fail(strings.TrimSpace(lastLine(out) + " " + err.Error()))
			return
		}
	}

	if size1, mtime1, ok := fileKey(file); !ok || size1 != size0 || mtime1 != mtime0 {
		return // file changed while reading
	}

	var list strings.Builder
	for _, e := range subs {
		forced := "0"
		if e.Forced {
			forced = "1"
		}
		fmt.Fprintf(&list, "%d\t%s\t%s\t%s\t%s\n", e.Index, e.Kind, e.Lang, forced, e.Title)
	}
	if err := os.WriteFile(filepath.Join(tmp, "list"), []byte(list.String()), 0o644); err != nil {
		return
	}
	os.RemoveAll(dir)
	if err := os.Rename(tmp, dir); err != nil {
		fail(err.Error())
		return
	}
	subsMu.Lock()
	subsExtracted[filepath.Base(dir)] = true
	subsMu.Unlock()
	log.Printf("subtitles: %s: %d kept for browsers (%s)", rel(file), len(subs), time.Since(start).Round(time.Second))
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return lines[len(lines)-1]
}

// fillSubs reads the subtitles out of every file that still needs it, newest
// first, one at a time, and only while nothing is playing. It also clears out
// what's kept for files that are gone.
func fillSubs(items map[string]*Item) {
	subsMu.Lock()
	subsLatest = items
	subsMu.Unlock()
	if !subsFill.TryLock() {
		return
	}
	defer subsFill.Unlock()
	fillSubsLocked(items)
}

func fillSubsLocked(items map[string]*Item) {
	subsMu.Lock()
	subsExtracted = map[string]bool{}
	subsMu.Unlock()
	var todo []*Item
	for _, it := range items {
		if !hasEmbeddedSubs(it) {
			continue
		}
		dir := subsDir(it.file)
		if !exists(filepath.Join(dir, "list")) && !exists(filepath.Join(dir, "failed")) {
			todo = append(todo, it)
		}
	}
	sort.Slice(todo, func(a, b int) bool { return todo[a].Added > todo[b].Added })
	for _, it := range todo {
		if fi, err := os.Stat(it.file); err != nil || time.Since(fi.ModTime()) < thumbSettle {
			continue // gone, or still being written
		}
		waitForNoStreams()
		extractSubs(it.file, true)
	}
	subsMu.Lock()
	latest := subsLatest
	keep := make(map[string]bool, len(subsExtracted)+len(subsQueued)+len(subsBusy))
	for k := range subsExtracted {
		keep[k] = true
	}
	for f := range subsQueued {
		keep[filepath.Base(subsDir(f))] = true
	}
	for f := range subsBusy {
		keep[filepath.Base(subsDir(f))] = true
	}
	subsMu.Unlock()
	if len(latest) == 0 {
		return // library not mounted? keep everything
	}
	for _, it := range latest {
		if hasEmbeddedSubs(it) {
			keep[filepath.Base(subsDir(it.file))] = true
		}
	}
	ents, _ := os.ReadDir(filepath.Join(DataDir, "subs"))
	for _, e := range ents {
		if name := strings.TrimSuffix(e.Name(), ".part"); !keep[name] {
			os.RemoveAll(filepath.Join(DataDir, "subs", e.Name()))
		}
	}
}
