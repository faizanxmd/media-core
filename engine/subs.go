package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
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
	subsMu   sync.Mutex
	subsBusy = map[string]bool{}
	subsFill sync.Mutex // one background pass at a time
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
		go extractSubs(it.file, false)
		return nil, true
	}
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		f := strings.SplitN(line, "\t", 5)
		if len(f) < 5 {
			continue
		}
		i, _ := strconv.Atoi(f[0])
		ready = append(ready, embSub{Index: i, Kind: f[1], Lang: f[2], Forced: f[3] == "1", Title: f[4]})
	}
	return ready, false
}

// extractSubs reads one file's subtitles out. idle: at the lowest disk priority (the background pass).
func extractSubs(file string, idle bool) {
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
	dir := subsDir(file)
	if exists(filepath.Join(dir, "list")) || exists(filepath.Join(dir, "failed")) {
		return
	}
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()
	streams, err := probeStreams(ctx, file)
	if err != nil {
		return
	}
	tmp := dir + ".part"
	os.RemoveAll(tmp)
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		return
	}
	defer os.RemoveAll(tmp)
	args := []string{"-nostdin", "-loglevel", "error", "-nostats", "-y", "-i", file}
	var subs []embSub
	for _, s := range streams {
		if s.Type != "subtitle" {
			continue
		}
		e := embSub{Index: s.Index, Lang: strings.ToLower(s.Tags.Language), Title: strings.Join(strings.Fields(s.Tags.Title), " "),
			Forced: s.Disp.Forced == 1 || strings.Contains(strings.ToLower(s.Tags.Title), "forced")}
		switch {
		case textSubs[s.Codec]:
			e.Kind = "vtt"
			args = append(args, "-map", "0:"+itoa(s.Index), "-c:s", "webvtt", "-f", "webvtt", filepath.Join(tmp, fmt.Sprintf("e%d.vtt", s.Index)))
		case s.Codec == "hdmv_pgs_subtitle":
			e.Kind = "sup"
			args = append(args, "-map", "0:"+itoa(s.Index), "-c:s", "copy", "-f", "sup", filepath.Join(tmp, fmt.Sprintf("e%d.sup", s.Index)))
		default:
			continue // VobSub and the like: no way to show them in a browser
		}
		subs = append(subs, e)
	}
	fail := func(why string) {
		log.Printf("subtitles: %s: %s", rel(file), why)
		os.MkdirAll(dir, 0o755)
		os.WriteFile(filepath.Join(dir, "failed"), []byte(why), 0o644)
	}
	if len(subs) == 0 {
		os.MkdirAll(tmp, 0o755)
	} else {
		name, full := "ffmpeg", args
		if idle {
			name, full = "ionice", append([]string{"-c3", "nice", "-n", "15", "ffmpeg"}, args...)
		}
		if out, err := exec.CommandContext(ctx, name, full...).CombinedOutput(); err != nil {
			fail(strings.TrimSpace(lastLine(string(out)) + " " + err.Error()))
			return
		}
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
	if !subsFill.TryLock() {
		return
	}
	defer subsFill.Unlock()
	var todo []*Item
	keep := map[string]bool{}
	for _, it := range items {
		if !hasEmbeddedSubs(it) {
			continue
		}
		dir := subsDir(it.file)
		keep[filepath.Base(dir)] = true
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
	if len(items) == 0 {
		return // library not mounted? keep everything
	}
	ents, _ := os.ReadDir(filepath.Join(DataDir, "subs"))
	for _, e := range ents {
		if name := strings.TrimSuffix(e.Name(), ".part"); !keep[name] {
			os.RemoveAll(filepath.Join(DataDir, "subs", e.Name()))
		}
	}
}
