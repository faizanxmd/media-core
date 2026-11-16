package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// filmBusy: another job is still bringing in the film a Light job belongs to.
func (e *Engine) filmBusy(light Job) bool {
	busy, _, _, _, _ := e.filmJobState(light)
	return busy
}

func (e *Engine) filmJobState(light Job) (busy, done, cancelled, failed bool, partialPath string) {
	want := strings.TrimSuffix(light.Plan.Into, filepath.Ext(light.Plan.Into))
	for _, j := range e.store.List() {
		if j.ID == light.ID || j.Plan.Light {
			continue
		}
		if _, _, final, err := planPaths(j.Plan); err == nil {
			r := rel(final)
			if r == light.Plan.Into || strings.TrimSuffix(r, filepath.Ext(r)) == want {
				switch {
				case j.busy():
					busy = true
				case j.State == Done:
					done = true
				case j.State == Cancelled:
					cancelled = true
					if !j.Downloaded {
						if tp, err := targetPath(j); err == nil {
							partialPath = tp
						}
					}
				case j.State == Failed:
					failed = true
					if !j.Downloaded {
						if tp, err := targetPath(j); err == nil {
							partialPath = tp
						}
					}
				}
			}
		}
	}
	return
}

// waitForFilm holds a Light job until its film is in the library (it may
// still be downloading, or waiting for the MP4 step). Other jobs aren't held up.
func (e *Engine) waitForFilm(ctx context.Context, id string) error {
	j, ok := e.store.Get(id)
	if !ok || !j.Plan.Light {
		return nil
	}
	c := jobCtx{e, id}
	ch := e.store.Subscribe()
	defer e.store.Unsubscribe(ch)
	for i := 0; ; i++ {
		j, ok = e.store.Get(id)
		if !ok {
			return errors.New("job removed")
		}
		busy, _, cancelled, failed, _ := e.filmJobState(j)
		if !busy {
			mv := mainVideo(j.Plan.Into)
			isPartial := mv != "" && exists(mv+".aria2") // an unfinished download keeps its control file
			if mv != "" && !isPartial {
				return nil
			}
			if cancelled {
				return errors.New("its film download was cancelled")
			}
			if failed {
				return errors.New("its film download failed")
			}
			return errors.New("its film isn't in the library, so the Light copy has nothing to belong to")
		}
		if i == 0 {
			c.phase(Processing, "Waiting for the film")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ch:
		case <-time.After(20 * time.Second):
		}
	}
}

func removeLightSidecars(light string) {
	os.Remove(light)
	os.Remove(light + ".aria2")
	dir := filepath.Dir(light)
	base := baseName(light)
	ents, _ := os.ReadDir(dir)
	otherVideo := false
	for _, en := range ents {
		if !en.IsDir() && baseName(en.Name()) == base && VideoExt[ext(en.Name())] {
			otherVideo = true
			break
		}
	}
	if !otherVideo {
		for _, en := range ents {
			n := en.Name()
			if !en.IsDir() && (SubExt[ext(n)] || ext(n) == "sup") && strings.HasPrefix(n, base+".") {
				os.Remove(filepath.Join(dir, n))
			}
		}
	}
	os.Remove(dir) // only succeeds if .light is now empty
}

// acceptLight keeps a downloaded Light copy only if it's the same cut as its
// film, makes it an MP4 if that's what lets it play in browsers, and removes
// any older Light copy of the same title.
func (e *Engine) acceptLight(ctx context.Context, c jobCtx, j Job, videos []string) error {
	if len(videos) == 0 {
		return errors.New("no video downloaded")
	}
	light := videos[0]
	film := mainVideo(j.Plan.Into)
	if film == "" {
		removeLightSidecars(light)
		c.set(true, func(j *Job) { j.Placed, j.Downloaded, j.Files = false, false, nil })
		return errors.New("its film isn't in the library")
	}
	c.phase(Processing, "Checking the Light copy")
	a, b := duration(ctx, light), duration(ctx, film)
	if !sameCut(a, b) {
		removeLightSidecars(light)
		c.set(true, func(j *Job) { j.Placed, j.Downloaded, j.Files = false, false, nil })
		return fmt.Errorf("not the same cut: the Light copy runs %s, the film %s, so it was removed", dur(int64(a)), dur(int64(b)))
	}
	if j.Plan.MP4 && canMP4(probeMediaCtx(ctx, light)) {
		mctx, mcancel := context.WithTimeout(ctx, convertTimeout)
		v, err := e.toMP4(mctx, c, light)
		mcancel()
		if err != nil {
			return err
		}
		light = v
	}
	base := baseName(light)
	ents, _ := os.ReadDir(filepath.Dir(light))
	for _, en := range ents { // an older Light copy in another format
		p := filepath.Join(filepath.Dir(light), en.Name())
		if p != light && baseName(en.Name()) == base && VideoExt[ext(en.Name())] {
			os.Remove(p)
		}
	}
	c.set(true, func(j *Job) { j.Placed, j.Files = true, []string{light} })
	c.log("Light copy ready: plays in browsers and on slow connections")
	return nil
}
