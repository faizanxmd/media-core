// media-core-engine: paste a link, it lands in the library. Web UI + API on :8780 (home network only).
//
//	media-core-engine [serve]             run the web app (media-core-engine.service)
//	media-core-engine add <url> [kind] [title]   queue a link with the guessed (or given) name
//	media-core-engine ls                  list jobs
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
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
		serve()
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
		fail("usage: media-core-engine [serve | add <url> [kind] [title] | ls]")
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
