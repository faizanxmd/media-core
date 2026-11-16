package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Backups hold what the engine can't rebuild: watch progress, settings, intro data, title matches, the state
// schema, the paired devices and engine.env. Left out on purpose: secrets (tmdb.key, aria2.conf, webdav.pass,
// cli.token, setup.code), the library and every cache. Device keys are stored only as hashes, so keeping
// devices.json means a restore doesn't make every device pair again.
//
// An archive is a gzip tar, mode 0600, with a manifest.json listing each file's size and SHA-256. The nightly
// copy is <data>/backups/state/state-YYYY-MM-DD.tgz, with a second copy in MEDIA_CORE_BACKUP_DIR when set.

type backupEntry struct{ base, rel, name string }

// backupEntries: the only names an archive may hold, and where each lives. restore accepts nothing else.
func backupEntries() []backupEntry {
	return []backupEntry{
		{DataDir, "watch.json", "data/watch.json"},
		{DataDir, "settings.json", "data/settings.json"},
		{DataDir, "intros.json", "data/intros.json"},
		{DataDir, "tmdb/matches.json", "data/tmdb/matches.json"},
		{DataDir, "state.json", "data/state.json"},
		{ConfDir, "engine.env", "conf/engine.env"},
		{ConfDir, "devices.json", "conf/devices.json"},
	}
}

type manifestFile struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type backupManifest struct {
	Schema  int            `json:"schema"`
	Engine  string         `json:"engine"`
	Created int64          `json:"created"`
	Files   []manifestFile `json:"files"`
}

func backupDir() string { return filepath.Join(DataDir, "backups", "state") }

// writeBackup puts an archive of the current state at out (through a temp file) and lists what went in.
func writeBackup(out string) ([]string, error) {
	if err := os.MkdirAll(filepath.Dir(out), 0o700); err != nil {
		return nil, err
	}
	type file struct {
		name string
		data []byte
		mod  time.Time
	}
	var files []file
	man := backupManifest{Schema: stateSchema, Engine: version, Created: time.Now().Unix()}
	for _, e := range backupEntries() {
		p := filepath.Join(e.base, e.rel)
		b, err := os.ReadFile(p)
		if err != nil {
			continue // not made yet on this server
		}
		fi, _ := os.Stat(p)
		h := sha256.Sum256(b)
		files = append(files, file{e.name, b, fi.ModTime()})
		man.Files = append(man.Files, manifestFile{e.name, int64(len(b)), hex.EncodeToString(h[:])})
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("nothing to back up in %s", DataDir)
	}
	mb, _ := json.MarshalIndent(man, "", " ")
	files = append([]file{{"manifest.json", mb, time.Now()}}, files...)

	f, err := os.OpenFile(out+".tmp", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, err
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	var names []string
	for _, x := range files {
		if err = tw.WriteHeader(&tar.Header{Name: x.name, Mode: 0o600, Size: int64(len(x.data)), ModTime: x.mod}); err == nil {
			_, err = tw.Write(x.data)
		}
		if err != nil {
			break
		}
		if x.name != "manifest.json" {
			names = append(names, x.name)
		}
	}
	for _, c := range []io.Closer{tw, gz} {
		if e := c.Close(); err == nil {
			err = e
		}
	}
	if err == nil {
		err = f.Sync()
	}
	if e := f.Close(); err == nil {
		err = e
	}
	if err == nil {
		err = os.Rename(out+".tmp", out)
	}
	if err != nil {
		os.Remove(out + ".tmp")
		return nil, err
	}
	return names, nil
}

// pruneBackups keeps the newest keep archives in dir whose names start with prefix.
func pruneBackups(dir, prefix string, keep int) int {
	names, _ := filepath.Glob(filepath.Join(dir, prefix+"*.tgz"))
	sort.Slice(names, func(i, j int) bool { return backupTime(names[i]).After(backupTime(names[j])) })
	n := 0
	for i, name := range names {
		if i >= keep && os.Remove(name) == nil {
			n++
		}
	}
	return n
}

func backupTime(p string) time.Time {
	if fi, err := os.Stat(p); err == nil {
		return fi.ModTime()
	}
	return time.Time{}
}

// mkdirWithin makes dir, giving up after d: an automounted drive that isn't there can hang the call.
func mkdirWithin(dir string, d time.Duration) error {
	done := make(chan error, 1)
	go func() { done <- os.MkdirAll(dir, 0o700) }()
	select {
	case err := <-done:
		return err
	case <-time.After(d):
		return fmt.Errorf("%s didn't answer within %s (a drive that isn't plugged in?)", dir, d)
	}
}

// statWithin checks dir is there and is a folder, giving up after d.
func statWithin(dir string, d time.Duration) error {
	done := make(chan error, 1)
	go func() {
		fi, err := os.Stat(dir)
		if err == nil && !fi.IsDir() {
			err = fmt.Errorf("%s isn't a folder", dir)
		}
		done <- err
	}()
	select {
	case err := <-done:
		if os.IsNotExist(err) {
			return fmt.Errorf("%s isn't there (is the drive plugged in?)", dir)
		}
		return err
	case <-time.After(d):
		return fmt.Errorf("%s didn't answer within %s (a drive that isn't plugged in?)", dir, d)
	}
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst+".tmp", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	if err == nil {
		err = out.Sync()
	}
	if e := out.Close(); err == nil {
		err = e
	}
	if err == nil {
		err = os.Rename(dst+".tmp", dst)
	}
	if err != nil {
		os.Remove(dst + ".tmp")
	}
	return err
}

func backupKeep() int {
	if n, err := strconv.Atoi(envOr("MEDIA_CORE_BACKUP_KEEP", "30")); err == nil && n > 0 {
		return n
	}
	return 30
}

type backupResult struct {
	OK          bool     `json:"ok"`
	Local       string   `json:"local"`
	Second      string   `json:"second,omitempty"`
	SecondError string   `json:"second_error,omitempty"`
	Files       []string `json:"files"`
	Pruned      int      `json:"pruned"`
}

// backupCmd: media-core-engine backup [--to DIR] [--keep N] [--json], and backup list.
func backupCmd(args []string) {
	if len(args) > 0 && args[0] == "list" {
		backupListCmd(args[1:])
		return
	}
	fs := flag.NewFlagSet("backup", flag.ContinueOnError)
	to := fs.String("to", envOr("MEDIA_CORE_BACKUP_DIR", ""), "a second folder for a copy (a USB stick, a share)")
	keep := fs.Int("keep", backupKeep(), "how many nightly backups to keep in each place")
	asJSON := fs.Bool("json", false, "print one JSON object")
	if fs.Parse(args) != nil {
		os.Exit(exitUsage)
	}
	out := filepath.Join(backupDir(), "state-"+time.Now().Format("2006-01-02")+".tgz")
	files, err := writeBackup(out)
	if err != nil {
		failWith(exitFailed, *asJSON, "backup: "+err.Error())
	}
	res := backupResult{OK: true, Local: out, Files: files}
	res.Pruned = pruneBackups(backupDir(), "state-", *keep)
	if *to != "" {
		dst := filepath.Join(*to, "media-core-backup")
		// The folder itself must exist: an unplugged drive leaves an empty mount point, and making the
		// folder there would put the "second copy" on the same disk.
		err := statWithin(*to, 20*time.Second)
		if err == nil {
			err = mkdirWithin(dst, 20*time.Second)
		}
		if err == nil {
			err = copyFile(out, filepath.Join(dst, filepath.Base(out)))
		}
		if err == nil {
			res.Second = filepath.Join(dst, filepath.Base(out))
			res.Pruned += pruneBackups(dst, "state-", *keep)
		} else {
			res.SecondError = err.Error()
		}
	}
	if *asJSON {
		printJSON(res)
	} else {
		fmt.Printf("saved %s: %s\n", res.Local, strings.Join(files, ", "))
		if res.Second != "" {
			fmt.Println("copied to", res.Second)
		}
		if res.SecondError != "" {
			fmt.Fprintln(os.Stderr, "no second copy:", res.SecondError, "(the local copy is fine)")
		}
	}
	if res.SecondError != "" {
		os.Exit(exitPartial)
	}
}

func backupListCmd(args []string) {
	asJSON := len(args) > 0 && args[0] == "--json"
	names, _ := filepath.Glob(filepath.Join(backupDir(), "*.tgz"))
	sort.Slice(names, func(i, j int) bool { return backupTime(names[i]).After(backupTime(names[j])) })
	type item struct {
		Path    string `json:"path"`
		Size    int64  `json:"size"`
		Created int64  `json:"created"`
	}
	list := []item{}
	for _, n := range names {
		fi, err := os.Stat(n)
		if err == nil {
			list = append(list, item{n, fi.Size(), fi.ModTime().Unix()})
		}
	}
	if asJSON {
		printJSON(map[string]any{"backups": list})
		return
	}
	if len(list) == 0 {
		fmt.Println("No backups yet in", backupDir())
	}
	for _, x := range list {
		fmt.Printf("%s  %s  %d KB\n", time.Unix(x.Created, 0).Format("2006-01-02 15:04"), x.Path, x.Size>>10)
	}
}

// readBackup reads an archive's files by name, checked against its manifest. An archive with no manifest was
// made by the old shell script: its entries are flat (watch.json, tmdb/matches.json) and belong in data/.
func readBackup(p string) (map[string][]byte, *backupManifest, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, nil, fmt.Errorf("%s isn't a backup (%v)", p, err)
	}
	allowed := map[string]bool{}
	for _, e := range backupEntries() {
		allowed[e.name] = true
	}
	files := map[string][]byte{}
	var man *backupManifest
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, err
		}
		if h.Typeflag != tar.TypeReg {
			continue
		}
		name := path.Clean(strings.TrimPrefix(h.Name, "./"))
		b, err := io.ReadAll(io.LimitReader(tr, 256<<20))
		if err != nil {
			return nil, nil, err
		}
		switch {
		case name == "manifest.json":
			man = &backupManifest{}
			if err := json.Unmarshal(b, man); err != nil {
				return nil, nil, fmt.Errorf("damaged manifest: %v", err)
			}
		case strings.HasPrefix(name, "config/"): // the first Go backups
			files["conf/"+strings.TrimPrefix(name, "config/")] = b
		default:
			files[name] = b
		}
	}
	if man == nil { // the old script's flat archive
		flat := map[string][]byte{}
		for name, b := range files {
			flat["data/"+name] = b
		}
		files = flat
	}
	for name := range files {
		if !allowed[name] {
			return nil, nil, fmt.Errorf("the backup holds %q, which restore doesn't write", name)
		}
	}
	if man != nil {
		if man.Schema > stateSchema {
			return nil, nil, fmt.Errorf("this backup is from a newer version (schema %d); install that version to restore it", man.Schema)
		}
		for _, m := range man.Files {
			b, ok := files[m.Path]
			h := sha256.Sum256(b)
			if !ok || hex.EncodeToString(h[:]) != m.SHA256 {
				return nil, nil, fmt.Errorf("%s in the backup is damaged or missing", m.Path)
			}
		}
	}
	return files, man, nil
}

// restoreCmd: media-core-engine restore FILE [--yes]. Only while the server is stopped; the current files are
// saved first as pre-restore-<time>.tgz.
func restoreCmd(args []string) {
	fs := flag.NewFlagSet("restore", flag.ContinueOnError)
	yes := fs.Bool("yes", false, "don't ask")
	if fs.Parse(args) != nil || fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: media-core-engine restore FILE [--yes]   (media-core-engine backup list shows them)")
		os.Exit(exitUsage)
	}
	src := fs.Arg(0)
	if waitHello(time.Second, "") {
		failWith(exitBusy, false, "The server is running. Stop it first: systemctl --user stop media-core-engine")
	}
	files, _, err := readBackup(src)
	if err != nil {
		failWith(exitFailed, false, "restore: "+err.Error())
	}
	if !*yes {
		if !askYes(fmt.Sprintf("Replace the server's progress, settings and devices with %s?", src)) {
			failWith(exitFailed, false, "Nothing was changed.")
		}
	}
	pre := filepath.Join(backupDir(), fmt.Sprintf("pre-restore-%d.tgz", time.Now().Unix()))
	if _, err := writeBackup(pre); err == nil {
		fmt.Println("saved the current files as", pre)
	}
	for _, e := range backupEntries() {
		b, ok := files[e.name]
		if !ok {
			continue
		}
		dst := filepath.Join(e.base, e.rel)
		os.MkdirAll(filepath.Dir(dst), 0o700)
		if err := os.WriteFile(dst+".tmp", b, 0o600); err != nil {
			failWith(exitFailed, false, err.Error())
		}
		if err := os.Rename(dst+".tmp", dst); err != nil {
			failWith(exitFailed, false, err.Error())
		}
		fmt.Println("restored", e.name)
	}
	fmt.Println("Done. Start the server: systemctl --user start media-core-engine")
}

// askYes asks on the terminal; false when there's no one to ask.
func askYes(q string) bool {
	if fi, err := os.Stdin.Stat(); err != nil || fi.Mode()&os.ModeCharDevice == 0 {
		return false
	}
	fmt.Print(q + " [y/N] ")
	var b [64]byte
	n, _ := os.Stdin.Read(b[:])
	a := strings.ToLower(strings.TrimSpace(string(bytes.TrimSpace(b[:n]))))
	return a == "y" || a == "yes"
}
