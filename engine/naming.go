package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Flex accepts a JSON string or number and stores it as a string, so plans
// from the old Python app (ints) and the UI (strings) both decode.
type Flex string

func (f *Flex) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if bytes.Equal(b, []byte("null")) {
		*f = ""
		return nil
	}
	if len(b) > 0 && b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*f = Flex(strings.TrimSpace(s))
		return nil
	}
	*f = Flex(b)
	return nil
}

type Plan struct {
	Kind    string `json:"kind"` // movie | tv | music
	Title   string `json:"title"`
	Year    Flex   `json:"year"`
	Season  Flex   `json:"season"`
	Episode Flex   `json:"episode"`
	Ext     string `json:"ext"`
	Archive bool   `json:"archive"`
	// Manager's choices. MP4: make an MP4 after downloading (video untouched).
	// Light: this download is the Light copy of the file or film folder at Into
	// (a library path).
	MP4   bool   `json:"mp4,omitempty"`
	Light bool   `json:"light,omitempty"`
	Into  string `json:"into,omitempty"`
	Path  string `json:"path,omitempty"`
}

var (
	reSep     = regexp.MustCompile(`[._]`)
	reBracket = regexp.MustCompile(`\[[^\]]*\]`)
	reSE      = regexp.MustCompile(`(?i)\bS(\d{1,2})[ ._-]?E(\d{1,3})\b`)
	reSeason  = regexp.MustCompile(`(?i)\b(?:S(\d{1,2})(?:P(?:art)? ?\d{1,2})?|Season ?(\d{1,2}))\b`)
	reYear    = regexp.MustCompile(`\(?\b(19[2-9]\d|20[0-4]\d)\b\)?`)
	reQuality = regexp.MustCompile(`(?i)\b(2160p|1080p|720p|480p|WEB|WEB-?DL|BluRay|REMUX|HDR|DV|x26[45]|H ?26[45])\b`)
	reBad     = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1f]`)
	reSpace   = regexp.MustCompile(`\s+`)
)

func clean(s string) string {
	s = reBad.ReplaceAllString(s, "")
	s = strings.Trim(s, " .-_")
	return reSpace.ReplaceAllString(s, " ")
}

// guess turns a release file name into a Plan.
func guess(filename string) Plan {
	name, ex := filename, ""
	if i := strings.LastIndex(filename, "."); i > 0 {
		name, ex = filename[:i], strings.ToLower(filename[i+1:])
	}
	s := reBracket.ReplaceAllString(reSep.ReplaceAllString(name, " "), " ")
	p := Plan{Kind: "movie", Ext: ex, Archive: ArchiveExt[ex]}
	if AudioExt[ex] {
		p.Kind = "music"
		return p
	}
	if m := reSE.FindStringSubmatchIndex(s); m != nil {
		p.Kind, p.Title = "tv", clean(s[:m[0]])
		p.Season, p.Episode = num(s[m[2]:m[3]]), num(s[m[4]:m[5]])
		return p
	}
	if m := reSeason.FindStringSubmatchIndex(s); m != nil {
		p.Kind, p.Title = "tv", clean(s[:m[0]])
		if m[2] >= 0 {
			p.Season = num(s[m[2]:m[3]])
		} else {
			p.Season = num(s[m[4]:m[5]])
		}
		return p
	}
	var last []int
	for _, y := range reYear.FindAllStringSubmatchIndex(s, -1) {
		if y[0] > 0 {
			last = y
		}
	}
	if last != nil {
		p.Title = clean(strings.ReplaceAll(s[:last[0]], "(", " "))
		p.Year = Flex(s[last[2]:last[3]])
	} else {
		p.Title = clean(reQuality.Split(s, 2)[0])
	}
	return p
}

func num(s string) Flex {
	n, _ := strconv.Atoi(s)
	return Flex(strconv.Itoa(n))
}

func atoi(f Flex, def int) (int, error) {
	if f == "" {
		return def, nil
	}
	n, err := strconv.Atoi(string(f))
	if err != nil || n < 0 || n > 999 {
		return 0, fmt.Errorf("%q is not a number", string(f))
	}
	return n, nil
}

// planPaths returns where aria2 downloads to, the file name (empty when the
// download lands in a .incoming folder to be unpacked/placed), and the final
// file or folder.
func planPaths(p Plan) (dir, file, final string, err error) {
	title := clean(p.Title)
	if title == "" {
		title = "Unknown"
	}
	cleanExt := strings.Map(func(r rune) rune {
		if r >= 'A' && r <= 'Z' {
			return r + ('a' - 'A')
		}
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			return r
		}
		return -1
	}, p.Ext)
	withExt := func(base string) string {
		if cleanExt == "" {
			return base
		}
		return base + "." + cleanExt
	}
	if p.Light {
		relInto := filepath.Clean("/" + p.Into)[1:]
		if relInto == "" {
			return "", "", "", errors.New("a Light copy needs its title")
		}
		if p.Archive {
			return "", "", "", errors.New("a Light copy has to be a link to one video file")
		}
		into := filepath.Join(Media, relInto)
		dir, base := filepath.Join(filepath.Dir(into), ".light"), baseName(into)
		if VideoExt[ext(into)] == false { // a film's folder
			dir, base = filepath.Join(into, ".light"), filepath.Base(into)
		}
		return dir, withExt(base), filepath.Join(dir, withExt(base)), nil
	}
	switch p.Kind {
	case "movie":
		base := title
		if y := clean(string(p.Year)); y != "" {
			base = fmt.Sprintf("%s (%s)", title, y)
		}
		d := filepath.Join(Media, "Movies", base)
		if p.Archive {
			return filepath.Join(d, ".incoming"), "", d, nil
		}
		return d, withExt(base), filepath.Join(d, withExt(base)), nil
	case "tv":
		season, err := atoi(p.Season, 1)
		if err != nil {
			return "", "", "", fmt.Errorf("season: %w", err)
		}
		d := filepath.Join(Media, "TV Shows", title, fmt.Sprintf("Season %02d", season))
		if p.Archive || p.Episode == "" {
			return filepath.Join(d, ".incoming"), "", d, nil
		}
		ep, err := atoi(p.Episode, 0)
		if err != nil {
			return "", "", "", fmt.Errorf("episode: %w", err)
		}
		fn := withExt(fmt.Sprintf("%s - S%02dE%02d", title, season, ep))
		return d, fn, filepath.Join(d, fn), nil
	case "music":
		return filepath.Join(Media, "Music", ".incoming"), "", filepath.Join(Media, "Music"), nil
	}
	return "", "", "", errors.New("unknown kind")
}

// jobPaths returns planPaths with a per-job subfolder under any shared .incoming
// directory so concurrent jobs never collide or delete each other's files.
func jobPaths(p Plan, id string) (dir, file, final string, err error) {
	dir, file, final, err = planPaths(p)
	if err != nil {
		return "", "", "", err
	}
	if file == "" && id != "" && filepath.Base(dir) == ".incoming" {
		dir = filepath.Join(dir, id)
	}
	return dir, file, final, nil
}

type Preview struct {
	Dest  string `json:"dest,omitempty"`
	Note  string `json:"note,omitempty"`
	Error string `json:"error,omitempty"`
}

func preview(p Plan) Preview {
	_, fn, final, err := planPaths(p)
	if err != nil {
		return Preview{Error: err.Error()}
	}
	note := ""
	if fn == "" {
		note = "files placed here"
		if p.Archive {
			note = "unpacked and renamed here"
		}
	}
	return Preview{Dest: rel(final), Note: note}
}

var (
	reEpSE   = regexp.MustCompile(`(?i)S(\d{1,2})[ -]?E(\d{1,3})`)
	reEpX    = regexp.MustCompile(`\b(\d{1,2})x(\d{2})\b`)
	reEpOnly = regexp.MustCompile(`(?i)\b(?:E|Ep|Episode) ?(\d{1,3})\b`)
)

// epParts reads season/episode from a file name; named is true when the season
// came from the name (including S00 specials).
func epParts(name string) (season, episode int, named bool) {
	s := reSep.ReplaceAllString(name, " ")
	if m := reEpSE.FindStringSubmatch(s); m != nil {
		a, _ := strconv.Atoi(m[1])
		b, _ := strconv.Atoi(m[2])
		return a, b, true
	}
	if m := reEpX.FindStringSubmatch(s); m != nil {
		a, _ := strconv.Atoi(m[1])
		b, _ := strconv.Atoi(m[2])
		return a, b, true
	}
	if m := reEpOnly.FindStringSubmatch(s); m != nil {
		b, _ := strconv.Atoi(m[1])
		return 0, b, false
	}
	return 0, 0, false
}

// epNumber reads season/episode from a file name; 0 means unknown.
func epNumber(name string) (season, episode int) {
	s, e, _ := epParts(name)
	return s, e
}
