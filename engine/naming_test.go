package main

import (
	"encoding/json"
	"os"
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
		want := strings.TrimSuffix(c.Final, ".") // Python left a trailing dot on extension-less names
		if p.Kind != c.Kind || p.Title != c.Title || string(p.Year) != c.Year ||
			string(p.Season) != c.Season || string(p.Episode) != c.Episode || final != want {
			t.Errorf("%s:\n got  %+v %s\n want %+v", c.Name, p, final, c)
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
