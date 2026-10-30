package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// LNode is one folder or file in the media library, as shown on the Library tab.
type LNode struct {
	Name     string   `json:"name"`
	Path     string   `json:"path"` // relative to /srv/data/media
	Dir      bool     `json:"dir"`
	Size     int64    `json:"size"`
	Files    int      `json:"files"`
	Added    float64  `json:"added"` // newest file's modification time
	Children []*LNode `json:"children,omitempty"`
	Info     *MInfo   `json:"info,omitempty"`  // videos; album folders get their tracks' quality
	Meta     *PMeta   `json:"meta,omitempty"`  // from the catalog
	Cover    string   `json:"cover,omitempty"` // a cover.jpg/folder.jpg inside this folder
}

var librarySections = map[string]string{"Movies": "movie", "TV Shows": "tv", "Music": "music"}

// scanTree reads a folder recursively, skipping dot-folders (.incoming,
// .convert: theater's work areas).
func scanTree(abs string) *LNode {
	n := &LNode{Name: filepath.Base(abs), Path: rel(abs), Dir: true}
	ents, _ := os.ReadDir(abs)
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		p := filepath.Join(abs, e.Name())
		var c *LNode
		if e.IsDir() {
			c = scanTree(p)
		} else {
			info, err := e.Info()
			if err != nil || !info.Mode().IsRegular() {
				continue
			}
			c = &LNode{Name: e.Name(), Path: rel(p), Size: info.Size(), Files: 1,
				Added: float64(info.ModTime().Unix())}
		}
		n.Children = append(n.Children, c)
		n.Size += c.Size
		n.Files += c.Files
		n.Added = max(n.Added, c.Added)
	}
	sort.Slice(n.Children, func(a, b int) bool {
		return strings.ToLower(n.Children[a].Name) < strings.ToLower(n.Children[b].Name)
	})
	return n
}

func Library(p *Player) map[string]any {
	out := map[string]any{}
	type probe struct {
		into *LNode // the video itself, or the album folder for music
		path string
	}
	var probes []probe
	var walk func(n *LNode)
	walk = func(n *LNode) {
		albumDone := false
		for _, c := range n.Children {
			switch e := ext(c.Name); {
			case c.Dir:
				walk(c)
			case VideoExt[e]:
				probes = append(probes, probe{c, filepath.Join(Media, c.Path)})
			case AudioExt[e] && !albumDone: // one track tells the album's quality
				probes = append(probes, probe{n, filepath.Join(Media, c.Path)})
				albumDone = true
			case n.Cover == "" && reCover.MatchString(c.Name):
				n.Cover = c.Path
			}
		}
	}
	for dir := range librarySections {
		n := scanTree(filepath.Join(Media, dir))
		walk(n)
		out[dir] = n
	}
	// picture/sound details, a few files at a time (cached after the first look)
	jobs := make(chan probe)
	var wg sync.WaitGroup
	for range 3 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for p := range jobs {
				if fi, err := os.Stat(p.path); err == nil {
					p.into.Info = mediaInfo(p.path, fi)
				}
			}
		}()
	}
	seen := map[string]bool{}
	for _, p := range probes {
		seen[p.path] = true
		jobs <- p
	}
	close(jobs)
	wg.Wait()
	saveMediaInfo(seen)

	idx := p.libraryIndex()
	for dir := range librarySections {
		attachMeta(out[dir].(*LNode), dir, idx)
	}
	free, total := disk()
	out["disk_free"], out["disk_total"] = free, total
	return out
}

var reCover = regexp.MustCompile(`(?i)^(cover|folder|front|album|poster)\.(jpe?g|png|webp)$`)

// attachMeta hangs the catalog's metadata on the folders and files it
// describes: the film folder gets the film, a show folder the show and a
// season folder the season; episodes get their own. Music has none (covers
// come from the album folder).
func attachMeta(root *LNode, section string, idx *metaIdx) {
	var walk func(n *LNode, chain []*LNode)
	walk = func(n *LNode, chain []*LNode) {
		chain = append(chain, n)
		if !n.Dir {
			ref, ok := idx.files[filepath.Join(Media, n.Path)]
			if !ok {
				return
			}
			set := func(depth int, key string) {
				if depth < len(chain) && chain[depth].Meta == nil && idx.items[key] != nil {
					chain[depth].Meta = idx.items[key]
				}
			}
			if section == "Movies" {
				set(1, ref.item) // chain[0] is the section folder
			} else {
				n.Meta = idx.items[ref.item]
				set(1, ref.grand)
				if len(chain) > 3 { // file sits inside a season / album folder
					set(2, ref.parent)
				}
			}
			return
		}
		for _, c := range n.Children {
			walk(c, chain)
		}
	}
	walk(root, nil)
}

// libraryTarget checks that p is something inside a library section that may
// be deleted: never a section root, nothing outside /srv/data/media, no
// theater work folders.
func libraryTarget(p string) (abs, section string, err error) {
	if p == "" || strings.Contains(p, "\x00") {
		return "", "", errors.New("no path")
	}
	cleaned := strings.TrimPrefix(filepath.Clean("/"+p), "/") // "/.." collapses, so no escaping
	parts := strings.Split(cleaned, "/")
	if _, ok := librarySections[parts[0]]; !ok || len(parts) < 2 {
		return "", "", errors.New("only items inside Movies, TV Shows or Music can be deleted")
	}
	for _, part := range parts {
		if strings.HasPrefix(part, ".") {
			return "", "", errors.New("that's a work folder, not a library item")
		}
	}
	abs = filepath.Join(Media, cleaned)
	if _, err := os.Lstat(abs); err != nil {
		return "", "", errors.New("already gone")
	}
	// the folder holding it must really live in the library (no symlinked escapes)
	real, err := filepath.EvalSymlinks(filepath.Dir(abs))
	if err != nil || !strings.HasPrefix(real+"/", Media+"/") {
		return "", "", errors.New("path leaves the media library")
	}
	return abs, parts[0], nil
}

func within(child, parent string) bool {
	return child == parent || strings.HasPrefix(child, parent+"/")
}

// DeleteFromLibrary removes a movie, show, season, episode, album or single
// file from disk and tidies empty folders.
func (e *Engine) DeleteFromLibrary(p string) (freed int64, err error) {
	abs, section, err := libraryTarget(p)
	if err != nil {
		return 0, err
	}
	for _, j := range e.store.List() {
		if !j.busy() {
			continue
		}
		dir, _, final, perr := planPaths(j.Plan)
		if perr == nil && (within(final, abs) || within(abs, final) || within(dir, abs)) {
			return 0, errors.New("a download is still working on this; cancel it first")
		}
	}
	if info, err := os.Lstat(abs); err == nil && info.IsDir() {
		freed = scanTree(abs).Size
	} else if err == nil {
		freed = info.Size()
	}
	if err := os.RemoveAll(abs); err != nil {
		return 0, fmt.Errorf("delete failed: %v", err)
	}
	root := filepath.Join(Media, section)
	parent := filepath.Dir(abs)
	for parent != root && within(parent, root) && os.Remove(parent) == nil { // only succeeds when empty
		parent = filepath.Dir(parent)
	}
	log.Printf("library: deleted %s (%s)", rel(abs), human(freed))
	if e.onLibrary != nil {
		e.onLibrary()
	}
	e.store.Notify() // disk space changed
	return freed, nil
}
