// media-core-engine: paste a link, it lands in the library. Web UI + API on :8780 (home network only).
//
//	media-core-engine [serve]             run the server (media-core-engine.service)
//	media-core-engine serve --library DIR [--state DIR]   a temporary server: stops with Ctrl-C
//	media-core-engine add <url> [kind] [title]   queue a link with the guessed (or given) name
//	media-core-engine ls                  list jobs
//	media-core-engine version
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"path/filepath"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"
)

func shutdownSignal() <-chan os.Signal {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGTERM, syscall.SIGINT)
	return ch
}

const usage = `usage: media-core-engine [command]

  serve                          run the server (what the installed service does)
  serve --library DIR [--state DIR] [--port N]
                                 a temporary server for DIR, until Ctrl-C; without --state
                                 its settings and progress live in a temp folder that goes with it.
                                 --port moves it off 8780/8790 (site N, apps N+10)
  add <url> [movie|tv|music] [title]   queue a link
  ls                             list downloads
  version                        print the version`

// serveFlags reads serve's options. --library makes a temporary server: it never touches the installed
// server's settings or state, runs its own downloader, and with no --state leaves nothing behind.
func serveFlags(args []string) serveOpts {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	fs.Usage = func() { fmt.Fprintln(os.Stderr, usage) }
	lib := fs.String("library", "", "library folder for a temporary server")
	state := fs.String("state", "", "keep the temporary server's settings and progress here")
	port := fs.Int("port", 0, "site port (the apps' port is this plus 10); default 8780")
	fs.Parse(args)
	if *lib == "" {
		if *state != "" {
			fail("--state goes with --library")
		}
		return serveOpts{}
	}
	abs, err := filepath.Abs(*lib)
	if err != nil {
		fail(err.Error())
	}
	if fi, err := os.Stat(abs); err != nil || !fi.IsDir() {
		fail("no such folder: " + abs)
	}
	if *port > 0 {
		os.Setenv("MEDIA_CORE_LISTEN", ":"+strconv.Itoa(*port))
		os.Setenv("MEDIA_CORE_PLAYER_LISTEN", ":"+strconv.Itoa(*port+10))
	}
	o := serveOpts{temp: true}
	dir := *state
	if dir == "" {
		if dir, err = os.MkdirTemp("", "media-core-"); err != nil {
			fail(err.Error())
		}
		o.removeAt = dir
	} else if dir, err = filepath.Abs(dir); err != nil {
		fail(err.Error())
	}
	setPaths(filepath.Join(dir, "conf"), filepath.Join(dir, "data"), trimSlash(abs))
	// a key the installed server uses works here too, so titles get posters
	if _, err := os.Stat(filepath.Join(ConfDir, "tmdb.key")); err != nil {
		if b, err := os.ReadFile(filepath.Join(defaultConfDir(), "tmdb.key")); err == nil {
			os.MkdirAll(ConfDir, 0o700)
			os.WriteFile(filepath.Join(ConfDir, "tmdb.key"), b, 0o600)
		}
	}
	fmt.Fprintf(os.Stderr, "Temporary server for %s\nState: %s%s\nStop it with Ctrl-C.\n\n", abs, dir,
		map[bool]string{true: " (removed when it stops)", false: ""}[o.removeAt != ""])
	return o
}

func adminURL() string {
	return "http://" + net.JoinHostPort("127.0.0.1", portOf(listenAddr(), "8780"))
}

func main() {
	cmd := "serve"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	switch cmd {
	case "serve":
		serve(serveFlags(os.Args[2:]))
	case "version", "--version", "-v":
		fmt.Println("media-core-engine", version)
	case "add":
		if len(os.Args) < 3 {
			fail("usage: media-core-engine add <url> [movie|tv|music] [title]")
		}
		body := map[string]string{"url": os.Args[2]}
		if len(os.Args) > 3 {
			body["kind"] = os.Args[3]
		}
		if len(os.Args) > 4 {
			body["title"] = strings.Join(os.Args[4:], " ")
		}
		var r struct {
			OK      bool   `json:"ok"`
			Message string `json:"message"`
		}
		post("/api/quick", body, &r)
		fmt.Println(r.Message)
		if !r.OK {
			os.Exit(1)
		}
	case "ls":
		st, err := fetchStatus(adminURL())
		if err != nil {
			fail(err.Error())
		}
		tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "ID\tSTATE\tPROGRESS\tSPEED\tETA\tDEST")
		for _, j := range st.Jobs {
			fmt.Fprintf(tw, "%s\t%s\t%d%%\t%s\t%s\t%s\n", j.ID, j.State, j.Progress, j.Speed, j.ETA, j.Dest)
		}
		tw.Flush()
		fmt.Printf("%d GB free\n", st.FreeGB)
	default:
		fail(usage)
	}
}

func fetchStatus(base string) (Status, error) {
	var st Status
	c := &http.Client{Timeout: 10 * time.Second}
	resp, err := c.Get(base + "/api/jobs")
	if err != nil {
		return st, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return st, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
		return st, err
	}
	return st, nil
}

func postJSON(base, path string, body, out any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	c := &http.Client{Timeout: 90 * time.Second}
	resp, err := c.Post(base+path, "application/json", bytes.NewReader(b))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func post(path string, body, out any) {
	if err := postJSON(adminURL(), path, body, out); err != nil {
		fail(err.Error())
	}
}

func fail(msg string) {
	fmt.Fprintln(os.Stderr, msg)
	os.Exit(1)
}
