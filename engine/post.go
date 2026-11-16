package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

var (
	unpackTimeout   = 2 * time.Hour
	convertTimeout  = 6 * time.Hour
	probeCmdTimeout = 30 * time.Second
)

// Heavy tools run at low CPU and IO priority so playback from the same
// HDD doesn't stutter.
func lowPrio(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "ionice", append([]string{"-c2", "-n7", "nice", "-n", "10", name}, args...)...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		return nil
	}
	cmd.WaitDelay = 5 * time.Second
	return cmd
}

func (e *Engine) startPost(id string) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	e.mu.Lock()
	if e.posts == nil {
		e.posts = map[string]postEntry{}
	}
	if _, running := e.posts[id]; running {
		e.mu.Unlock()
		cancel()
		close(done)
		return
	}
	e.posts[id] = postEntry{cancel: cancel, done: done}
	e.mu.Unlock()
	go func() {
		defer func() {
			e.mu.Lock()
			delete(e.posts, id)
			e.mu.Unlock()
			cancel()
			close(done)
		}()
		if err := e.waitForFilm(ctx, id); err != nil { // a Light copy waits for its film, outside the queue
			if ctx.Err() == nil {
				e.fail(id, err.Error())
			}
			return
		}
		select { // wait for our turn
		case e.postSem <- struct{}{}:
		case <-ctx.Done():
			return
		}
		defer func() { <-e.postSem }()
		if err := e.post(ctx, id); err != nil && ctx.Err() == nil {
			e.fail(id, err.Error())
		}
	}()
}

func (e *Engine) cancelPost(id string) <-chan struct{} {
	e.mu.Lock()
	ent, ok := e.posts[id]
	e.mu.Unlock()
	if !ok {
		return nil
	}
	ent.cancel()
	return ent.done
}

type jobCtx struct {
	e  *Engine
	id string
}

func (c jobCtx) log(msg string) {
	c.e.store.Update(c.id, true, func(j *Job) { logLine(j, msg) })
}

func (c jobCtx) set(persist bool, fn func(*Job)) Job {
	j, _ := c.e.store.Update(c.id, persist, fn)
	return j
}

func (c jobCtx) phase(state, phase string) {
	c.set(true, func(j *Job) { j.State, j.Phase, j.Progress = state, phase, 0 })
}

// progress updates the bar at most every 2 s.
func (c jobCtx) progressFn() func(int) {
	last := time.Time{}
	return func(p int) {
		if time.Since(last) > 2*time.Second {
			last = time.Now()
			c.set(false, func(j *Job) { j.Progress = min(max(p, 0), 100) })
		}
	}
}

func (e *Engine) post(ctx context.Context, id string) error {
	c := jobCtx{e, id}
	j, ok := e.store.Get(id)
	if !ok {
		return nil
	}
	dlDir, fn, final, err := jobPaths(j.Plan, j.ID)
	if err != nil {
		return err
	}
	videos := append([]string(nil), j.Files...)
	placedNow := false
	if fn != "" {
		if len(videos) == 0 {
			videos = []string{filepath.Join(dlDir, fn)}
		}
	} else if !j.Placed {
		legacyDir := filepath.Dir(dlDir)
		if filepath.Base(legacyDir) == ".incoming" {
			if name := clean(j.Filename); name != "" {
				dst := filepath.Join(dlDir, name)
				srcLegacy := filepath.Join(legacyDir, name)
				if !exists(dst) && exists(srcLegacy) {
					os.MkdirAll(dlDir, 0o2775)
					if err := os.Rename(srcLegacy, dst); err != nil {
						return err
					}
				}
			}
		}
		src := dlDir
		if j.Plan.Archive {
			if src, err = unpack(ctx, c, filepath.Join(dlDir, clean(j.Filename))); err != nil {
				return err
			}
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		c.phase(Processing, "Placing files")
		if videos, err = placeCtx(ctx, c, j.Plan, final, src); err != nil {
			return err
		}
		// placeCtx moved everything into the library; commit Placed and drop
		// .incoming before checking ctx so disk and job state never disagree.
		placedNow = true
		c.set(true, func(j *Job) {
			if !j.Plan.Light {
				j.Placed = true
			}
			j.Files = append([]string(nil), videos...) // videos changes below; the store's copy must not
		})
		os.RemoveAll(dlDir)
		if filepath.Base(legacyDir) == ".incoming" {
			os.Remove(legacyDir) // remove shared .incoming only if now empty
		}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if !placedNow {
		c.set(true, func(j *Job) {
			if !j.Plan.Light {
				j.Placed = true
			}
			j.Files = append([]string(nil), videos...) // videos changes below; the store's copy must not
		})
	}

	for i, v := range videos {
		switch prof := dvProfile(ctx, v); {
		case prof == 7:
			v2, err := dvConvert(ctx, c, v)
			if err != nil {
				return err
			}
			videos[i] = v2
			c.set(true, func(j *Job) { j.Files = append([]string(nil), videos...) })
		case prof > 0:
			c.log(fmt.Sprintf("Dolby Vision profile %d: plays in DV on the TV", prof))
		}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	switch {
	case j.Plan.Light:
		if err := e.acceptLight(ctx, c, j, videos); err != nil {
			return err
		}
	case j.Plan.MP4 || j.Plan.Kind == "convert":
		for i, v := range videos {
			m := probeMediaCtx(ctx, v)
			if !canMP4(m) {
				if j.Plan.Kind == "convert" {
					return errors.New("an MP4 wouldn't make this play in browsers (4K, HDR or an unusual video format)")
				}
				c.log(filepath.Base(v) + ": kept as it is (an MP4 wouldn't help it play in browsers)")
				continue
			}
			mctx, mcancel := context.WithTimeout(ctx, convertTimeout)
			v2, err := e.toMP4(mctx, c, v)
			mcancel()
			if err != nil {
				return err
			}
			videos[i] = v2
			c.set(true, func(j *Job) { j.Files = append([]string(nil), videos...) })
		}
	}

	if e.onLibrary != nil {
		e.onLibrary()
	}
	where := final
	if fn == "" && len(videos) > 0 {
		where = filepath.Dir(videos[0])
	} else if len(videos) == 1 {
		where = videos[0] // the MP4 step may have renamed it
	}
	c.set(true, func(j *Job) {
		j.State, j.Phase, j.Progress, j.Finished = Done, "", 100, now()
		logLine(j, "Done: in the library under "+rel(where))
	})
	return nil
}

// ---------- archives ----------

var (
	rePct     = regexp.MustCompile(`(\d{1,3})%`)
	reSubLang = regexp.MustCompile(`[._ -]([a-zA-Z]{2,3}|forced|sdh)\.[a-z]{3}$`)
)

// scanChunks splits r on any byte in delims (capping stored line bytes at
// maxLine) and keeps draining until EOF so an overlong line never blocks the child.
func scanChunks(r io.Reader, delims string, maxLine int, fn func([]byte)) {
	var cur []byte
	buf := make([]byte, 32<<10)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			data := buf[:n]
			for len(data) > 0 {
				i := bytes.IndexAny(data, delims)
				if i < 0 {
					if len(cur) < maxLine {
						take := min(len(data), maxLine-len(cur))
						cur = append(cur, data[:take]...)
					}
					break
				}
				if len(cur) < maxLine {
					take := min(i, maxLine-len(cur))
					cur = append(cur, data[:take]...)
				}
				fn(cur)
				cur = cur[:0]
				data = data[i+1:]
			}
		}
		if err != nil {
			if len(cur) > 0 {
				fn(cur)
			}
			return
		}
	}
}

// scanProgress feeds the last "NN%" seen in r (split on \r, \n and \b) to fn.
func scanProgress(r io.Reader, re *regexp.Regexp, fn func(int), tail *bytes.Buffer) {
	scanChunks(r, "\r\n\b", 64<<10, func(line []byte) {
		if m := re.FindSubmatch(line); m != nil {
			n, _ := strconv.Atoi(string(m[1]))
			fn(n)
		} else if tail != nil && len(bytes.TrimSpace(line)) > 0 {
			tail.Write(append(bytes.TrimSpace(line), '\n'))
			if tail.Len() > 4096 {
				tail.Next(tail.Len() - 4096)
			}
		}
	})
}

func unpack(ctx context.Context, c jobCtx, archive string) (string, error) {
	uctx, cancel := context.WithTimeout(ctx, unpackTimeout)
	defer cancel()
	x := filepath.Join(filepath.Dir(archive), "x")
	c.phase(Processing, "Unpacking")
	c.log("Unpacking " + filepath.Base(archive))
	// One pass: 7z checks CRCs while extracting, so a separate `7z t` would
	// only read the whole archive from the HDD twice.
	cmd := lowPrio(uctx, "7z", "x", "-y", "-p", "-bsp1", "-bso0", "-bse1", "-o"+x, archive)
	out, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	var tail bytes.Buffer
	if err := cmd.Start(); err != nil {
		out.Close()
		return "", err
	}
	scanProgress(out, rePct, c.progressFn(), &tail)
	if err := cmd.Wait(); err != nil {
		if uctx.Err() != nil {
			return "", uctx.Err()
		}
		msg := strings.TrimSpace(tail.String())
		if strings.Contains(msg, "Wrong password") || strings.Contains(msg, "encrypted") {
			msg = "archive is password-protected"
		}
		return "", fmt.Errorf("unpack failed: %s", last(msg, 300))
	}
	return x, nil
}

func last(s string, n int) string {
	if len(s) > n {
		return "…" + s[len(s)-n:]
	}
	return s
}

func filesWith(root string, exts map[string]bool) []string {
	var out []string
	filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && exts[ext(p)] {
			out = append(out, p)
		}
		return nil
	})
	sort.Strings(out)
	return out
}

func size(p string) int64 {
	st, err := os.Stat(p)
	if err != nil {
		return 0
	}
	return st.Size()
}

// isSample skips "sample" clips so they don't take an episode slot.
func isSample(p string) bool {
	return strings.Contains(strings.ToLower(filepath.Base(p)), "sample") && size(p) < 400e6
}

func move(src, dst string) error {
	return moveCtx(context.Background(), src, dst)
}

func moveCtx(ctx context.Context, src, dst string) error {
	if _, err := os.Lstat(dst); err == nil {
		return fmt.Errorf("%s already exists", filepath.Base(dst))
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o2775); err != nil {
		return err
	}
	if err := os.Link(src, dst); err == nil {
		if err := os.Remove(src); err != nil {
			os.Remove(dst)
			return err
		}
		return nil
	} else if errors.Is(err, os.ErrExist) {
		return fmt.Errorf("%s already exists", filepath.Base(dst))
	}
	if _, err := os.Lstat(dst); err == nil {
		return fmt.Errorf("%s already exists", filepath.Base(dst))
	}
	mctx, mcancel := context.WithTimeout(ctx, unpackTimeout)
	defer mcancel()
	cmd := exec.CommandContext(mctx, "mv", "-n", "--", src, dst) // across filesystems
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("move %s: %s", filepath.Base(src), out)
	}
	if _, err := os.Lstat(src); err == nil {
		return fmt.Errorf("%s already exists", filepath.Base(dst))
	}
	return nil
}

func uniqueSubPath(dst string) string {
	if _, err := os.Lstat(dst); err != nil {
		return dst
	}
	e := filepath.Ext(dst)
	stem := strings.TrimSuffix(dst, e)
	for i := 2; ; i++ {
		cand := fmt.Sprintf("%s.%d%s", stem, i, e)
		if _, err := os.Lstat(cand); err != nil {
			return cand
		}
	}
}

type movePair struct{ src, dst string }

type placer struct {
	id          string
	media       string
	roots       map[string]bool
	moved       []movePair
	createdDirs []string
	createdSet  map[string]bool
}

func newPlacer(id string) *placer {
	media := filepath.Clean(Media)
	return &placer{
		id:    id,
		media: media,
		roots: map[string]bool{
			media:                            true,
			filepath.Join(media, "Movies"):   true,
			filepath.Join(media, "TV Shows"): true,
			filepath.Join(media, "Music"):    true,
		},
		createdSet: map[string]bool{},
	}
}

func (pl *placer) trackNewDirs(dir string) {
	for d := filepath.Clean(dir); !pl.roots[d] && strings.HasPrefix(d, pl.media+"/"); d = filepath.Dir(d) {
		if pl.createdSet[d] {
			break
		}
		if _, err := os.Lstat(d); os.IsNotExist(err) {
			pl.createdSet[d] = true
			pl.createdDirs = append(pl.createdDirs, d)
		} else {
			break
		}
	}
}

func (pl *placer) rollback() error {
	var firstErr error
	for i := len(pl.moved) - 1; i >= 0; i-- {
		m := pl.moved[i]
		if err := move(m.dst, m.src); err != nil {
			log.Printf("[%s] rollback move %s -> %s: %v", pl.id, m.dst, m.src, err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	sort.Slice(pl.createdDirs, func(i, j int) bool {
		return len(pl.createdDirs[i]) > len(pl.createdDirs[j])
	})
	for _, d := range pl.createdDirs {
		os.Remove(d)
	}
	return firstErr
}

func (pl *placer) moveOne(ctx context.Context, from, to string) error {
	fail := func(err error) error {
		if rbErr := pl.rollback(); rbErr != nil {
			return fmt.Errorf("%w (rollback failed: %v)", err, rbErr)
		}
		return err
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	pl.trackNewDirs(filepath.Dir(to))
	if err := move(from, to); err != nil {
		return fail(err)
	}
	pl.moved = append(pl.moved, movePair{from, to})
	return nil
}

// place moves unpacked/downloaded files to their library names; returns video paths.
func place(c jobCtx, p Plan, final, src string) ([]string, error) {
	return placeCtx(context.Background(), c, p, final, src)
}

// placeCtx places files all-or-nothing: on error or cancellation mid-transfer,
// every file moved in this call is moved back to its source and any empty
// library folders created for it are removed.
func placeCtx(ctx context.Context, c jobCtx, p Plan, final, src string) ([]string, error) {
	var videos []string
	for _, v := range filesWith(src, VideoExt) {
		if !isSample(v) {
			videos = append(videos, v)
		}
	}
	pl := newPlacer(c.id)

	var placed []string
	switch p.Kind {
	case "movie":
		if len(videos) == 0 {
			return nil, errors.New("no video file found")
		}
		// A batch of episodes named like a movie ("Show S01P2"): stop before
		// keeping one and deleting the rest, so it can be queued as a show.
		if eps := 0; len(videos) > 1 {
			for _, v := range videos {
				if _, e := epNumber(filepath.Base(v)); e > 0 {
					eps++
				}
			}
			if eps > 1 {
				return nil, fmt.Errorf("this holds %d episodes, not one movie: queue it as a TV show", eps)
			}
		}
		main := videos[0]
		for _, v := range videos {
			if size(v) > size(main) {
				main = v
			}
		}
		base := filepath.Base(final)
		dst := filepath.Join(final, base+"."+ext(main))
		if err := pl.moveOne(ctx, main, dst); err != nil {
			return nil, err
		}
		placed = append(placed, dst)
		for _, sub := range filesWith(src, SubExt) {
			_, tail, _ := strings.Cut(filepath.Base(sub), ".")
			subDst := uniqueSubPath(filepath.Join(final, base+"."+tail))
			if err := pl.moveOne(ctx, sub, subDst); err != nil {
				return nil, err
			}
		}
	case "tv":
		show := clean(p.Title)
		def, _ := atoi(p.Season, 1)
		var videoMoves []movePair
		seen := map[string]bool{}
		for _, v := range videos {
			s, e, named := epParts(filepath.Base(v))
			if !named {
				s = def
			}
			sdir := filepath.Join(Media, "TV Shows", show, fmt.Sprintf("Season %02d", s))
			name := filepath.Base(v)
			if e > 0 {
				name = fmt.Sprintf("%s - S%02dE%02d.%s", show, s, e, ext(v))
			}
			dst := filepath.Join(sdir, name)
			if _, err := os.Lstat(dst); err == nil || seen[dst] {
				return nil, fmt.Errorf("%s already exists", name)
			}
			seen[dst] = true
			videoMoves = append(videoMoves, movePair{v, dst})
		}
		if len(videoMoves) == 0 {
			return nil, errors.New("no video files found")
		}
		for _, m := range videoMoves {
			if err := pl.moveOne(ctx, m.src, m.dst); err != nil {
				return nil, err
			}
			placed = append(placed, m.dst)
		}
		// subtitles that name an episode: "Show - S01E02.en.srt"
		for _, sub := range filesWith(src, SubExt) {
			s, e, named := epParts(filepath.Base(sub))
			if e == 0 {
				continue
			}
			if !named {
				s = def
			}
			suffix := "." + ext(sub)
			if lang := reSubLang.FindStringSubmatch(filepath.Base(sub)); lang != nil {
				suffix = "." + strings.ToLower(lang[1]) + suffix
			}
			subDst := uniqueSubPath(filepath.Join(Media, "TV Shows", show, fmt.Sprintf("Season %02d", s),
				fmt.Sprintf("%s - S%02dE%02d%s", show, s, e, suffix)))
			if err := pl.moveOne(ctx, sub, subDst); err != nil {
				return nil, err
			}
		}
		c.log(fmt.Sprintf("Placed %d episode%s", len(placed), plural(len(placed))))
	case "music":
		audio := filesWith(src, AudioExt)
		if len(audio) == 0 {
			return nil, errors.New("no audio files found")
		}
		albums := map[string][]string{}
		for _, a := range audio {
			albums[filepath.Dir(a)] = append(albums[filepath.Dir(a)], a)
		}
		dirs := make([]string, 0, len(albums))
		for dir := range albums {
			dirs = append(dirs, dir)
		}
		sort.Strings(dirs)
		var musicMoves []movePair
		var albumLogs []string
		seen := map[string]bool{}
		for _, dir := range dirs {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			files := albums[dir]
			tags := audioTagsCtx(ctx, files[0])
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			artist := clean(firstOf(p.Title, tags["album_artist"], tags["artist"], "Unknown Artist"))
			fallbackAlbum := filepath.Base(dir)
			if filepath.Base(filepath.Dir(dir)) == ".incoming" || fallbackAlbum == ".incoming" || fallbackAlbum == "x" {
				fallbackAlbum = baseName(files[0])
			}
			album := clean(firstOf(tags["album"], fallbackAlbum))
			dst := filepath.Join(Media, "Music", artist, album)
			ents, err := os.ReadDir(dir)
			if err != nil {
				return nil, err
			}
			for _, f := range ents {
				low := strings.ToLower(f.Name())
				if f.IsDir() || strings.HasSuffix(low, ".url") || strings.HasSuffix(low, ".lnk") ||
					strings.HasSuffix(low, ".exe") || strings.HasSuffix(low, ".html") || strings.HasSuffix(low, ".htm") {
					continue
				}
				to := filepath.Join(dst, f.Name())
				if _, err := os.Lstat(to); err == nil || seen[to] {
					return nil, fmt.Errorf("%s already exists", f.Name())
				}
				seen[to] = true
				musicMoves = append(musicMoves, movePair{filepath.Join(dir, f.Name()), to})
			}
			albumLogs = append(albumLogs, "Album → Music/"+artist+"/"+album)
		}
		for _, m := range musicMoves {
			if err := pl.moveOne(ctx, m.src, m.dst); err != nil {
				return nil, err
			}
		}
		for _, msg := range albumLogs {
			c.log(msg)
		}
	}
	return placed, nil
}

func firstOf(xs ...string) string {
	for _, x := range xs {
		if strings.TrimSpace(x) != "" {
			return x
		}
	}
	return ""
}

func audioTags(p string) map[string]string {
	return audioTagsCtx(context.Background(), p)
}

func audioTagsCtx(ctx context.Context, p string) map[string]string {
	pctx, cancel := context.WithTimeout(ctx, probeCmdTimeout)
	defer cancel()
	out, _ := exec.CommandContext(pctx, "ffprobe", "-v", "error", "-show_entries", "format_tags", "-of", "json", p).Output()
	var r struct {
		Format struct {
			Tags map[string]string `json:"tags"`
		} `json:"format"`
	}
	json.Unmarshal(out, &r)
	tags := map[string]string{}
	for k, v := range r.Format.Tags {
		tags[strings.ToLower(k)] = v
	}
	return tags
}

// ---------- Dolby Vision ----------

func ffprobe(ctx context.Context, p string, args ...string) string {
	pctx, cancel := context.WithTimeout(ctx, probeCmdTimeout)
	defer cancel()
	out, _ := exec.CommandContext(pctx, "ffprobe", append(append([]string{"-v", "error"}, args...), "-of", "default=nw=1:nk=1", p)...).Output()
	return strings.TrimSpace(string(out))
}

func dvProfile(ctx context.Context, p string) int {
	f := strings.Fields(ffprobe(ctx, p, "-select_streams", "v:0", "-show_entries", "stream_side_data=dv_profile"))
	if len(f) == 0 {
		return 0
	}
	n, _ := strconv.Atoi(f[0])
	return n
}

var reOutTime = regexp.MustCompile(`^out_time_us=(\d+)`)

// dvConvert rewrites Dolby Vision profile 7 as 8.1 (video copied, no re-encode)
// so the Android TV plays it in DV instead of HDR10.
func dvConvert(ctx context.Context, c jobCtx, path string) (string, error) {
	dctx, dcancel := context.WithTimeout(ctx, convertTimeout)
	defer dcancel()
	ctx = dctx

	work := filepath.Join(filepath.Dir(path), ".convert")
	os.RemoveAll(work)
	os.MkdirAll(work, 0o2775)
	defer os.RemoveAll(work)
	hevc, out := filepath.Join(work, "bl_rpu.hevc"), filepath.Join(work, "new.mkv")
	c.phase(Converting, "Dolby Vision 7 → 8.1: extracting video")
	c.log("Dolby Vision profile 7 found: converting to 8.1 so the TV plays it in DV")

	fps := ffprobe(ctx, path, "-select_streams", "v:0", "-show_entries", "stream=r_frame_rate")
	if fps == "" {
		fps = "24000/1001"
	}
	srcDur, _ := strconv.ParseFloat(ffprobe(ctx, path, "-show_entries", "format=duration"), 64)
	durUS := srcDur * 1e6
	origStreams, origStreamsErr := probeStreams(ctx, path)
	origAudio := 0
	for _, s := range origStreams {
		if s.Type == "audio" {
			origAudio++
		}
	}

	ff := lowPrio(ctx, "ffmpeg", "-nostdin", "-loglevel", "error", "-nostats", "-progress", "pipe:2",
		"-i", path, "-map", "0:v:0", "-c:v", "copy", "-bsf:v", "hevc_mp4toannexb", "-f", "hevc", "-")
	dv := lowPrio(ctx, DoviTool, "-m", "2", "convert", "--discard", "-", "-o", hevc)
	pr, pw, err := os.Pipe()
	if err != nil {
		return "", err
	}
	ff.Stdout, dv.Stdin = pw, pr
	var dvErr bytes.Buffer
	dv.Stderr = &dvErr
	ffErrPipe, err := ff.StderrPipe()
	if err != nil {
		pr.Close()
		pw.Close()
		return "", err
	}
	if err := ff.Start(); err != nil {
		pr.Close()
		pw.Close()
		ffErrPipe.Close()
		return "", err
	}
	if err := dv.Start(); err != nil {
		pw.Close()
		pr.Close()
		if ff.Cancel != nil {
			ff.Cancel()
		} else if ff.Process != nil {
			ff.Process.Kill()
		}
		ff.Wait()
		return "", err
	}
	pw.Close()
	pr.Close()
	prog := c.progressFn()
	var ffTail bytes.Buffer
	scanChunks(ffErrPipe, "\r\n", 64<<10, func(line []byte) {
		text := string(line)
		if m := reOutTime.FindStringSubmatch(text); m != nil && durUS > 0 {
			us, _ := strconv.ParseFloat(m[1], 64)
			prog(int(us / durUS * 100))
		} else if !strings.Contains(text, "=") {
			ffTail.WriteString(text + "\n")
		}
	})
	ffErr := ff.Wait()
	if err := dv.Wait(); err != nil || ffErr != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", fmt.Errorf("dovi_tool/ffmpeg failed: %s", last(strings.TrimSpace(dvErr.String()+ffTail.String()), 300))
	}

	c.phase(Converting, "Dolby Vision 7 → 8.1: remuxing with audio and subtitles")
	mk := lowPrio(ctx, "mkvmerge", "--gui-mode", "-o", out, "--default-duration", "0:"+fps+"p",
		"--language", "0:und", hevc, "-D", path)
	mkOut, err := mk.StdoutPipe()
	if err != nil {
		return "", err
	}
	var mkTail bytes.Buffer
	if err := mk.Start(); err != nil {
		mkOut.Close()
		return "", err
	}
	scanProgress(mkOut, regexp.MustCompile(`#GUI#progress (\d+)%`), c.progressFn(), &mkTail)
	err = mk.Wait()
	os.Remove(hevc)
	var ee *exec.ExitError
	if err != nil && !(errors.As(err, &ee) && ee.ExitCode() == 1) { // 1 = warnings only
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", fmt.Errorf("remux failed: %s", last(mkTail.String(), 300))
	}
	if outDur := duration(ctx, out); srcDur <= 0 || outDur <= 0 || math.Abs(outDur-srcDur) > 1.0 {
		return "", fmt.Errorf("verify failed: converted file runs %.1f s, original %.1f s; kept original", outDur, srcDur)
	}
	outStreams, err := probeStreams(ctx, out)
	if err != nil {
		return "", fmt.Errorf("verify failed: %w; kept original", err)
	}
	outVideo, outAudio := 0, 0
	for _, s := range outStreams {
		switch {
		case s.Type == "video" && s.Disp.Pic == 0:
			outVideo++
		case s.Type == "audio":
			outAudio++
		}
	}
	if outVideo == 0 {
		return "", errors.New("verify failed: converted file has no video stream; kept original")
	}
	if origStreamsErr != nil {
		return "", fmt.Errorf("verify failed: original streams unreadable (%w); kept original", origStreamsErr)
	}
	if outAudio != origAudio {
		return "", fmt.Errorf("verify failed: converted file has %d audio stream%s, original %d; kept original", outAudio, plural(outAudio), origAudio)
	}
	if p := dvProfile(ctx, out); p != 8 {
		return "", fmt.Errorf("verify failed: converted file reports DV profile %d; kept original", p)
	}
	final := strings.TrimSuffix(path, filepath.Ext(path)) + ".mkv"
	if final != path && exists(final) {
		return "", fmt.Errorf("%s already exists", filepath.Base(final))
	}
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if err := os.Rename(out, final); err != nil {
		return "", err
	}
	if final != path {
		os.Remove(path)
		if c.e != nil && c.e.onMoved != nil && !strings.Contains(rel(path), "/.") {
			c.e.onMoved(path, final)
		}
	}
	log.Printf("[%s] converted %s", c.id, final)
	c.log("Converted to Dolby Vision 8.1")
	return final, nil
}
