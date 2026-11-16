package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// How the owner's devices reach the server, as Tailscale sees it. Away from home a
// device either reaches the server directly or falls back to Tailscale's relay,
// which carries about 10 Mb/s: enough to browse, not
// to play a film. That fallback is silent, so System shows it and the journal
// records it when a device is playing through the relay.

type Device struct {
	Name   string `json:"name"`
	OS     string `json:"os,omitempty"`
	Online bool   `json:"online"`
	Active bool   `json:"active"`          // talking to the server right now
	Direct bool   `json:"direct"`          // reaches the server itself, not by the relay
	Relay  string `json:"relay,omitempty"` // the relay it would use, or is using
	ips    []string
}

type devCall struct {
	done chan struct{}
	list []Device
}

var (
	devMu     sync.Mutex
	devAt     time.Time
	devList   []Device
	devFlight *devCall

	fetchDevices = readTailscaleDevices
)

// devices lists the other machines on the owner's Tailscale, from tailscaled (kept for 10 s).
func devices() []Device {
	devMu.Lock()
	if !devAt.IsZero() && time.Since(devAt) < 10*time.Second {
		list := devList
		devMu.Unlock()
		return list
	}
	if c := devFlight; c != nil {
		devMu.Unlock()
		<-c.done
		return c.list
	}
	c := &devCall{done: make(chan struct{})}
	devFlight = c
	devMu.Unlock()

	list := fetchDevices()

	devMu.Lock()
	devAt = time.Now()
	devList = list
	c.list = list
	if devFlight == c {
		devFlight = nil
	}
	close(c.done)
	devMu.Unlock()
	return list
}

func readTailscaleDevices() []Device {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "tailscale", "status", "--json").Output()
	if err != nil {
		return nil
	}
	var r struct {
		Peer map[string]struct {
			HostName     string
			OS           string
			Online       bool
			Active       bool
			CurAddr      string
			Relay        string
			TailscaleIPs []string
		}
	}
	if json.Unmarshal(out, &r) != nil {
		return nil
	}
	var list []Device
	for _, p := range r.Peer {
		list = append(list, Device{Name: p.HostName, OS: p.OS, Online: p.Online, Active: p.Active, Direct: p.CurAddr != "", Relay: p.Relay, ips: p.TailscaleIPs})
	}
	sort.Slice(list, func(a, b int) bool { return list[a].Name < list[b].Name })
	return list
}

// deviceOf names the machine an address belongs to: a Tailscale device, or the home network.
func deviceOf(host string) (name string, relayed bool) {
	for _, d := range devices() {
		for _, ip := range d.ips {
			if ip == host {
				return d.Name, d.Active && !d.Direct
			}
		}
	}
	if host == "127.0.0.1" || host == "::1" {
		return serverName() + " itself", false
	}
	return "Home network (" + host + ")", false
}

// Playing is one title being watched right now.
type Playing struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Device  string `json:"device"`
	Relayed bool   `json:"relayed,omitempty"` // through Tailscale's relay: it will stall
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
		pl.Device, pl.Relayed = deviceOf(host)
		out = append(out, pl)
	}
	return out
}

// watchRelay notes in the journal when something is playing through the relay, once per stretch.
func (p *Player) watchRelay() {
	said := map[string]bool{}
	for {
		time.Sleep(30 * time.Second)
		now := map[string]bool{}
		for _, pl := range p.playingList() {
			if pl.Relayed {
				now[pl.Device] = true
				if !said[pl.Device] {
					log.Printf("network: %s is playing through Tailscale's relay, not directly: expect stalls above about 10 Mb/s", pl.Device)
				}
			}
		}
		said = now
	}
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
