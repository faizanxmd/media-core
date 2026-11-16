package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSidecarSubsPrefixAndRegionalLang(t *testing.T) {
	dir := t.TempDir()
	ep1 := filepath.Join(dir, "Show - S01E01.mkv")
	os.WriteFile(ep1, []byte("v"), 0o644)
	os.WriteFile(filepath.Join(dir, "Show - S01E01.en.srt"), []byte("1"), 0o644)
	os.WriteFile(filepath.Join(dir, "Show - S01E01.pt-BR.forced.srt"), []byte("1"), 0o644)
	os.WriteFile(filepath.Join(dir, "Show - S01E010.en.srt"), []byte("1"), 0o644)

	subs := sidecarSubs(ep1)
	if len(subs) != 2 {
		t.Fatalf("got %d sidecar subs (%+v), want 2", len(subs), subs)
	}
	byFile := map[string]XSub{}
	for _, s := range subs {
		byFile[filepath.Base(s.file)] = s
	}
	if _, bad := byFile["Show - S01E010.en.srt"]; bad {
		t.Fatal("Show - S01E01.mkv wrongly picked up Show - S01E010.en.srt")
	}
	pt, ok := byFile["Show - S01E01.pt-BR.forced.srt"]
	if !ok {
		t.Fatal("missing Show - S01E01.pt-BR.forced.srt")
	}
	if pt.Lang != "pt-br" || !pt.Forced || pt.Label != "Portuguese (forced)" {
		t.Fatalf("pt-BR sub = %+v", pt)
	}
}

func TestLangNameRegionalTags(t *testing.T) {
	cases := map[string]string{
		"en":    "English",
		"en-US": "English",
		"pt-BR": "Portuguese",
		"zh-CN": "Chinese",
		"en_GB": "English",
		"xx-YY": "",
	}
	for code, want := range cases {
		if got := langName(code); got != want {
			t.Errorf("langName(%q) = %q, want %q", code, got, want)
		}
	}
}
