package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Cases were generated from the engine's first (Python) version, so the Go
// rewrite names things the same way.
func TestGuessMatchesPython(t *testing.T) {
	b, err := os.ReadFile("testdata/naming_cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct{ Name, Kind, Title, Year, Season, Episode, Final string }
	json.Unmarshal(b, &cases)
	for _, c := range cases {
		p := guess(c.Name)
		_, _, final, err := planPaths(p)
		if err != nil {
			t.Errorf("%s: %v", c.Name, err)
			continue
		}
		// Python left a trailing dot on extension-less names; its library was /srv/data/media
		want := strings.Replace(strings.TrimSuffix(c.Final, "."), "/srv/data/media", Media, 1)
		if p.Kind != c.Kind || p.Title != c.Title || string(p.Year) != c.Year ||
			string(p.Season) != c.Season || string(p.Episode) != c.Episode || final != want {
			t.Errorf("%s:\n got  %+v %s\n want %+v", c.Name, p, final, c)
		}
	}
}

func TestGuessSeasonPart(t *testing.T) {
	for _, name := range []string{
		"Chumbak.S01P2.1080p.NF.WEB-DL.DDP5.1.Atmos.H.265-XDMovies.com-1-001.zip",
		"Chumbak S01 Part 2 1080p.zip",
	} {
		p := guess(name)
		if p.Kind != "tv" || p.Title != "Chumbak" || string(p.Season) != "1" {
			t.Errorf("%s: got %+v", name, p)
		}
	}
}

func TestEpNumber(t *testing.T) {
	for name, want := range map[string][2]int{
		"Show.S02E05.mkv": {2, 5}, "show 3x07.mkv": {3, 7}, "Show Episode 12.mkv": {0, 12},
		"Movie.1920x1080.mkv": {0, 0}, "Show.E04.720p.mkv": {0, 4},
	} {
		if s, e := epNumber(name); s != want[0] || e != want[1] {
			t.Errorf("%s: got %d,%d want %v", name, s, e, want)
		}
	}
}

func TestEpPartsNamesSeason(t *testing.T) {
	for _, tc := range []struct {
		name  string
		s, e  int
		named bool
	}{
		{"S00E01", 0, 1, true},
		{"S02E05", 2, 5, true},
		{"3x07", 3, 7, true},
		{"Show Episode 12", 0, 12, false},
		{"Show.E04.720p", 0, 4, false},
		{"Movie.1920x1080.mkv", 0, 0, false},
	} {
		if s, e, named := epParts(tc.name); s != tc.s || e != tc.e || named != tc.named {
			t.Errorf("%q: got (%d,%d,%v) want (%d,%d,%v)", tc.name, s, e, named, tc.s, tc.e, tc.named)
		}
	}
}

func TestNormalizeURL(t *testing.T) {
	for in, want := range map[string]string{
		"https://drive.google.com/file/d/AbC_1-2/view?usp=sharing).": "https://drive.usercontent.google.com/download?id=AbC_1-2&export=download&confirm=t",
		"https://x.com/a.mkv,":                "https://x.com/a.mkv",
		"https://drive.google.com/open?id=Q9": "https://drive.usercontent.google.com/download?id=Q9&export=download&confirm=t",
	} {
		if got := normalizeURL(in); got != want {
			t.Errorf("%s: got %s", in, got)
		}
	}
}

func TestDispositionName(t *testing.T) {
	want := "Georgie & Mandy's First Marriage (2024) - S01E14 - A Sportsbook and a Breakup [WEBDL-1080p][EAC3 5.1][h264]-FLUX.mkv"
	for _, d := range []string{
		// what workers.dev file proxies send: the encoded name keeps ' ( ), which Go's parser rejects
		`inline; filename="` + want + `"; filename*=UTF-8''Georgie%20%26%20Mandy's%20First%20Marriage%20(2024)%20-%20S01E14%20-%20A%20Sportsbook%20and%20a%20Breakup%20%5BWEBDL-1080p%5D%5BEAC3%205.1%5D%5Bh264%5D-FLUX.mkv`,
		`attachment; filename="` + want + `"`,
		`attachment; filename*=UTF-8''Georgie%20%26%20Mandy's%20First%20Marriage%20(2024)%20-%20S01E14%20-%20A%20Sportsbook%20and%20a%20Breakup%20%5BWEBDL-1080p%5D%5BEAC3%205.1%5D%5Bh264%5D-FLUX.mkv`,
	} {
		if got := dispositionName(d); got != want {
			t.Errorf("%s\n got %q", d, got)
		}
	}
	if got := dispositionName(`attachment; filename=Plain.Name.mkv`); got != "Plain.Name.mkv" {
		t.Errorf("bare: %q", got)
	}
	if got := dispositionName(""); got != "" {
		t.Errorf("empty: %q", got)
	}
}

func TestExtIsSanitised(t *testing.T) {
	cases := []struct {
		ext  string
		want string
	}{
		{"MKV", "Film (2020).mkv"},
		{"mkv/../../x", "Film (2020).mkvx"},
		{"../", "Film (2020)"},
	}
	for _, tc := range cases {
		_, file, final, err := planPaths(Plan{
			Kind:  "movie",
			Title: "Film",
			Year:  "2020",
			Ext:   tc.ext,
		})
		if err != nil {
			t.Fatalf("ext %q: unexpected error: %v", tc.ext, err)
		}
		if file != tc.want {
			t.Errorf("ext %q: file = %q, want %q", tc.ext, file, tc.want)
		}
		wantFinal := filepath.Join(Media, "Movies", "Film (2020)", tc.want)
		if final != wantFinal {
			t.Errorf("ext %q: final = %q, want %q", tc.ext, final, wantFinal)
		}
	}
}

func TestPlanPathsRejectsEmptyCleanPath(t *testing.T) {
	badPaths := []string{".", "..", "/", "a/.."}
	for _, p := range badPaths {
		_, _, _, err := planPaths(Plan{Kind: "movie", Light: true, Into: p, Ext: "mp4"})
		if err == nil || err.Error() != "a Light copy needs its title" {
			t.Errorf("Light Into %q: err = %v, want %q", p, err, "a Light copy needs its title")
		}
	}
}
