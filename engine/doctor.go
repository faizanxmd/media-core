package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// doctor checks everything a working server needs and says how to fix what isn't right. It changes nothing
// and, without --online, calls nothing outside this computer.
// Exit code: 0 all fine, 1 only warnings, 2 something is broken. With --json, one object an agent can read.
// The check ids and the field names are a public contract: add, never rename.

type check struct {
	ID      string `json:"id"`
	Status  string `json:"status"` // ok, warn, fail
	Message string `json:"message"`
	Fix     string `json:"fix,omitempty"` // a command or step, when not ok
}

type doctorReport struct {
	Schema    int     `json:"schema"` // of this report
	Version   string  `json:"version"`
	OS        string  `json:"os"`
	Arch      string  `json:"arch"`
	ConfigDir string  `json:"config_dir"`
	DataDir   string  `json:"data_dir"`
	Status    string  `json:"status"`
	Checks    []check `json:"checks"`
}

// rollUp: any fail makes the report fail, else any warn makes it warn.
func rollUp(cs []check) string {
	status := "ok"
	for _, c := range cs {
		if c.Status == "fail" {
			return "fail"
		}
		if c.Status == "warn" {
			status = "warn"
		}
	}
	return status
}

func runDoctor(online bool) doctorReport {
	var cs []check
	add := func(id, status, msg, fix string) { cs = append(cs, check{id, status, msg, fix}) }
	systemd := runtime.GOOS == "linux" && hasSystemd()

	// the library
	if p := libraryProblem(Media); p != "" {
		add("library", "fail", p, "media-core-engine setup --library /path/to/your/media --make-folders")
	} else {
		if missing := missingFolders(Media); len(missing) > 0 {
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

	// settings, state and keys
	if fi, err := os.Stat(ConfDir); err == nil && fi.Mode().Perm()&0o077 != 0 {
		add("config", "warn", ConfDir+" can be read by other users", "chmod 700 "+shellQuote(ConfDir))
	} else if err == nil {
		add("config", "ok", ConfDir+" is private", "")
	}
	switch schema, hasData, err := readSchema(); {
	case err != nil:
		add("state", "fail", err.Error(), "restore a backup: media-core-engine backup list")
	case schema > stateSchema:
		add("state", "fail", fmt.Sprintf("The data was written by a newer version (schema %d; this one knows %d)", schema, stateSchema),
			"install that version (media-core-engine upgrade), or restore an older backup: media-core-engine backup list")
	case hasData && schema < stateSchema:
		add("state", "ok", fmt.Sprintf("schema %d; the server brings it to %d when it next starts", schema, stateSchema), "")
	default:
		add("state", "ok", fmt.Sprintf("schema %d", stateSchema), "")
	}
	if d, err := loadDevices(filepath.Join(ConfDir, "devices.json")); err != nil {
		add("devices", "fail", err.Error(), "restore it: media-core-engine backup list, or move the file away and pair every device again")
	} else if !d.claimed() {
		add("devices", "warn", "Not set up yet: no admin device", "open http://<this server>:"+sitePort()+"/#/setup with the code from: media-core-engine setup-code")
	} else {
		add("devices", "ok", "set up", "")
	}
	if b, err := os.ReadFile(filepath.Join(ConfDir, "tmdb.key")); err != nil || strings.TrimSpace(string(b)) == "" {
		add("tmdb", "warn", "No TMDB key: titles have no posters or details", "get a free key at https://www.themoviedb.org/settings/api, then: media-core-engine setup --tmdb-key-file FILE")
	} else {
		add("tmdb", "ok", "TMDB key saved", "")
	}

	// the service
	exe, _ := selfExe()
	if systemd {
		if out, _ := runSystemctl("is-active", engineUnit); out != "active" {
			add("service", "fail", "The engine's service isn't running", "systemctl --user restart media-core-engine; journalctl --user -u media-core-engine -n 50")
		} else {
			add("service", "ok", engineUnit+" is running", "")
		}
		var differ []string
		for _, name := range unitNames {
			text, ok := renderUnits(currentUnitData(exe))[name]
			if !ok {
				continue
			}
			if b, err := os.ReadFile(filepath.Join(unitDir(), name)); err != nil || string(b) != text {
				differ = append(differ, name)
			}
		}
		if len(differ) > 0 {
			add("units", "warn", "The service files aren't what this version writes: "+strings.Join(differ, ", "), "media-core-engine install")
		} else {
			add("units", "ok", "the service files are current", "")
		}
		if name := userName(); name != "" {
			if out, _ := runLoginctl("show-user", name, "-p", "Linger", "--value"); out != "yes" {
				add("linger", "warn", "The server stops when you log out", "sudo loginctl enable-linger "+name)
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
		unit := ""
		if systemd {
			unit, _ = runSystemctl("is-active", aria2Unit)
		}
		switch {
		case err != nil:
			add("aria2", "fail", "The downloader doesn't answer ("+err.Error()+")", "systemctl --user restart media-core-aria2; journalctl --user -u media-core-aria2 -n 50")
		case systemd && unit != "active":
			add("aria2", "warn", "aria2 "+v.Version+" runs as the engine's child, so downloads stop when the server restarts", "media-core-engine install")
		default:
			add("aria2", "ok", "aria2 "+v.Version+" answers", "")
		}
	}

	// around it
	if dir := filepath.Dir(exe); exe != "" && !onPath(dir) {
		add("path", "warn", dir+" isn't on your PATH", `add to your shell's profile: export PATH="`+homeRel(dir)+`:$PATH"`)
	} else if exe != "" {
		add("path", "ok", dir+" is on your PATH", "")
	}
	add(backupCheck())
	switch apps := appVersions(); {
	case len(apps) == 2:
		add("apps", "ok", "TV app "+apps["tv"]+", phone app "+apps["phone"], "")
	case len(apps) == 1:
		add("apps", "warn", "One of the two apps isn't on this server", "media-core-engine upgrade --apps")
	default:
		add("apps", "warn", "No app builds on this server, so its apps page has nothing to hand out", "media-core-engine upgrade --apps")
	}
	if online {
		if tag, err := latestTag(); err != nil {
			add("update", "warn", "Couldn't ask for the latest release ("+err.Error()+")", "")
		} else if latest := strings.TrimPrefix(tag, "v"); latest != version {
			add("update", "warn", "The latest release is "+latest+"; this is "+version, "media-core-engine upgrade")
		} else {
			add("update", "ok", version+" is the latest release", "")
		}
	}

	return doctorReport{Schema: 1, Version: version, OS: runtime.GOOS, Arch: runtime.GOARCH, ConfigDir: ConfDir, DataDir: DataDir,
		Status: rollUp(cs), Checks: cs}
}

// backupCheck: the nightly backup ran. A server too young to have had one isn't warned about.
func backupCheck() (id, status, msg, fix string) {
	const fixLine = "media-core-engine backup; systemctl --user status media-core-backup.timer"
	if last := lastBackup(); last > 0 {
		age := time.Since(time.Unix(last, 0))
		if age > 48*time.Hour {
			return "backup", "warn", fmt.Sprintf("The newest backup is %d days old", int(age.Hours()/24)), fixLine
		}
		return "backup", "ok", "backed up " + time.Unix(last, 0).Format("2006-01-02 15:04"), ""
	}
	var st stateFile
	if b, err := os.ReadFile(statePath()); err == nil && json.Unmarshal(b, &st) == nil && st.Migrated > 0 &&
		time.Since(time.Unix(st.Migrated, 0)) > 48*time.Hour {
		return "backup", "warn", "No backup yet, and the server is more than two days old", fixLine
	}
	return "backup", "ok", "no backup yet: the first one is made tonight", ""
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

func doctorCmd(args []string) {
	asJSON, online := false, false
	for _, a := range args {
		switch a {
		case "--json", "-json":
			asJSON = true
		case "--online", "-online":
			online = true
		default:
			fmt.Fprintln(os.Stderr, "usage: media-core-engine doctor [--json] [--online]")
			os.Exit(exitUsage)
		}
	}
	rep := runDoctor(online)
	if asJSON {
		printJSON(rep)
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
