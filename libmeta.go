package main

import "time"

// PMeta is what the Library tab shows about a film, show, season or
// episode: artwork, summary, ratings and watch state. It comes from the
// catalog (TMDB) and watch.json.
type PMeta struct {
	Key      string   `json:"key"`
	Type     string   `json:"type"`
	Title    string   `json:"title"`
	Year     int      `json:"year,omitempty"`
	Index    int      `json:"index,omitempty"` // episode / season number
	Thumb    string   `json:"thumb,omitempty"` // URLs under /api/v1/img
	Art      string   `json:"art,omitempty"`
	Logo     string   `json:"logo,omitempty"`
	Summary  string   `json:"summary,omitempty"`
	Tagline  string   `json:"tagline,omitempty"`
	Rated    string   `json:"rated,omitempty"` // content rating: R, TV-MA
	Rating   float64  `json:"rating,omitempty"`
	Genres   []string `json:"genres,omitempty"`
	Cast     []string `json:"cast,omitempty"`
	Studio   string   `json:"studio,omitempty"`
	Duration int64    `json:"duration,omitempty"`
	Viewed   int      `json:"viewed,omitempty"` // times watched
	Offset   int64    `json:"offset,omitempty"` // part-way through (ms)
	Leaves   int      `json:"leaves,omitempty"` // episodes in a show or season
	Seen     int      `json:"seen,omitempty"`   // of which watched
}

// fileRef ties a file on disk to its catalog item and that item's parents.
type fileRef struct{ item, parent, grand string }

type metaIdx struct {
	at    time.Time
	files map[string]fileRef
	items map[string]*PMeta
}

func tmdbImg(size, file string) string {
	if file == "" {
		return ""
	}
	return "/api/v1/img/" + size + "/" + file[1:] // TMDB files start with "/"
}

func (p *Player) pmeta(it *Item) *PMeta {
	m := &PMeta{Key: it.ID, Type: it.Type, Title: it.Title, Year: it.Year}
	switch it.Type {
	case "season":
		m.Index = it.Season
	case "episode":
		m.Index = it.Episode
	}
	if t := it.Meta; t != nil {
		if t.Title != "" && it.Type != "season" {
			m.Title = t.Title
		}
		m.Summary, m.Tagline, m.Rated, m.Rating, m.Genres, m.Studio = t.Overview, t.Tagline, t.Cert, t.Rating, t.Genres, t.Studio
		m.Thumb, m.Art, m.Logo = tmdbImg("w342", t.Poster), tmdbImg("w1280", t.Backdrop), tmdbImg("w500", t.Logo)
		if m.Thumb == "" {
			m.Thumb = tmdbImg("w300", t.Still)
		}
		for i, c := range t.Cast {
			if i < 4 {
				m.Cast = append(m.Cast, c.Name)
			}
		}
		m.Duration = int64(t.Runtime) * 60000
	}
	if it.Info != nil && it.Info.Dur > 0 {
		m.Duration = int64(it.Info.Dur * 1000)
	}
	if it.Type == "show" || it.Type == "season" {
		for _, e := range episodesOf(it) {
			m.Leaves++
			if s := p.w.Get(e.ID); s != nil && s.Watched {
				m.Seen++
			}
		}
	} else if s := p.w.Get(it.ID); s != nil {
		if s.Watched {
			m.Viewed = max(s.Plays, 1)
		} else {
			m.Offset = s.Pos
		}
	}
	return m
}

// libraryIndex maps every video the catalog knows to its metadata.
func (p *Player) libraryIndex() *metaIdx {
	idx := &metaIdx{at: time.Now(), files: map[string]fileRef{}, items: map[string]*PMeta{}}
	movies, shows := p.c.Lists()
	for _, m := range movies {
		idx.items[m.ID] = p.pmeta(m)
		idx.files[m.file] = fileRef{item: m.ID}
	}
	for _, s := range shows {
		idx.items[s.ID] = p.pmeta(s)
		for _, sn := range s.Seasons {
			idx.items[sn.ID] = p.pmeta(sn)
			for _, e := range sn.Eps {
				idx.items[e.ID] = p.pmeta(e)
				idx.files[e.file] = fileRef{e.ID, sn.ID, s.ID}
			}
		}
	}
	return idx
}
