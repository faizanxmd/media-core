package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// THEATER_DVTEST=<some DV file> go test -run TestDVPipeline -v
// Runs the real ffmpeg | dovi_tool | mkvmerge pipeline on a copy of the file.
func TestDVPipeline(t *testing.T) {
	src := os.Getenv("THEATER_DVTEST")
	if src == "" {
		t.Skip("set THEATER_DVTEST to a Dolby Vision file")
	}
	dir := t.TempDir()
	JobsDir = filepath.Join(dir, "jobs")
	work := filepath.Join(dir, "movie"+filepath.Ext(src))
	in, _ := os.Open(src)
	out, _ := os.Create(work)
	io.Copy(out, in)
	in.Close()
	out.Close()

	st := NewStore()
	e := NewEngine(st, nil, &settingsStore{})
	st.Add(&Job{ID: "dvtest", State: Processing})
	ctx := context.Background()
	if p := dvProfile(ctx, work); p == 0 {
		t.Fatalf("source has no DV profile")
	}
	final, err := dvConvert(ctx, jobCtx{e, "dvtest"}, work)
	if err != nil {
		t.Fatal(err)
	}
	j, _ := st.Get("dvtest")
	t.Logf("final %s (profile %d), state %s, phase %q, progress %d, log %v",
		final, dvProfile(ctx, final), j.State, j.Phase, j.Progress, j.Log)
	if fi, _ := os.Stat(final); fi.Size() < 1e6 {
		t.Fatal("output too small")
	}
}
