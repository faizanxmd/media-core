package main

import (
	"log"
	"net/http"
	"os"
	"testing"
	"time"
)

// By hand, against a real file, for ffmpeg or a browser to read:
//
//	STREAM_FILE=/path/film.mkv STREAM_FOR=120 go test -run TestServeRealStream -timeout 0
//	ffprobe 'http://127.0.0.1:18799/master.m3u8?can=h264,hevc,hdr,aac'
func TestServeRealStream(t *testing.T) {
	file := os.Getenv("STREAM_FILE")
	if file == "" {
		t.Skip("set STREAM_FILE")
	}
	info := probeMedia(file)
	if info == nil {
		t.Fatal("can't read the file")
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{name}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*") // a test page on another port may read it
		t0 := time.Now()
		serveStream(w, r, "test", file, info, r.PathValue("name"))
		if d := time.Since(t0); d > 300*time.Millisecond || os.Getenv("STREAM_DEBUG") != "" {
			log.Printf("served %s in %v (gone: %v)", r.PathValue("name"), d.Round(time.Millisecond), r.Context().Err() != nil)
		}
	})
	secs, _ := time.ParseDuration(os.Getenv("STREAM_FOR") + "s")
	if secs == 0 {
		secs = time.Minute
	}
	srv := &http.Server{Addr: "127.0.0.1:18799", Handler: mux}
	go srv.ListenAndServe()
	time.Sleep(secs)
	srv.Close()
}
