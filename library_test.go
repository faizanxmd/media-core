package main

import (
	"os"
	"testing"
)

func TestLibraryTargetRefusesUnsafePaths(t *testing.T) {
	for _, p := range []string{"", "Movies", "TV Shows", "Music", "/", "..", "../etc/passwd",
		"Movies/../../etc", "Movies/X/.incoming", "Other/X", "Movies/.convert/x"} {
		if abs, _, err := libraryTarget(p); err == nil {
			t.Errorf("%q was accepted (%s)", p, abs)
		}
	}
	// a real, existing item passes and stays inside the library
	ents, _ := os.ReadDir("/srv/data/media/Movies")
	if len(ents) == 0 {
		t.Skip("no movies on this machine")
	}
	name := ents[0].Name()
	if abs, sec, err := libraryTarget("Movies/" + name); err != nil || sec != "Movies" ||
		abs != "/srv/data/media/Movies/"+name {
		t.Errorf("%s: %s %s %v", name, abs, sec, err)
	}
	// ".." that collapses back inside is just a normal path
	if abs, _, err := libraryTarget("Movies/x/../" + name); err != nil || abs != "/srv/data/media/Movies/"+name {
		t.Errorf("collapsed path: %s %v", abs, err)
	}
}
