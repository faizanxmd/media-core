package main

import (
	"context"
	"fmt"
	"io"
	"math"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Probe struct {
	URL         string   `json:"url"`
	Host        string   `json:"host"`
	OK          bool     `json:"ok"`
	Error       string   `json:"error,omitempty"`
	Warnings    []string `json:"warnings"`
	Status      int      `json:"status"`
	Resumable   bool     `json:"resumable"`
	Size        *int64   `json:"size"`
	Filename    string   `json:"filename"`
	Connections int      `json:"connections"`
	EtaMin      *int     `json:"eta_min,omitempty"`
	ShortLived  bool     `json:"short_lived"`
	Plan        *Plan    `json:"plan,omitempty"`
	Preview     *Preview `json:"preview,omitempty"`
	FreeGB      int      `json:"free_gb,omitempty"`
	// The file's picture and sound, read from the link before downloading
	// (nil for archives and links ffprobe can't read: shown after the download).
	Media   *MInfo `json:"media,omitempty"`
	Browser bool   `json:"browser"` // plays in browsers as it is
	CanMP4  bool   `json:"can_mp4"` // an MP4 after downloading would make it play in browsers
	Heavy   bool   `json:"heavy"`   // 4K or HDR: worth a Light copy
	PGS     bool   `json:"pgs"`     // picture subtitles, which an MP4 can only keep beside the file
}

var (
	reTrail   = ".,;)]>'\""
	reDriveID = regexp.MustCompile(`^/file/d/([\w-]+)`)
	reSigned  = regexp.MustCompile(`(?i)[?&](expires|exp|token|signature|sig|x-amz-signature)=`)
	reStatus  = regexp.MustCompile(`status=(\d{3})`)
	reExpired = regexp.MustCompile(`(?i)40[134]|410|429|expired|forbidden|refused|not found`)
	reURL     = regexp.MustCompile(`https?://[^\s<>"']+`)

	probeClient = &http.Client{Transport: &http.Transport{
		Proxy: http.ProxyFromEnvironment, ForceAttemptHTTP2: true,
		MaxIdleConnsPerHost: 8, IdleConnTimeout: 30 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
	}}
)

func normalizeURL(raw string) string {
	raw = strings.TrimRight(strings.TrimSpace(raw), reTrail)
	u, err := url.Parse(raw)
	if err != nil || u.Host != "drive.google.com" {
		return raw
	}
	// Google Drive share link -> direct download form
	id := ""
	if m := reDriveID.FindStringSubmatch(u.Path); m != nil {
		id = m[1]
	} else {
		id = u.Query().Get("id")
	}
	if id == "" {
		return raw
	}
	return "https://drive.usercontent.google.com/download?id=" + url.QueryEscape(id) + "&export=download&confirm=t"
}

type hop struct {
	code  int
	h     http.Header
	final *url.URL
}

// get fetches only the headers (and at most one byte of body).
func get(ctx context.Context, u, rng string, timeout time.Duration) (hop, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return hop{}, err
	}
	req.Header.Set("User-Agent", UA)
	req.Header.Set("Accept-Encoding", "identity") // keep Content-Length/Range honest
	if rng != "" {
		req.Header.Set("Range", "bytes="+rng)
	}
	resp, err := probeClient.Do(req)
	if err != nil {
		return hop{}, err
	}
	if rng != "" {
		io.CopyN(io.Discard, resp.Body, 64<<10) // tiny; lets the connection be reused
	}
	resp.Body.Close()
	return hop{resp.StatusCode, resp.Header, resp.Request.URL}, nil
}

func filenameFrom(raw string, h hop) string {
	if _, params, err := mime.ParseMediaType(h.h.Get("Content-Disposition")); err == nil {
		if fn := strings.TrimSpace(params["filename"]); fn != "" {
			return path.Base(strings.ReplaceAll(fn, "\\", "/"))
		}
	}
	base := func(s string) string {
		u, err := url.Parse(s)
		if err != nil {
			return ""
		}
		b := path.Base(u.Path)
		if b == "/" || b == "." {
			return ""
		}
		return b
	}
	fn := base(raw)
	if (fn == "" || ext(fn) == "") && h.final != nil {
		if f := base(h.final.String()); ext(f) != "" {
			fn = f
		}
	}
	if fn == "" {
		return "download"
	}
	return fn
}

var httpHints = map[int]string{403: "Forbidden: expired or tied to another browser/IP.",
	404: "Not found.", 410: "Gone: one-time link already used/expired.",
	429: "Rate limited, try again in a bit.", 0: "No answer (timeout/DNS)."}

func (e *Engine) Probe(ctx context.Context, raw string) Probe {
	u := normalizeURL(raw)
	pu, _ := url.Parse(u)
	r := Probe{URL: u, Warnings: []string{}}
	if pu == nil || (pu.Scheme != "http" && pu.Scheme != "https") {
		r.Error = "Only http(s) links are supported (no magnets/torrents)."
		return r
	}
	r.Host = pu.Host
	first, err := get(ctx, u, "0-0", 15*time.Second)
	r.Status = first.code
	if err != nil || (first.code != 200 && first.code != 206) {
		r.Error = fmt.Sprintf("Link answered HTTP %d. %s", first.code, httpHints[first.code])
		if err != nil {
			r.Error = "No answer: " + shortErr(err)
		}
		return r
	}
	if strings.HasPrefix(first.h.Get("Content-Type"), "text/html") {
		r.Error = "Link returns a web page, not a file (login/virus-scan/expired page)."
		return r
	}
	r.Resumable = first.code == 206
	h := first
	if cr := first.h.Get("Content-Range"); strings.Contains(cr, "/") {
		if n, err := strconv.ParseInt(cr[strings.LastIndex(cr, "/")+1:], 10, 64); err == nil {
			r.Size = &n
		}
	}
	if r.Size == nil {
		if first.code == 200 && first.h.Get("Content-Length") != "" {
			// ignored the Range header: the length is the whole file
		} else if h2, err := get(ctx, u, "", 15*time.Second); err == nil {
			h = h2
		}
		if n, err := strconv.ParseInt(h.h.Get("Content-Length"), 10, 64); err == nil && n > 1 {
			r.Size = &n
		}
	}
	r.Filename = filenameFrom(u, h)

	// How many parallel connections does the server tolerate?
	r.Connections = 1
	tested := false
	if r.Resumable && r.Size != nil && *r.Size > 50_000_000 {
		tested = true
		var wg sync.WaitGroup
		ok := make([]bool, 4)
		for i := range 4 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				o := *r.Size / 5 * int64(i+1)
				res, err := get(ctx, u, fmt.Sprintf("%d-%d", o, o+1000), 10*time.Second)
				ok[i] = err == nil && res.code == 206
			}()
		}
		wg.Wait()
		r.Connections = 16
		for _, b := range ok {
			if !b {
				r.Connections = 1
			}
		}
	}
	if r.Size != nil {
		m := int(math.Round(float64(*r.Size) / e.settings.get().bps() / 60))
		r.EtaMin = &m
	}
	r.ShortLived = reSigned.MatchString(u)
	for _, s := range []string{"workers.dev", "googleusercontent.com", "1fichier.com"} {
		if strings.Contains(r.Host, s) {
			r.ShortLived = true
		}
	}
	if !r.Resumable {
		r.Warnings = append(r.Warnings, "Can't resume: if it drops, it restarts from zero.")
	}
	if r.Connections == 1 && tested {
		r.Warnings = append(r.Warnings, "Server allows only 1 connection, so expect ~3 MB/s.")
	}
	if r.ShortLived && r.EtaMin != nil && *r.EtaMin > 60 {
		r.Warnings = append(r.Warnings, "Short-lived link and a long download: it may expire midway "+
			"(paste a fresh link on the job to resume).")
	}
	p := guess(r.Filename)
	r.Plan = &p
	pv := preview(p)
	r.Preview = &pv
	if _, fn, final, err := planPaths(p); err == nil && fn != "" {
		if _, err := os.Stat(final); err == nil {
			r.Warnings = append(r.Warnings, "Already in the library: "+rel(final))
		}
	}
	if !p.Archive && VideoExt[p.Ext] {
		if m := linkMedia(ctx, u); m != nil && m.VCodec != "" {
			r.Media, r.Browser, r.CanMP4, r.Heavy, r.PGS = m, playsInBrowser(m), canMP4(m), heavy(m), m.PGS
		} else if p.Ext != "mp4" && p.Ext != "m4v" {
			// a slow link (one connection, other downloads running) can't be
			// read in time: offer the MP4 anyway, and the step after the
			// download checks the file itself and keeps 4K or HDR as it is
			r.CanMP4 = true
		}
		// on unless picture subtitles would be lost on TV and phone; /api/quick takes it as it is
		r.Plan.MP4 = r.CanMP4 && !r.PGS
	}
	r.FreeGB = freeGB()
	r.OK = true
	return r
}

func shortErr(err error) string {
	s := err.Error()
	if i := strings.LastIndex(s, ": "); i >= 0 {
		s = s[i+2:]
	}
	return s
}
