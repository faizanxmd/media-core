package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Engine hands downloads to aria2, mirrors their progress into the store and
// runs the post-processing (unpack, rename, Dolby Vision fix, catalog refresh).
type Engine struct {
	store    *Store
	aria     *Aria2
	settings *settingsStore

	kick    chan struct{}
	postSem chan struct{} // one heavy post-processing job at a time (HDD + i3)

	op          sync.Mutex // serialises aria2 bookkeeping (sync loop vs. user actions)
	mu          sync.Mutex
	posts       map[string]context.CancelFunc
	ariaOK      bool
	lastStart   time.Time
	speed       int64
	appliedLim  string
	appliedConc int
	session     string

	onLibrary func()                        // library changed on disk (download placed, item deleted)
	onMoved   func(oldPath, newPath string) // a library file got a new name (MP4 step): carry its watch state over
}

func NewEngine(st *Store, a *Aria2, set *settingsStore) *Engine {
	return &Engine{store: st, aria: a, settings: set, kick: make(chan struct{}, 1),
		postSem: make(chan struct{}, 1), posts: map[string]context.CancelFunc{}}
}

func (e *Engine) Kick() {
	select {
	case e.kick <- struct{}{}:
	default:
	}
}

func (e *Engine) Run() {
	// Jobs that were mid-processing when the app stopped: run that step again
	// (every step is safe to repeat).
	for _, j := range e.store.List() {
		if j.State == Processing || j.State == Converting {
			e.startPost(j.ID)
		}
	}
	for {
		active := e.sync()
		wait := 5 * time.Second
		if active {
			wait = time.Second
		}
		select {
		case <-time.After(wait):
		case <-e.kick:
		}
	}
}

func targetPath(j Job) (string, error) {
	dir, fn, _, err := jobPaths(j.Plan, j.ID)
	if err != nil {
		return "", err
	}
	if fn == "" {
		fn = clean(j.Filename)
	}
	return filepath.Join(dir, fn), nil
}

// legacyTargetPath returns the pre-per-job .incoming/<file> path for jobs
// saved before .incoming/<job ID> isolation.
func legacyTargetPath(j Job) string {
	dir, fn, _, err := planPaths(j.Plan)
	if err != nil || fn != "" || filepath.Base(dir) != ".incoming" {
		return ""
	}
	return filepath.Join(dir, clean(j.Filename))
}

// sync mirrors aria2 state into the jobs; returns whether anything is downloading.
func (e *Engine) sync() bool {
	e.op.Lock()
	defer e.op.Unlock()
	return e.syncLocked()
}

func (e *Engine) syncLocked() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	all, err := e.aria.All(ctx)
	e.mu.Lock()
	wasOK := e.ariaOK
	e.ariaOK = err == nil
	e.mu.Unlock()
	if err != nil {
		if time.Since(e.lastStart) > 30*time.Second {
			e.lastStart = time.Now()
			log.Printf("aria2 unreachable (%v); starting media-core-aria2", err)
			exec.Command("systemctl", "--user", "start", "media-core-aria2.service").Run()
		}
		if wasOK {
			e.store.Notify()
		}
		return false
	}
	if sid := e.aria.Session(ctx); sid != e.session { // aria2 (re)started: it forgot our limits
		if e.session != "" {
			log.Printf("aria2 restarted; re-applying settings")
		}
		e.session = sid
		e.applySettings(ctx, true)
	}
	if !wasOK {
		e.store.Notify()
	}
	byPath := map[string]string{}
	for gid, s := range all {
		if s.Status != "removed" {
			byPath[s.Path()] = gid
		}
	}
	active := false
	jobs := e.store.List()
	known := map[string]bool{}
	for _, j := range jobs {
		known[j.GID] = true
	}
	for gid, s := range all { // results nobody is waiting for (e.g. after a cancel)
		if !known[gid] && (s.Status == "complete" || s.Status == "error" || s.Status == "removed") {
			e.aria.Forget(ctx, gid)
		}
	}
	for _, j := range jobs {
		if !j.inAria() {
			continue
		}
		s, ok := all[j.GID]
		if !ok {
			path, err := targetPath(j)
			if err != nil {
				e.fail(j.ID, err.Error())
				continue
			}
			gid, found := byPath[path]
			if !found {
				if lp := legacyTargetPath(j); lp != "" {
					gid, found = byPath[lp]
				}
			}
			if found { // aria2 restored it under a new gid
				e.store.Update(j.ID, true, func(j *Job) { j.GID = gid })
				s = all[gid]
			} else {
				e.submit(ctx, j)
				active = true
				continue
			}
		}
		switch s.Status {
		case "active", "waiting", "paused":
			state := map[string]string{"active": Downloading, "waiting": Queued, "paused": Paused}[s.Status]
			active = active || state == Downloading
			done, total, spd := i64(s.CompletedLength), i64(s.TotalLength), i64(s.DownloadSpeed)
			e.store.Update(j.ID, state != j.State, func(j *Job) {
				if state == Downloading && j.Started == 0 {
					j.Started = now()
				}
				j.State, j.DoneBytes, j.TotalBytes, j.SpeedBps = state, done, total, spd
				j.Conns, j.QueuePos = int(i64(s.Connections)), s.pos
				j.Progress, j.DoneH, j.TotalH, j.Speed, j.ETA, j.EtaSec = 0, "", "", "", "", 0
				if total > 0 {
					j.Progress = int(done * 100 / total)
					j.DoneH, j.TotalH = human(done), human(total)
				}
				if state == Downloading {
					j.Speed = human(spd) + "/s"
					if spd > 0 && total > done {
						j.EtaSec = (total - done) / spd
						j.ETA = dur(j.EtaSec)
					}
					j.Message = fmt.Sprintf("Downloading (%d connection%s)", j.Conns, plural(j.Conns))
				} else if state == Queued {
					j.Message = "Waiting in queue"
				} else {
					j.Message = "Paused"
				}
			})
		case "complete":
			e.aria.Forget(ctx, s.GID)
			e.store.Update(j.ID, true, func(j *Job) {
				j.State, j.Downloaded, j.Progress, j.Speed, j.ETA, j.SpeedBps, j.EtaSec = Processing, true, 100, "", "", 0, 0
				j.DoneBytes = j.TotalBytes
				if j.Started > 0 && j.TotalBytes > 50e6 {
					if secs := now() - j.Started; secs > 30 {
						e.settings.update(func(st *Settings) {
							bps := float64(j.TotalBytes) / secs
							if st.AvgBps == 0 {
								st.AvgBps = bps
							} else {
								st.AvgBps = 0.6*st.AvgBps + 0.4*bps
							}
						})
					}
				}
				logLine(j, "Downloaded "+human(j.TotalBytes))
			})
			e.startPost(j.ID)
		case "error":
			e.aria.Forget(ctx, s.GID)
			e.fail(j.ID, errorHint(s))
		case "removed":
			e.aria.Forget(ctx, s.GID)
			e.store.Update(j.ID, true, func(j *Job) { j.State, j.GID = Cancelled, ""; logLine(j, "Removed from aria2") })
		}
	}
	spd := int64(0)
	if active {
		spd = e.aria.Speed(ctx)
	}
	e.mu.Lock()
	changed := e.speed != spd
	e.speed = spd
	e.mu.Unlock()
	if changed {
		e.store.Notify()
	}
	return active
}

func (e *Engine) submit(ctx context.Context, j Job) {
	dir, fn, _, err := jobPaths(j.Plan, j.ID)
	if err != nil {
		e.fail(j.ID, err.Error())
		return
	}
	if fn == "" {
		fn = clean(j.Filename)
	}
	if lp := legacyTargetPath(j); lp != "" {
		dst := filepath.Join(dir, fn)
		if !exists(dst) && exists(lp) {
			os.MkdirAll(dir, 0o2775)
			if os.Rename(lp, dst) == nil {
				if exists(lp + ".aria2") {
					os.Rename(lp+".aria2", dst+".aria2")
				}
				os.Remove(filepath.Dir(lp))
			}
		}
	}
	n := itoa(max(j.Connections, 1))
	gid, err := e.aria.Add(ctx, j.URL, map[string]string{
		"dir": dir, "out": fn, "split": n, "max-connection-per-server": n,
	})
	if err != nil {
		e.fail(j.ID, "aria2 refused the download: "+err.Error())
		return
	}
	e.store.Update(j.ID, true, func(j *Job) {
		j.GID = gid
		logLine(j, fmt.Sprintf("Queued in aria2 → %s (%s connection%s)", rel(filepath.Join(dir, fn)), n, plural(max(j.Connections, 1))))
	})
}

func (e *Engine) fail(id, msg string) {
	e.store.Update(id, true, func(j *Job) {
		j.State, j.Speed, j.ETA, j.SpeedBps, j.EtaSec, j.Phase = Failed, "", "", 0, 0, ""
		logLine(j, "Failed: "+msg)
	})
}

// ---------- user actions ----------

var errBusy = errors.New("job is running")

func (e *Engine) NewJob(p Probe, plan Plan) (Job, error) {
	if pv := preview(plan); pv.Error != "" {
		return Job{}, errors.New("bad plan: " + pv.Error)
	}
	if p.URL == "" {
		return Job{}, errors.New("missing link")
	}
	if p.Size != nil {
		need := *p.Size
		for _, j := range e.store.List() {
			if j.inAria() && j.TotalBytes > j.DoneBytes {
				need += j.TotalBytes - j.DoneBytes
			}
		}
		if free, _ := disk(); need > free-5e9 {
			return Job{}, fmt.Errorf("not enough free space: needs %s with the queue, %s free", human(need), human(free))
		}
	}
	plan.Ext = strings.ToLower(plan.Ext)
	j := &Job{ID: newID(), Created: now(), State: Queued, URL: p.URL, Filename: p.Filename,
		Size: p.Size, Connections: max(p.Connections, 1), Plan: plan, Log: []string{}}
	if p.Size != nil {
		j.TotalBytes = *p.Size
	}
	logLine(j, "Added")
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	e.op.Lock()
	e.store.Add(j)
	e.submit(ctx, *j)
	e.op.Unlock()
	e.Kick()
	cp, _ := e.store.Get(j.ID)
	return cp, nil
}

func (e *Engine) Action(id, action, newURL string, del bool) (Job, error) {
	j, ok := e.store.Get(id)
	if !ok {
		return Job{}, errors.New("no such job")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var fresh *Probe
	if action == "retry" && newURL != "" { // probe before taking the lock: it can take a while
		p := e.Probe(ctx, newURL)
		if !p.OK {
			return j, errors.New(p.Error)
		}
		if j.Size != nil && p.Size != nil && *p.Size != *j.Size {
			return j, errors.New("new link is a different file (size differs)")
		}
		fresh = &p
	}
	e.op.Lock()
	defer e.op.Unlock()
	if j, ok = e.store.Get(id); !ok {
		return Job{}, errors.New("no such job")
	}
	switch action {
	case "pause":
		if !j.inAria() || j.GID == "" {
			return j, errors.New("not downloading")
		}
		if err := e.aria.Do(ctx, "pause", j.GID); err != nil {
			return j, err
		}
	case "resume":
		if j.State != Paused {
			return j, errors.New("not paused")
		}
		if err := e.aria.Do(ctx, "unpause", j.GID); err != nil && !gone(err) {
			return j, err
		}
	case "top":
		if j.State != Queued || j.GID == "" {
			return j, errors.New("not queued")
		}
		if err := e.aria.Top(ctx, j.GID); err != nil {
			return j, err
		}
	case "cancel":
		if !j.busy() {
			return j, errors.New("nothing to cancel")
		}
		e.cancelPost(id)
		gid := j.GID
		if j.inAria() && gid != "" {
			e.aria.Do(ctx, "forceRemove", gid)
		}
		j, _ = e.store.Update(id, true, func(j *Job) {
			j.State, j.GID, j.Speed, j.ETA, j.SpeedBps, j.EtaSec, j.Phase = Cancelled, "", "", "", 0, 0, ""
			logLine(j, "Cancelled (partial file kept, retry resumes)")
		})
		if del {
			go e.deletePartial(j, gid)
		}
	case "retry":
		if j.busy() {
			return j, errBusy
		}
		if p := fresh; p != nil {
			j, _ = e.store.Update(id, true, func(j *Job) {
				j.URL, j.Connections = p.URL, max(p.Connections, 1)
				logLine(j, "New link: "+p.Host)
			})
		}
		if j.Downloaded {
			lp := legacyTargetPath(j)
			if path, err := targetPath(j); j.Placed || (err == nil && exists(path)) || (lp != "" && exists(lp)) {
				e.store.Update(id, true, func(j *Job) { j.State = Processing; logLine(j, "Retrying processing") })
				e.startPost(id)
				break
			}
		}
		e.store.Update(id, true, func(j *Job) {
			j.State, j.GID, j.Downloaded = Queued, "", false
			logLine(j, "Queued again (resumes where it stopped)")
		})
		e.submit(ctx, j)
	case "remove":
		if j.busy() {
			return j, errBusy
		}
		e.store.Remove(id)
		return j, nil
	default:
		return j, errors.New("unknown action")
	}
	e.syncLocked() // reply with aria2's new state, not the old one
	e.Kick()
	j, _ = e.store.Get(id)
	return j, nil
}

// ClearFinished removes done and cancelled jobs from the list.
func (e *Engine) ClearFinished() int {
	n := 0
	for _, j := range e.store.List() {
		if j.State == Done || j.State == Cancelled {
			e.store.Remove(j.ID)
			n++
		}
	}
	return n
}

func (e *Engine) deletePartial(j Job, gid string) {
	if gid != "" { // wait until aria2 lets go of the file
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		for ctx.Err() == nil {
			s, err := e.aria.Status(ctx, gid)
			if err != nil || s.Status == "removed" {
				break
			}
			time.Sleep(300 * time.Millisecond)
		}
		e.aria.Forget(ctx, gid)
		cancel()
	}
	path, err := targetPath(j)
	if err != nil || j.Placed {
		return
	}
	os.Remove(path)
	os.Remove(path + ".aria2")
	startDir := filepath.Dir(path)
	if filepath.Base(filepath.Dir(startDir)) == ".incoming" {
		os.RemoveAll(startDir)
		startDir = filepath.Dir(startDir)
	}
	if lp := legacyTargetPath(j); lp != "" && lp != path {
		os.Remove(lp)
		os.Remove(lp + ".aria2")
	}
	// tidy empty folders we created (never the library roots)
	roots := map[string]bool{Media: true, filepath.Join(Media, "Movies"): true,
		filepath.Join(Media, "TV Shows"): true, filepath.Join(Media, "Music"): true}
	for d := startDir; !roots[d] && strings.HasPrefix(d, Media+"/"); d = filepath.Dir(d) {
		if os.Remove(d) != nil {
			break
		}
	}
	e.store.Update(j.ID, true, func(j *Job) { logLine(j, "Cancelled and partial file deleted") })
}

// ---------- speed limits ----------

func (e *Engine) applySettings(ctx context.Context, force bool) {
	s := e.settings.get()
	e.mu.Lock()
	opt := limitOpt(s.LimitMBps)
	needLim := force || opt != e.appliedLim
	needConc := force || s.MaxConcurrent != e.appliedConc
	e.mu.Unlock()
	opts := map[string]string{}
	if needLim {
		opts["max-overall-download-limit"] = opt
	}
	if needConc {
		opts["max-concurrent-downloads"] = itoa(s.MaxConcurrent)
	}
	if len(opts) == 0 {
		return
	}
	if err := e.aria.SetGlobal(ctx, opts); err != nil {
		log.Printf("aria2 options: %v", err)
		return
	}
	e.mu.Lock()
	e.appliedLim, e.appliedConc = opt, s.MaxConcurrent
	e.mu.Unlock()
}

func (e *Engine) SetSettings(fn func(*Settings)) Settings {
	s := e.settings.update(fn)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	e.applySettings(ctx, false)
	e.store.Notify()
	return s
}

// ---------- helpers ----------

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func human(b int64) string {
	f := float64(b)
	for _, u := range []string{"B", "KiB", "MiB", "GiB"} {
		if math.Abs(f) < 1024 || u == "GiB" {
			if u == "B" {
				return fmt.Sprintf("%d B", b)
			}
			return fmt.Sprintf("%.1f %s", f, u)
		}
		f /= 1024
	}
	return ""
}

func dur(s int64) string {
	switch {
	case s >= 3600:
		return fmt.Sprintf("%dh%02dm", s/3600, s%3600/60)
	case s >= 60:
		return fmt.Sprintf("%dm%02ds", s/60, s%60)
	}
	return fmt.Sprintf("%ds", s)
}
