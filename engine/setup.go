package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// The server's own settings, from the site (Settings > Server, and right after setup): the library folder,
// the TMDB key and the server's name. They are saved where the engine always reads them (engine.env and
// tmdb.key), and the engine then restarts itself to use them, which takes about a second.

var libraryFolders = []string{"Movies", "TV Shows", "Music"}

type serverConfig struct {
	Library   string          `json:"library"`
	LibraryOK string          `json:"library_problem,omitempty"` // why the folder can't be used ("" when fine)
	Folders   map[string]bool `json:"folders"`                   // Movies, TV Shows, Music: there or not
	Name      string          `json:"name"`
	TMDB      bool            `json:"tmdb"`      // a key is saved
	Temporary bool            `json:"temporary"` // serve --library: the folder is fixed
}

// libraryProblem says why dir can't be the library ("" if it can): it must exist and be writable.
func libraryProblem(dir string) string {
	fi, err := os.Stat(dir)
	if err != nil {
		return "There's no folder at " + dir
	}
	if !fi.IsDir() {
		return dir + " isn't a folder"
	}
	f, err := os.CreateTemp(dir, ".media-core-write-test-")
	if err != nil {
		return "Media Core can't write in " + dir + ". Give this user write access to it."
	}
	f.Close()
	os.Remove(f.Name())
	return ""
}

func currentConfig(temp bool) serverConfig {
	c := serverConfig{Library: Media, LibraryOK: libraryProblem(Media), Folders: map[string]bool{}, Name: serverName(), Temporary: temp}
	for _, f := range libraryFolders {
		fi, err := os.Stat(filepath.Join(Media, f))
		c.Folders[f] = err == nil && fi.IsDir()
	}
	if b, err := os.ReadFile(filepath.Join(ConfDir, "tmdb.key")); err == nil && strings.TrimSpace(string(b)) != "" {
		c.TMDB = true
	}
	return c
}

// setEnvLine sets KEY=value in an engine.env file, keeping every other line (and comments) as they were.
// An empty value removes the line.
func setEnvLine(path, key, value string) error {
	if strings.ContainsAny(value, "\r\n\x00") {
		return fmt.Errorf("a setting can't contain a line break")
	}
	var lines []string
	if b, err := os.ReadFile(path); err == nil {
		lines = strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	}
	out := lines[:0]
	done := false
	for _, l := range lines {
		k, _, ok := strings.Cut(strings.TrimSpace(l), "=")
		if ok && strings.TrimSpace(k) == key {
			if value != "" && !done {
				out = append(out, key+"="+value)
				done = true
			}
			continue
		}
		out = append(out, l)
	}
	if value != "" && !done {
		out = append(out, key+"="+value)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	text := strings.Join(out, "\n")
	if text != "" {
		text += "\n"
	}
	if err := os.WriteFile(path+".tmp", []byte(text), 0o600); err != nil {
		return err
	}
	return os.Rename(path+".tmp", path)
}

// checkTMDBKey asks TMDB whether a key works: a v3 key (32 hex) or a v4 read token.
var checkTMDBKey = func(key string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	u := "https://api.themoviedb.org/3/configuration"
	if len(key) <= 40 {
		u += "?api_key=" + url.QueryEscape(key)
	}
	req, _ := http.NewRequestWithContext(ctx, "GET", u, nil)
	if len(key) > 40 {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("couldn't reach TMDB to check the key. Is the server online?") // err holds the URL, key and all
	}
	resp.Body.Close()
	if resp.StatusCode == 401 {
		return fmt.Errorf("TMDB doesn't accept that key")
	}
	if resp.StatusCode != 200 {
		return fmt.Errorf("TMDB answered %s", resp.Status)
	}
	return nil
}

// restartSelf replaces the running engine with a fresh copy of itself (same arguments), after flush has saved
// what's in memory. The listening sockets close on exec, so the new copy can open them again.
var restartSelf = func(flush func()) {
	go func() {
		time.Sleep(400 * time.Millisecond) // let the answer reach the browser
		flush()
		exe, err := os.Executable()
		if err != nil {
			log.Printf("restart: %v", err)
			os.Exit(1) // the service manager starts it again
		}
		log.Print("restarting to use the new settings")
		if err := syscall.Exec(exe, restartArgs(), os.Environ()); err != nil {
			log.Printf("restart: %v", err)
			os.Exit(1)
		}
	}()
}

// restartArgs: how to start this engine again. A temporary server keeps its state folder across the restart
// (and still removes it at the end).
var restartArgs = func() []string { return os.Args }

type setupAPI struct {
	temp  bool
	busy  func() string // what's running that a library change would break ("" when nothing)
	flush func()
}

func (s *setupAPI) mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/setup/config", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, currentConfig(s.temp))
	})
	// what a restart, an upgrade or an uninstall must wait for: the one place that decides "busy"
	mux.HandleFunc("GET /api/busy", func(w http.ResponseWriter, r *http.Request) {
		why := s.busy()
		writeJSON(w, 200, map[string]any{"busy": why != "", "why": why})
	})
	mux.HandleFunc("POST /api/setup/config", func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			Library     *string `json:"library"`
			MakeFolders bool    `json:"make_folders"`
			Name        *string `json:"name"`
			TMDBKey     *string `json:"tmdb_key"`
		}
		if !readJSON(w, r, &b) {
			return
		}
		res, err := applyConfig(configChange{Library: b.Library, MakeFolders: b.MakeFolders, Name: b.Name, TMDBKey: b.TMDBKey,
			temp: s.temp, busy: s.busy})
		if err != nil {
			errJSON(w, err.status, err.msg)
			return
		}
		restart := len(res.Changed) > 0
		if restart {
			restartSelf(s.flush)
		}
		writeJSON(w, 200, map[string]any{"ok": true, "restart": restart})
	})
}

// configChange is a change to the server's own settings; a nil field is left as it is.
type configChange struct {
	Library     *string
	MakeFolders bool // make the library folder and Movies, TV Shows, Music in it
	Name        *string
	TMDBKey     *string
	temp        bool          // a temporary server: its folder is fixed
	busy        func() string // asked before a change the server must restart for; nil: don't ask
}

type configResult struct {
	Changed []string // "library", "name", "tmdb": what was written
	Library string   // the library after the change
	Name    string
	TMDB    bool // a key is saved
}

// applyConfig checks a settings change and writes it where the engine reads its settings (engine.env and
// tmdb.key). The site's Settings page and `media-core-engine setup` and `install` all go through it, so
// they accept and refuse the same things. Nothing is written unless every part of the change is fine, and
// nothing is restarted here: anything in Changed needs a restart to take effect.
func applyConfig(in configChange) (configResult, *cliErr) {
	envPath := filepath.Join(ConfDir, "engine.env")
	keyPath := filepath.Join(ConfDir, "tmdb.key")
	res := configResult{Library: Media, Name: serverName()}
	saved := ""
	if b, err := os.ReadFile(keyPath); err == nil {
		saved = strings.TrimSpace(string(b))
	}
	res.TMDB = saved != ""

	if in.Library != nil {
		if strings.IndexFunc(*in.Library, func(c rune) bool { return c < 0x20 || c == 0x7f }) >= 0 {
			return res, cliFail(exitUsage, "The folder's path can't contain line breaks or control characters")
		}
		want := expandHome(strings.TrimSpace(*in.Library))
		if !filepath.IsAbs(want) {
			return res, cliFail(exitUsage, "Give the library folder's full path, starting with /")
		}
		if want = filepath.Clean(want); want != Media {
			if in.temp {
				return res, cliFail(exitFailed, "A temporary server's folder is fixed. Start it again with another --library.")
			}
			res.Library = want
		}
	}
	name, key := res.Name, ""
	if in.Name != nil {
		name = cleanName(*in.Name, "")
	}
	if in.TMDBKey != nil {
		key = strings.TrimSpace(*in.TMDBKey)
	}
	newKey := key != "" && key != saved
	// saving restarts the engine, which would cut off a stream or a download being finished
	if (res.Library != Media || name != res.Name || newKey) && in.busy != nil {
		if what := in.busy(); what != "" {
			return res, cliFail(exitBusy, "Saving restarts the server. Wait until %s, then save.", what)
		}
	}
	if in.MakeFolders {
		for _, f := range libraryFolders {
			if err := os.MkdirAll(filepath.Join(res.Library, f), 0o775); err != nil {
				return res, cliFail(exitFailed, "Couldn't make %s: %v", filepath.Join(res.Library, f), err)
			}
		}
	}
	if in.Library != nil || in.MakeFolders {
		if p := libraryProblem(res.Library); p != "" {
			res.Library = Media
			return res, cliFail(exitFailed, "%s", p)
		}
	}
	if newKey {
		if err := checkTMDBKey(key); err != nil {
			res.Library = Media
			return res, cliFail(exitFailed, "%s", err.Error())
		}
	}

	internal := func(err error) *cliErr {
		return &cliErr{code: exitFailed, status: http.StatusInternalServerError, msg: err.Error()}
	}
	if res.Library != Media {
		if err := setEnvLine(envPath, "MEDIA_CORE_LIBRARY", res.Library); err != nil {
			return res, internal(err)
		}
		res.Changed = append(res.Changed, "library")
	} else if in.Library != nil && parseEnvFile(envPath)["MEDIA_CORE_LIBRARY"] == "" {
		// the default, named on purpose: pin it, so a later version's different default can't move it
		// (the same folder as now, so nothing restarts)
		if err := setEnvLine(envPath, "MEDIA_CORE_LIBRARY", res.Library); err != nil {
			return res, internal(err)
		}
	}
	if name != res.Name {
		if err := setEnvLine(envPath, "MEDIA_CORE_NAME", name); err != nil {
			return res, internal(err)
		}
		res.Name = name
		res.Changed = append(res.Changed, "name")
	}
	if newKey {
		if err := os.MkdirAll(ConfDir, 0o700); err != nil {
			return res, internal(err)
		}
		if err := os.WriteFile(keyPath+".tmp", []byte(key+"\n"), 0o600); err != nil {
			return res, internal(err)
		}
		if err := os.Rename(keyPath+".tmp", keyPath); err != nil {
			return res, internal(err)
		}
		res.TMDB = true
		res.Changed = append(res.Changed, "tmdb")
	}
	return res, nil
}

// busyWith: what a library move must wait for, in words ("" when nothing).
func busyWith(store *Store) string {
	for _, j := range store.List() {
		if j.busy() {
			return "the downloads finish"
		}
	}
	if playingNow() > 0 {
		return "nothing is playing"
	}
	return ""
}
