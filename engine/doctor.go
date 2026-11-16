package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// doctor checks everything a working server needs and says how to fix what isn't right. It changes nothing.
// Exit code: 0 all fine, 1 only warnings, 2 something is broken. With --json, one object an agent can read:
// {"version": "...", "status": "ok|warn|fail", "checks": [{"id", "status", "message", "fix"}]}.

type check struct {
	ID      string `json:"id"`
	Status  string `json:"status"` // ok, warn, fail
	Message string `json:"message"`
	Fix     string `json:"fix,omitempty"` // a command or step, when not ok
}

type doctorReport struct {
	Version string  `json:"version"`
	Status  string  `json:"status"`
	Checks  []check `json:"checks"`
}

func runDoctor() doctorReport {
	var cs []check
	add := func(id, status, msg, fix string) { cs = append(cs, check{id, status, msg, fix}) }

	// the library
	if p := libraryProblem(Media); p != "" {
		add("library", "fail", p, "media-core-engine setup --library /path/to/your/media")
	} else {
		var missing []string
		for _, f := range libraryFolders {
			if fi, err := os.Stat(filepath.Join(Media, f)); err != nil || !fi.IsDir() {
				missing = append(missing, f)
			}
		}
		if len(missing) > 0 {
			add("library", "warn", Media+" has no "+strings.Join(missing, ", ")+" folder", "mkdir -p "+shellQuoteAll(Media, missing))
		} else {
			add("library", "ok", Media, "")
		}
		if free, _ := diskAt(Media); free > 0 && free < 20<<30 {
			add("disk", "warn", fmt.Sprintf("%d GB free in the library", free>>30), "free some space: downloads stop when the disk is full")
		} else if free > 0 {
			add("disk", "ok", fmt.Sprintf("%d GB free in the library", free>>30), "")
		}
	}

	// the tools
	for _, t := range toolStates() {
		switch {
		case t.Found && t.Note != "":
			add("tool:"+t.ID, "warn", t.ID+": "+t.Note, t.Install)
		case t.Found:
			add("tool:"+t.ID, "ok", t.ID+" at "+t.Path, "")
		case t.Needed:
			add("tool:"+t.ID, "fail", t.ID+" is missing: "+t.Feature, t.Install)
		default:
			add("tool:"+t.ID, "warn", t.ID+" is missing: "+t.Feature, t.Install)
		}
	}

	// settings and keys
	if fi, err := os.Stat(ConfDir); err == nil && fi.Mode().Perm()&0o077 != 0 {
		add("config", "warn", ConfDir+" can be read by other users", "chmod 700 "+shellQuote(ConfDir))
	}
	if _, err := loadDevices(filepath.Join(ConfDir, "devices.json")); err != nil {
		add("devices", "fail", err.Error(), "")
	} else if d, _ := loadDevices(filepath.Join(ConfDir, "devices.json")); !d.claimed() {
		add("devices", "warn", "Not set up yet: no admin device", "open http://<this server>:"+sitePort()+"/#/setup with the code from: media-core-engine setup-code")
	} else {
		add("devices", "ok", "set up", "")
	}
	if b, err := os.ReadFile(filepath.Join(ConfDir, "tmdb.key")); err != nil || strings.TrimSpace(string(b)) == "" {
		add("tmdb", "warn", "No TMDB key: titles have no posters or details", "get a free key at https://www.themoviedb.org/settings/api, then: media-core-engine setup --tmdb-key KEY")
	} else {
		add("tmdb", "ok", "TMDB key saved", "")
	}
	if envOr("MEDIA_CORE_OPEN_HOME", "") == "1" {
		add("open-home", "warn", "MEDIA_CORE_OPEN_HOME=1 lets home devices in without a key (development only)", "remove it from "+filepath.Join(ConfDir, "engine.env"))
	}

	// the running server
	if runtime.GOOS == "linux" && hasSystemd() {
		if out, _ := exec.Command("systemctl", "--user", "is-active", "media-core-engine.service").Output(); strings.TrimSpace(string(out)) != "active" {
			add("service", "fail", "The engine's service isn't running", "systemctl --user restart media-core-engine; journalctl --user -u media-core-engine -n 50")
		} else {
			add("service", "ok", "media-core-engine.service is running", "")
		}
		if u := os.Getenv("USER"); u != "" {
			if out, _ := exec.Command("loginctl", "show-user", u, "-p", "Linger", "--value").Output(); strings.TrimSpace(string(out)) != "yes" {
				add("linger", "warn", "The server stops when you log out", "sudo loginctl enable-linger "+u)
			} else {
				add("linger", "ok", "keeps running after you log out", "")
			}
		}
	}
	add(helloCheck())
	if a, err := NewAria2(); err != nil {
		add("aria2", "fail", err.Error(), "media-core-engine install")
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		var v struct {
			Version string `json:"version"`
		}
		err := a.call(ctx, "getVersion", &v)
		cancel()
		if err != nil {
			add("aria2", "fail", "The downloader doesn't answer ("+err.Error()+")", "systemctl --user restart media-core-aria2; journalctl --user -u media-core-aria2 -n 50")
		} else {
			add("aria2", "ok", "aria2 "+v.Version+" answers", "")
		}
	}

	rep := doctorReport{Version: version, Status: "ok", Checks: cs}
	for _, c := range cs {
		if c.Status == "fail" {
			rep.Status = "fail"
			break
		}
		if c.Status == "warn" {
			rep.Status = "warn"
		}
	}
	return rep
}

// helloCheck asks the running server who it is: up, and the same version as this binary.
func helloCheck() (id, status, msg, fix string) {
	u := "http://127.0.0.1:" + portOf(playerAddr(), "8790") + "/api/v1/hello"
	c := &http.Client{Timeout: 3 * time.Second}
	r, err := c.Get(u)
	if err != nil {
		return "server", "fail", "The server doesn't answer on the apps' port", "systemctl --user restart media-core-engine"
	}
	defer r.Body.Close()
	var h struct {
		Version string `json:"version"`
		Name    string `json:"name"`
	}
	json.NewDecoder(r.Body).Decode(&h)
	if h.Version != version {
		return "server", "warn", fmt.Sprintf("%s answers, running version %s (this binary is %s)", h.Name, h.Version, version), "systemctl --user restart media-core-engine"
	}
	return "server", "ok", h.Name + " answers (" + h.Version + ")", ""
}

func hasSystemd() bool {
	_, err := exec.LookPath("systemctl")
	return err == nil
}

func doctorCmd(args []string) {
	rep := runDoctor()
	if len(args) > 0 && args[0] == "--json" {
		b, _ := json.MarshalIndent(rep, "", "  ")
		fmt.Println(string(b))
	} else {
		mark := map[string]string{"ok": "  ok  ", "warn": " warn ", "fail": " FAIL "}
		for _, c := range rep.Checks {
			fmt.Printf("[%s] %s\n", mark[c.Status], c.Message)
			if c.Fix != "" {
				fmt.Printf("         fix: %s\n", c.Fix)
			}
		}
	}
	switch rep.Status {
	case "fail":
		os.Exit(2)
	case "warn":
		os.Exit(1)
	}
}

// shellQuote: s as one argument for sh.
func shellQuote(s string) string {
	if s != "" && strings.IndexFunc(s, func(r rune) bool {
		return !(r == '/' || r == '.' || r == '-' || r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9')
	}) < 0 {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func shellQuoteAll(dir string, names []string) string {
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = shellQuote(filepath.Join(dir, n))
	}
	return strings.Join(out, " ")
}
