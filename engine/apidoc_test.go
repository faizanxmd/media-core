package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestAPIDocListsEveryRoute: /api/v1 is the apps' stable API, so docs/api.md must name every route in it.
func TestAPIDocListsEveryRoute(t *testing.T) {
	doc, err := os.ReadFile("../docs/api.md")
	if err != nil {
		t.Fatal(err)
	}
	route := regexp.MustCompile(`"(?:GET|POST|PUT|DELETE) /api/v1/([^"]*)"`)
	files, _ := filepath.Glob("*.go")
	seen := 0
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, _ := os.ReadFile(f)
		for _, m := range route.FindAllStringSubmatch(string(src), -1) {
			seen++
			key := m[1]
			if i := strings.Index(key, "/{"); i >= 0 {
				key = key[:i]
			}
			// the route's name, as a path piece: `name`, name/…, name?…, /name
			re := regexp.MustCompile("[`/ ]" + regexp.QuoteMeta(key) + "[`/?{ ,.]")
			if !re.Match(doc) {
				t.Errorf("docs/api.md doesn't mention /api/v1/%s (from %s)", m[1], f)
			}
		}
	}
	if seen < 10 {
		t.Fatalf("found only %d routes; has the registration style changed?", seen)
	}
}
