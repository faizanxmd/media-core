package main

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// backup saves what the engine can't rebuild: watch progress, settings, intro data, title matches, the paired
// devices and engine.env. The library, the TMDB cache and thumbnails are left out (too big, or made again).
// Secrets (tmdb.key, aria2.conf) are left out too, so a copy on a shared drive gives nothing away; device keys
// are stored only as hashes. A dated copy goes in the data folder, and one to MEDIA_CORE_BACKUP_DIR when set
// (a USB stick or a network share), so it survives the main disk. The newest backupKeep of each are kept.

const backupKeep = 30

type backupFile struct{ base, rel, as string }

func backupFiles() []backupFile {
	return []backupFile{
		{DataDir, "watch.json", "data/watch.json"},
		{DataDir, "settings.json", "data/settings.json"},
		{DataDir, "intros.json", "data/intros.json"},
		{DataDir, "tmdb/matches.json", "data/tmdb/matches.json"},
		{ConfDir, "devices.json", "config/devices.json"},
		{ConfDir, "engine.env", "config/engine.env"},
	}
}

// makeBackup writes state-YYYY-MM-DD.tgz into dir (replacing today's) and returns its path.
func makeBackup(dir string, now time.Time) (string, []string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", nil, err
	}
	out := filepath.Join(dir, "state-"+now.Format("2006-01-02")+".tgz")
	f, err := os.OpenFile(out+".tmp", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return "", nil, err
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	var saved []string
	for _, b := range backupFiles() {
		src, err := os.Open(filepath.Join(b.base, b.rel))
		if err != nil {
			continue // not made yet on this server
		}
		fi, _ := src.Stat()
		err = tw.WriteHeader(&tar.Header{Name: b.as, Mode: 0o600, Size: fi.Size(), ModTime: fi.ModTime()})
		if err == nil {
			_, err = io.Copy(tw, src)
		}
		src.Close()
		if err != nil {
			f.Close()
			os.Remove(out + ".tmp")
			return "", nil, err
		}
		saved = append(saved, b.as)
	}
	err = tw.Close()
	if e := gz.Close(); err == nil {
		err = e
	}
	if e := f.Close(); err == nil {
		err = e
	}
	if err == nil && len(saved) == 0 {
		err = fmt.Errorf("nothing to back up in %s", DataDir)
	}
	if err != nil {
		os.Remove(out + ".tmp")
		return "", nil, err
	}
	return out, saved, os.Rename(out+".tmp", out)
}

// pruneBackups keeps the newest keep state-*.tgz in dir.
func pruneBackups(dir string, keep int) {
	names, _ := filepath.Glob(filepath.Join(dir, "state-*.tgz"))
	sort.Sort(sort.Reverse(sort.StringSlice(names))) // dated names sort by date
	for i, n := range names {
		if i >= keep {
			os.Remove(n)
		}
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
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(dst + ".tmp")
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Rename(dst+".tmp", dst)
}

// backupCmd: media-core-engine backup. Exit 1 if MEDIA_CORE_BACKUP_DIR is set but can't be written (the
// local copy is still made), so the timer shows as failed.
func backupCmd() {
	local := filepath.Join(DataDir, "backups", "state")
	out, saved, err := makeBackup(local, time.Now())
	if err != nil {
		fail("backup: " + err.Error())
	}
	pruneBackups(local, backupKeep)
	fmt.Printf("saved %s: %s\n", out, strings.Join(saved, ", "))
	dir := envOr("MEDIA_CORE_BACKUP_DIR", "")
	if dir == "" {
		return
	}
	dst := filepath.Join(dir, "media-core-backup")
	if err := os.MkdirAll(dst, 0o700); err == nil {
		err = copyFile(out, filepath.Join(dst, filepath.Base(out)))
		if err == nil {
			pruneBackups(dst, backupKeep)
			fmt.Printf("copied to %s\n", dst)
			return
		}
		fmt.Fprintf(os.Stderr, "backup: couldn't copy to %s: %v (the local copy is fine)\n", dst, err)
	} else {
		fmt.Fprintf(os.Stderr, "backup: %s isn't there (a drive not plugged in?): only the local copy was made\n", dir)
	}
	os.Exit(1)
}
