package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"
)

// One schema number covers the whole data folder, in <data>/state.json. When a release changes the shape of
// a state file, it adds a migration and raises stateSchema; serve runs the pending ones at start, after a
// backup. Data written by a newer version is never touched. (devices.json, in the config folder, carries
// its own "v".)

const stateSchema = 1

type migration struct {
	to   int
	name string
	run  func() error // must be safe to run twice
}

var migrations = []migration{
	{1, "older job files: state 'starting' becomes 'queued'", migrateV1},
}

type stateFile struct {
	Schema   int    `json:"schema"`
	Engine   string `json:"engine"`
	Migrated int64  `json:"migrated"`
}

func statePath() string { return filepath.Join(DataDir, "state.json") }

// readSchema: the data folder's schema, and whether it has any data at all.
func readSchema() (schema int, hasData bool, err error) {
	b, err := os.ReadFile(statePath())
	if err == nil {
		var s stateFile
		if err := json.Unmarshal(b, &s); err != nil {
			return 0, true, fmt.Errorf("%s is damaged: %v", statePath(), err)
		}
		return s.Schema, true, nil
	}
	if !os.IsNotExist(err) {
		return 0, true, err
	}
	ents, _ := os.ReadDir(DataDir)
	for _, e := range ents {
		switch e.Name() {
		case "watch.json", "settings.json", "jobs", "intros.json", "tmdb":
			return 0, true, nil
		}
	}
	return 0, false, nil
}

func writeSchema(n int) error {
	b, _ := json.Marshal(stateFile{Schema: n, Engine: version, Migrated: time.Now().Unix()})
	if err := os.MkdirAll(DataDir, 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(statePath()+".tmp", b, 0o600); err != nil {
		return err
	}
	return os.Rename(statePath()+".tmp", statePath())
}

// migrateState brings the data folder up to stateSchema, or explains why it can't.
func migrateState() error {
	have, hasData, err := readSchema()
	if err != nil {
		return err
	}
	if !hasData {
		return writeSchema(stateSchema)
	}
	if have == stateSchema {
		return nil
	}
	if have > stateSchema {
		return fmt.Errorf("this data was written by a newer version of Media Core (schema %d; this one knows %d). Install that version, or restore an older backup: media-core-engine backup list", have, stateSchema)
	}
	bk := filepath.Join(backupDir(), fmt.Sprintf("upgrade-%dto%d-%d.tgz", have, stateSchema, time.Now().Unix()))
	if _, err := writeBackup(bk); err != nil {
		log.Printf("migrate: no backup before migrating (%v)", err)
	} else {
		log.Printf("migrate: saved %s first", bk)
		pruneBackups(backupDir(), "upgrade-", 5)
	}
	for _, m := range migrations {
		if m.to <= have {
			continue
		}
		log.Printf("migrate: schema %d: %s", m.to, m.name)
		if err := m.run(); err != nil {
			return fmt.Errorf("migrating to schema %d (%s): %v. Your data is as it was before this step; a backup is at %s", m.to, m.name, err, bk)
		}
		if err := writeSchema(m.to); err != nil {
			return err
		}
	}
	return nil
}

// migrateV1: job files from older versions may hold the state "starting"; it means "queued".
func migrateV1() error {
	ents, err := os.ReadDir(JobsDir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, e := range ents {
		if filepath.Ext(e.Name()) != ".json" {
			continue
		}
		p := filepath.Join(JobsDir, e.Name())
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		var j map[string]any
		if json.Unmarshal(b, &j) != nil || j["state"] != "starting" {
			continue
		}
		j["state"] = string(Queued)
		nb, _ := json.Marshal(j)
		if err := os.WriteFile(p+".tmp", nb, 0o600); err != nil {
			return err
		}
		if err := os.Rename(p+".tmp", p); err != nil {
			return err
		}
	}
	return nil
}
