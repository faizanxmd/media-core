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
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"
)

// shutdownSignal: the signals that stop the server. A temporary server also stops when its terminal closes
// (SIGHUP), so its temp folder still goes.
func shutdownSignal(temp bool) <-chan os.Signal {
	ch := make(chan os.Signal, 1)
	sigs := []os.Signal{syscall.SIGTERM, syscall.SIGINT}
	if temp {
		sigs = append(sigs, syscall.SIGHUP)
	}
	signal.Notify(ch, sigs...)
	return ch
}

const usage = `usage: media-core-engine [command]

  install [--library DIR] [--name NAME] [--tmdb-key KEY] [--yes]
                                 set this computer up as a server and start it (safe to run again)
  setup [--library DIR] [--name NAME] [--tmdb-key KEY]
                                 change those settings later
  upgrade [--version vX.Y.Z] [--force]
                                 install a release; puts the old one back if the new one doesn't start
  apps [--version vX.Y.Z]        fetch the TV and phone apps from a release, for the server to hand out
  uninstall [--purge] [--yes]    remove the services and binary (--purge: settings and progress too;
                                 the library is never touched)
  serve                          run the server (what the installed service does)
  serve --library DIR [--state DIR] [--port N]
                                 a temporary server for DIR, until Ctrl-C; without --state
                                 its settings and progress live in a temp folder that goes with it.
                                 --port moves it off 8780/8790 (site N, apps N+10)
  add <url> [movie|tv|music] [title]   queue a link
  ls                             list downloads
  devices                        list paired devices and any waiting to pair
  devices approve CODE [--admin] let a waiting device in (a TV showing CODE, say)
  devices revoke ID              take a device's key away
  doctor [--json]                check the server and say how to fix what's wrong (exit 0 ok, 1 warnings, 2 broken)
  backup                         save progress, settings and devices (MEDIA_CORE_BACKUP_DIR: a second copy)
  setup-code                     a new code for setting the server up in a browser
  version                        print the version`

// serveFlags reads serve's options. --library makes a temporary server: it never touches the installed
// server's settings or state, runs its own downloader, and with no --state leaves nothing behind.
func serveFlags(args []string) serveOpts {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	fs.Usage = func() { fmt.Fprintln(os.Stderr, usage) }
	lib := fs.String("library", "", "library folder for a temporary server")
	state := fs.String("state", "", "keep the temporary server's settings and progress here")
	port := fs.Int("port", 0, "site port (the apps' port is this plus 10); default 8780")
	removeState := fs.Bool("remove-state", false, "") // set by a restart: the state folder is temporary
	fs.Parse(args)
	if *lib == "" {
		if *state != "" || *port != 0 {
			fail("--state and --port go with --library")
		}
		return serveOpts{}
	}
	if *port != 0 && (*port < 1 || *port+10 > 65535) {
		fail("--port must be between 1 and 65525 (the apps use it plus 10)")
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
	} else if *removeState {
		o.removeAt = dir
	}
	again := []string{os.Args[0], "serve", "--library", abs, "--state", dir}
	if o.removeAt != "" {
		again = append(again, "--remove-state")
	}
	if *port > 0 {
		again = append(again, "--port", strconv.Itoa(*port))
	}
	restartArgs = func() []string { return again }
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
	case "setup-code":
		code, err := newSetupCode(filepath.Join(ConfDir, "setup.code"))
		if err != nil {
			fail(err.Error())
		}
		fmt.Printf("Setup code: %s (lasts 24 hours)\nOpen http://<this server>:%s/#/setup on a device at home and enter it.\n", code, sitePort())
	case "devices":
		devicesCmd(os.Args[2:])
	case "doctor":
		doctorCmd(os.Args[2:])
	case "backup":
		backupCmd()
	case "install":
		installCmd(installFlags("install", os.Args[2:]))
	case "setup":
		setupCmd(installFlags("setup", os.Args[2:]))
	case "upgrade":
		upgradeCmd(os.Args[2:])
	case "uninstall":
		uninstallCmd(os.Args[2:])
	case "apps":
		appsCmd(os.Args[2:])
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
	case "help", "-h", "--help":
		fmt.Println(usage)
	default:
		fail(usage)
	}
}

// cliCall sends a request with the command line's own key (cli.token, made by the running server).
func cliCall(method, url string, body []byte, timeout time.Duration) (*http.Response, error) {
	req, err := http.NewRequest(method, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if b, err := os.ReadFile(cliTokenPath()); err == nil {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(b)))
	}
	resp, err := (&http.Client{Timeout: timeout}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("%v (is the server running?)", err)
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		var e struct{ Error, Message string }
		json.NewDecoder(resp.Body).Decode(&e)
		if e.Message == "" {
			e.Message = e.Error
		}
		if e.Message == "" {
			e.Message = resp.Status
		}
		return nil, fmt.Errorf("%s", e.Message)
	}
	return resp, nil
}

func fetchStatus(base string) (Status, error) {
	var st Status
	resp, err := cliCall("GET", base+"/api/jobs", nil, 10*time.Second)
	if err != nil {
		return st, err
	}
	defer resp.Body.Close()
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
	resp, err := cliCall("POST", base+path, b, 90*time.Second)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
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

func devicesCmd(args []string) {
	if len(args) == 0 || args[0] == "ls" {
		resp, err := cliCall("GET", adminURL()+"/api/devices", nil, 10*time.Second)
		if err != nil {
			fail(err.Error())
		}
		defer resp.Body.Close()
		var d struct {
			Devices []deviceView
			Waiting []struct{ Code, Name, Kind, Address string }
		}
		json.NewDecoder(resp.Body).Decode(&d)
		tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "ID\tNAME\tKIND\tROLE\tLAST SEEN")
		for _, v := range d.Devices {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", v.ID, v.Name, v.Kind, v.Role, time.Unix(v.Seen, 0).Format("2006-01-02 15:04"))
		}
		tw.Flush()
		for _, w := range d.Waiting {
			fmt.Printf("\nWaiting to pair: %s (%s) from %s, code %s\n  media-core-engine devices approve %s\n", w.Name, w.Kind, w.Address, w.Code, w.Code)
		}
		return
	}
	switch args[0] {
	case "approve":
		if len(args) < 2 {
			fail("usage: media-core-engine devices approve CODE [--admin]")
		}
		role := rolePlayer
		if len(args) > 2 && args[2] == "--admin" {
			role = roleAdmin
		}
		var r struct{ Name, Role string }
		post("/api/pair/approve", map[string]string{"code": args[1], "role": role}, &r)
		fmt.Printf("%s is paired (%s).\n", r.Name, r.Role)
	case "revoke":
		if len(args) < 2 {
			fail("usage: media-core-engine devices revoke ID")
		}
		var r struct{ OK bool }
		post("/api/devices/"+args[1]+"/revoke", map[string]string{}, &r)
		fmt.Println("Revoked.")
	default:
		fail(usage)
	}
}
