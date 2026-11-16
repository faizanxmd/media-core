package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func setupFakeDVTools(t *testing.T, newDur, newStreamsJSON, newProfile string) (binDir string) {
	t.Helper()
	binDir = t.TempDir()
	writeBin := func(name, script string) {
		p := filepath.Join(binDir, name)
		if err := os.WriteFile(p, []byte("#!/bin/sh\n"+script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeBin("ffmpeg", `cat > /dev/null
echo "out_time_us=1000000" >&2
`)
	writeBin("dovi_tool", `out=""
while [ $# -gt 0 ]; do
  if [ "$1" = "-o" ]; then out="$2"; shift 2; continue; fi
  shift
done
cat > /dev/null
printf 'hevc-data' > "$out"
`)
	writeBin("mkvmerge", `out=""
while [ $# -gt 0 ]; do
  if [ "$1" = "-o" ]; then out="$2"; shift 2; continue; fi
  shift
done
echo "#GUI#progress 100%"
printf 'new-mkv-data' > "$out"
`)
	origStreamsJSON := `{"streams":[{"index":0,"codec_type":"video","codec_name":"hevc"},{"index":1,"codec_type":"audio","codec_name":"truehd"},{"index":2,"codec_type":"audio","codec_name":"ac3"}]}`
	writeBin("ffprobe", `args="$*"
last=""
for a in "$@"; do last="$a"; done
case "$last" in
  *new.mkv)
    case "$args" in
      *format=duration*) echo "`+newDur+`" ;;
      *dv_profile*) echo "`+newProfile+`" ;;
      *json*) printf '%s\n' '`+newStreamsJSON+`' ;;
      *) echo "24000/1001" ;;
    esac
    ;;
  *)
    case "$args" in
      *format=duration*) echo "10.0" ;;
      *dv_profile*) echo "7" ;;
      *json*) printf '%s\n' '`+origStreamsJSON+`' ;;
      *) echo "24000/1001" ;;
    esac
    ;;
esac
`)
	t.Setenv("PATH", binDir+":"+os.Getenv("PATH"))
	return binDir
}

func TestDVConvertVerifiesDurationAudioVideoAndKeepsOriginal(t *testing.T) {
	oldM, oldJ, oldDovi := Media, JobsDir, DoviTool
	Media, JobsDir = t.TempDir(), t.TempDir()
	defer func() { Media, JobsDir, DoviTool = oldM, oldJ, oldDovi }()

	goodStreams := `{"streams":[{"index":0,"codec_type":"video","codec_name":"hevc"},{"index":1,"codec_type":"audio","codec_name":"truehd"},{"index":2,"codec_type":"audio","codec_name":"ac3"}]}`
	badAudioStreams := `{"streams":[{"index":0,"codec_type":"video","codec_name":"hevc"},{"index":1,"codec_type":"audio","codec_name":"truehd"}]}`
	noVideoStreams := `{"streams":[{"index":0,"codec_type":"audio","codec_name":"truehd"},{"index":1,"codec_type":"audio","codec_name":"ac3"}]}`

	cases := []struct {
		name       string
		newDur     string
		newStreams string
		newProf    string
	}{
		{"duration mismatch", "8.0", goodStreams, "8"},
		{"audio stream count mismatch", "10.0", badAudioStreams, "8"},
		{"missing video stream", "10.0", noVideoStreams, "8"},
		{"wrong dv profile", "10.0", goodStreams, "7"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			binDir := setupFakeDVTools(t, tc.newDur, tc.newStreams, tc.newProf)
			DoviTool = filepath.Join(binDir, "dovi_tool")

			dir := filepath.Join(Media, "Movies", "DV (2020)")
			os.MkdirAll(dir, 0o755)
			orig := filepath.Join(dir, "DV (2020).mkv")
			os.WriteFile(orig, []byte("original-dv7"), 0o644)

			e := testEngine(t)
			e.store.Add(&Job{ID: "dv1", State: Converting, Log: []string{}})

			if _, err := dvConvert(context.Background(), jobCtx{e, "dv1"}, orig); err == nil {
				t.Fatalf("expected error for %s, got nil", tc.name)
			}
			b, err := os.ReadFile(orig)
			if err != nil || string(b) != "original-dv7" {
				t.Fatalf("original file was modified or lost: %q (%v)", string(b), err)
			}
		})
	}
}

func TestDVConvertCallsOnMovedWhenExtensionChanges(t *testing.T) {
	oldM, oldJ, oldDovi := Media, JobsDir, DoviTool
	Media, JobsDir = t.TempDir(), t.TempDir()
	defer func() { Media, JobsDir, DoviTool = oldM, oldJ, oldDovi }()

	goodStreams := `{"streams":[{"index":0,"codec_type":"video","codec_name":"hevc"},{"index":1,"codec_type":"audio","codec_name":"truehd"},{"index":2,"codec_type":"audio","codec_name":"ac3"}]}`
	binDir := setupFakeDVTools(t, "10.2", goodStreams, "8")
	DoviTool = filepath.Join(binDir, "dovi_tool")

	dir := filepath.Join(Media, "Movies", "DV (2020)")
	os.MkdirAll(dir, 0o755)
	orig := filepath.Join(dir, "DV (2020).mp4")
	os.WriteFile(orig, []byte("original-mp4"), 0o644)

	e := testEngine(t)
	e.store.Add(&Job{ID: "dv2", State: Converting, Log: []string{}})
	var movedFrom, movedTo string
	e.onMoved = func(from, to string) {
		movedFrom, movedTo = from, to
	}

	final, err := dvConvert(context.Background(), jobCtx{e, "dv2"}, orig)
	if err != nil {
		t.Fatalf("dvConvert failed: %v", err)
	}
	wantFinal := filepath.Join(dir, "DV (2020).mkv")
	if final != wantFinal {
		t.Fatalf("expected final %q, got %q", wantFinal, final)
	}
	if movedFrom != orig || movedTo != wantFinal {
		t.Fatalf("expected onMoved(%q, %q), got (%q, %q)", orig, wantFinal, movedFrom, movedTo)
	}
}

func TestScanProgressDoesNotStopOnLongLine(t *testing.T) {
	pr, pw := io.Pipe()
	errCh := make(chan error, 1)
	go func() {
		// Write > 1 MiB without newline, then newline + 100%
		_, err := pw.Write(bytes.Repeat([]byte("x"), 2<<20))
		if err == nil {
			_, err = pw.Write([]byte("\n100%\n"))
		}
		pw.Close()
		errCh <- err
	}()

	got := 0
	var tail bytes.Buffer
	done := make(chan struct{})
	go func() {
		scanProgress(pr, regexp.MustCompile(`(\d{1,3})%`), func(p int) { got = p }, &tail)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		pr.Close()
		pw.Close()
		t.Fatal("scanProgress blocked on >1MiB line")
	}
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("writer blocked/errored: %v", err)
		}
	case <-time.After(2 * time.Second):
		pr.Close()
		pw.Close()
		t.Fatal("writer blocked because scanProgress stopped reading early")
	}
	if got != 100 {
		t.Fatalf("expected progress 100 after long line, got %d", got)
	}
}

func TestDVConvertDoesNotBlockOnLongStderrLine(t *testing.T) {
	oldM, oldJ, oldDovi := Media, JobsDir, DoviTool
	Media, JobsDir = t.TempDir(), t.TempDir()
	defer func() { Media, JobsDir, DoviTool = oldM, oldJ, oldDovi }()

	goodStreams := `{"streams":[{"index":0,"codec_type":"video","codec_name":"hevc"},{"index":1,"codec_type":"audio","codec_name":"truehd"},{"index":2,"codec_type":"audio","codec_name":"ac3"}]}`
	binDir := setupFakeDVTools(t, "10.0", goodStreams, "8")
	// Overwrite fake ffmpeg to emit a 128 KiB stderr line (>64 KiB Scanner limit)
	longLine := strings.Repeat("A", 128<<10)
	err := os.WriteFile(filepath.Join(binDir, "ffmpeg"), []byte("#!/bin/sh\ncat > /dev/null\nprintf '%s\\n' '"+longLine+"' >&2\necho 'out_time_us=1000000' >&2\n"), 0o755)
	if err != nil {
		t.Fatal(err)
	}
	DoviTool = filepath.Join(binDir, "dovi_tool")

	dir := filepath.Join(Media, "Movies", "DV (2020)")
	os.MkdirAll(dir, 0o755)
	orig := filepath.Join(dir, "DV (2020).mkv")
	os.WriteFile(orig, []byte("original"), 0o644)

	e := testEngine(t)
	e.store.Add(&Job{ID: "dvlong", State: Converting, Log: []string{}})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := dvConvert(ctx, jobCtx{e, "dvlong"}, orig); err != nil {
		t.Fatalf("dvConvert failed or blocked on long stderr line: %v", err)
	}
}

func TestUnpackAndDVConvertTimeouts(t *testing.T) {
	oldM, oldJ, oldDovi := Media, JobsDir, DoviTool
	Media, JobsDir = t.TempDir(), t.TempDir()
	defer func() { Media, JobsDir, DoviTool = oldM, oldJ, oldDovi }()

	binDir := t.TempDir()
	os.WriteFile(filepath.Join(binDir, "7z"), []byte("#!/bin/sh\nsleep 10\n"), 0o755)
	os.WriteFile(filepath.Join(binDir, "ffmpeg"), []byte("#!/bin/sh\nsleep 10\n"), 0o755)
	os.WriteFile(filepath.Join(binDir, "dovi_tool"), []byte("#!/bin/sh\ncat > /dev/null\n"), 0o755)
	os.WriteFile(filepath.Join(binDir, "ffprobe"), []byte("#!/bin/sh\necho 10.0\n"), 0o755)
	t.Setenv("PATH", binDir+":"+os.Getenv("PATH"))
	DoviTool = filepath.Join(binDir, "dovi_tool")

	oldUnpackTO, oldConvertTO := unpackTimeout, convertTimeout
	unpackTimeout = 100 * time.Millisecond
	convertTimeout = 100 * time.Millisecond
	defer func() { unpackTimeout, convertTimeout = oldUnpackTO, oldConvertTO }()

	e := testEngine(t)
	e.store.Add(&Job{ID: "to1", State: Processing, Log: []string{}})
	archDir := filepath.Join(Media, "Movies", "T (2020)", ".incoming", "to1")
	os.MkdirAll(archDir, 0o755)
	arch := filepath.Join(archDir, "t.zip")
	os.WriteFile(arch, []byte("zip"), 0o644)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := unpack(ctx, jobCtx{e, "to1"}, arch); err != context.DeadlineExceeded {
		t.Fatalf("unpack: expected DeadlineExceeded, got %v", err)
	}

	vid := filepath.Join(Media, "Movies", "T (2020)", "T (2020).mkv")
	os.WriteFile(vid, []byte("mkv"), 0o644)
	if _, err := dvConvert(ctx, jobCtx{e, "to1"}, vid); err != context.DeadlineExceeded {
		t.Fatalf("dvConvert: expected DeadlineExceeded, got %v", err)
	}
}
