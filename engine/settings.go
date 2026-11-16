package main

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"sync"
)

type Settings struct {
	LimitMBps     float64 `json:"limit_mbps"`     // 0 = unlimited
	MaxConcurrent int     `json:"max_concurrent"` // parallel downloads
	AvgBps        float64 `json:"avg_bps"`        // learned from finished downloads, for ETAs
}

func (s Settings) bps() float64 {
	if s.AvgBps > 100_000 {
		return s.AvgBps
	}
	return DefaultMBps * 1e6
}

type settingsStore struct {
	mu sync.Mutex
	s  Settings
}

var settingsPath = filepath.Join(DataDir, "settings.json")

func loadSettings() *settingsStore {
	st := &settingsStore{s: Settings{MaxConcurrent: 1}}
	if b, err := os.ReadFile(settingsPath); err == nil {
		json.Unmarshal(b, &st.s)
	}
	return st
}

func (st *settingsStore) get() Settings {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.s
}

func (st *settingsStore) update(fn func(*Settings)) Settings {
	st.mu.Lock()
	defer st.mu.Unlock()
	fn(&st.s)
	s := &st.s
	s.MaxConcurrent = min(max(s.MaxConcurrent, 1), 3)
	s.LimitMBps = max(s.LimitMBps, 0)
	b, err := json.MarshalIndent(st.s, "", " ")
	if err != nil {
		log.Printf("settings: marshal: %v", err)
		return st.s
	}
	if err := os.WriteFile(settingsPath+".tmp", b, 0o600); err != nil {
		log.Printf("settings: write %s: %v", settingsPath, err)
		return st.s
	}
	if err := os.Rename(settingsPath+".tmp", settingsPath); err != nil {
		os.Remove(settingsPath + ".tmp")
		log.Printf("settings: rename %s: %v", settingsPath, err)
	}
	return st.s
}

func itoa(n int) string { return strconv.Itoa(n) }

// limitOpt formats MB/s for aria2 ("0" = unlimited).
func limitOpt(mbps float64) string {
	if mbps <= 0 {
		return "0"
	}
	return strconv.Itoa(int(mbps*1000)) + "K"
}
