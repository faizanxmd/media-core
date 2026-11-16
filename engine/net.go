package main

import (
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

// deviceOf names the machine an address belongs to: this machine or the home network.
func deviceOf(host string) string {
	if host == "127.0.0.1" || host == "::1" {
		return serverName() + " itself"
	}
	return "Home network (" + host + ")"
}

// Playing is one title being watched right now.
type Playing struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Device string `json:"device"`
}

func (p *Player) playingList() []Playing {
	playMu.Lock()
	var keys []string
	for k, t := range playing {
		if time.Since(t) < 45*time.Second {
			keys = append(keys, k)
		}
	}
	playMu.Unlock()
	sort.Strings(keys)
	var out []Playing
	for _, k := range keys {
		i := strings.LastIndex(k, "/")
		host, id := k[:i], k[i+1:]
		pl := Playing{ID: id, Title: id}
		if it := p.c.Get(id); it != nil {
			pl.Title = it.Title
			if it.Show != "" {
				pl.Title = it.Show + " · S" + strconv.Itoa(it.Season) + ", E" + strconv.Itoa(it.Episode)
			} else if it.Meta != nil && it.Meta.Title != "" {
				pl.Title = it.Meta.Title
			}
		}
		pl.Device = deviceOf(host)
		out = append(out, pl)
	}
	return out
}

// loadAvg: the server's one-minute load and how many cores it has to carry it.
func loadAvg() (float64, int) {
	b, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0, runtime.NumCPU()
	}
	f := strings.Fields(string(b))
	if len(f) == 0 {
		return 0, runtime.NumCPU()
	}
	l, _ := strconv.ParseFloat(f[0], 64)
	return l, runtime.NumCPU()
}
