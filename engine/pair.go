package main

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Pairing, the way a TV signs in to a streaming service: the new device asks for a code and shows it; someone
// on an admin device (or the server's command line) approves the code; the new device, which keeps asking
// with a secret only it knows, then collects its key. Seeing the code on a screen isn't enough to get the key.
//
// The first admin is made with the setup code the installer prints, once, before anything else works.

const (
	pairTTL        = 10 * time.Minute
	pairMaxPending = 5
	pairStartsPerM = 10
	codeAlphabet   = "23456789ABCDEFGHJKMNPQRSTUVWXYZ" // 31 letters: no 0/O, 1/I/L
)

type pendingPair struct {
	Code    string
	poll    string
	Name    string
	Kind    string
	IP      string
	Created time.Time
	state   string // pending, approved, denied
	token   string // set on approval, handed over once
	role    string
}

type pairing struct {
	mu     sync.Mutex
	byCode map[string]*pendingPair
	starts map[string][]time.Time // per address, for the rate limit
	dev    *devStore
	auth   *authGate

	setupPath string
	tries     int
	lockUntil time.Time
}

func newPairing(dev *devStore, a *authGate) *pairing {
	return &pairing{byCode: map[string]*pendingPair{}, starts: map[string][]time.Time{}, dev: dev, auth: a,
		setupPath: filepath.Join(ConfDir, "setup.code")}
}

// randCode is n letters from codeAlphabet, each equally likely (bytes past the last whole multiple are skipped).
func randCode(n int) string {
	limit := byte(256 - 256%len(codeAlphabet))
	out := make([]byte, 0, n)
	buf := make([]byte, 32)
	for len(out) < n {
		if _, err := rand.Read(buf); err != nil {
			panic(err)
		}
		for _, c := range buf {
			if c < limit && len(out) < n {
				out = append(out, codeAlphabet[int(c)%len(codeAlphabet)])
			}
		}
	}
	return string(out)
}

// normCode: "k7m 2xq", "K7M-2XQ" -> "K7M2XQ".
func normCode(s string) string {
	var b strings.Builder
	for _, c := range strings.ToUpper(s) {
		if strings.ContainsRune(codeAlphabet, c) {
			b.WriteRune(c)
		}
	}
	return b.String()
}

func showCode(c string) string {
	if len(c) == 6 {
		return c[:3] + "-" + c[3:]
	}
	if len(c) == 8 {
		return c[:4] + "-" + c[4:]
	}
	return c
}

func (p *pairing) expireLocked() {
	for k, v := range p.byCode {
		if time.Since(v.Created) > pairTTL {
			delete(p.byCode, k)
		}
	}
	for ip, ts := range p.starts {
		if len(ts) == 0 || time.Since(ts[len(ts)-1]) > time.Minute {
			delete(p.starts, ip)
		}
	}
}

func cleanName(s, def string) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > 60 {
		s = string(r[:60])
	}
	if s == "" {
		return def
	}
	return s
}

// sitePort is the port of the site (where a code is approved), for links shown on a TV.
func sitePort() string { return portOf(listenAddr(), "8780") }

func (p *pairing) start(w http.ResponseWriter, r *http.Request) {
	var b struct{ Name, Kind string }
	if !readJSON(w, r, &b) {
		return
	}
	switch b.Kind {
	case "tv", "phone", "web":
	default:
		errJSON(w, 400, "kind must be tv, phone or web")
		return
	}
	ip := clientIP(r).String()
	p.mu.Lock()
	defer p.mu.Unlock()
	p.expireLocked()
	recent := p.starts[ip][:0]
	for _, t := range p.starts[ip] {
		if time.Since(t) < time.Minute {
			recent = append(recent, t)
		}
	}
	p.starts[ip] = recent
	if len(recent) >= pairStartsPerM {
		errJSON(w, 429, "Too many codes asked for. Wait a minute.")
		return
	}
	if len(p.byCode) >= pairMaxPending {
		errJSON(w, 429, "Too many devices are waiting to pair. Approve or wait for one, then try again.")
		return
	}
	p.starts[ip] = append(p.starts[ip], time.Now())
	code := randCode(6)
	for p.byCode[code] != nil {
		code = randCode(6)
	}
	def := map[string]string{"tv": "TV", "phone": "Phone", "web": "Browser"}[b.Kind]
	pp := &pendingPair{Code: code, poll: randToken(32), Name: cleanName(b.Name, def), Kind: b.Kind, IP: ip,
		Created: time.Now(), state: "pending"}
	p.byCode[code] = pp
	host, _, err := net.SplitHostPort(r.Host)
	if err != nil {
		host = r.Host
	}
	writeJSON(w, 200, map[string]any{"code": showCode(code), "poll": pp.poll, "expires_in": int(pairTTL.Seconds()),
		"interval": 2, "url": "http://" + net.JoinHostPort(host, sitePort()) + "/#/pair/" + code})
}

func (p *pairing) poll(w http.ResponseWriter, r *http.Request) {
	var b struct{ Poll string }
	if !readJSON(w, r, &b) {
		return
	}
	p.mu.Lock()
	p.expireLocked()
	var pp *pendingPair
	for _, v := range p.byCode {
		if subtle.ConstantTimeCompare([]byte(v.poll), []byte(b.Poll)) == 1 {
			pp = v
			break
		}
	}
	if pp == nil {
		p.mu.Unlock()
		writeJSON(w, 404, map[string]any{"state": "expired"})
		return
	}
	state, tok, role, name, kind := pp.state, pp.token, pp.role, pp.Name, pp.Kind
	if state != "pending" {
		delete(p.byCode, pp.Code) // the answer is given once
	}
	p.mu.Unlock()
	switch state {
	case "approved":
		if kind == "web" {
			setSessionCookie(w, tok)
		}
		writeJSON(w, 200, map[string]any{"state": "approved", "token": tok, "role": role, "name": name})
	default:
		writeJSON(w, 200, map[string]any{"state": state})
	}
}

func (p *pairing) find(code string) *pendingPair {
	p.expireLocked()
	return p.byCode[normCode(code)]
}

func (p *pairing) info(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	defer p.mu.Unlock()
	pp := p.find(r.PathValue("code"))
	if pp == nil || pp.state != "pending" {
		errJSON(w, 404, "No device is waiting with that code. Codes last 10 minutes.")
		return
	}
	writeJSON(w, 200, map[string]any{"code": showCode(pp.Code), "name": pp.Name, "kind": pp.Kind, "address": pp.IP,
		"age_s": int(time.Since(pp.Created).Seconds())})
}

// approve gives the waiting device its key. by is who approved it, for the log.
func (p *pairing) approve(code, name, role, by string) (*pendingPair, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	pp := p.find(code)
	if pp == nil || pp.state != "pending" {
		return nil, fmt.Errorf("No device is waiting with that code. Codes last 10 minutes.")
	}
	if role != roleAdmin {
		role = rolePlayer
	}
	tok, _, err := p.dev.add(cleanName(name, pp.Name), pp.Kind, role)
	if err != nil {
		return nil, err
	}
	pp.state, pp.token, pp.role = "approved", tok, role
	pp.Name = cleanName(name, pp.Name)
	log.Printf("devices: %s paired (%s, %s), approved by %s", pp.Name, pp.Kind, role, by)
	c := *pp
	return &c, nil
}

func (p *pairing) deny(code string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	pp := p.find(code)
	if pp == nil || pp.state != "pending" {
		return false
	}
	pp.state = "denied"
	return true
}

// ---------- the setup code ----------

type setupFile struct {
	Hash    string `json:"hash"`
	Expires int64  `json:"expires"`
}

// newSetupCode makes a fresh setup code (the old one stops working), valid for a day, and returns it.
func newSetupCode(path string) (string, error) {
	code := randCode(8)
	b, _ := json.Marshal(setupFile{Hash: hashToken(code), Expires: time.Now().Add(24 * time.Hour).Unix()})
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	if err := os.WriteFile(path+".tmp", b, 0o600); err != nil {
		return "", err
	}
	return showCode(code), os.Rename(path+".tmp", path)
}

func (p *pairing) setupState(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	locked := time.Until(p.lockUntil)
	p.mu.Unlock()
	out := map[string]any{"claimed": p.dev.claimed(), "name": serverName()}
	if locked > 0 {
		out["locked_s"] = int(locked.Seconds())
	}
	writeJSON(w, 200, out)
}

// claim turns the setup code into the first admin: this browser.
func (p *pairing) claim(w http.ResponseWriter, r *http.Request) {
	var b struct{ Code, Name string }
	if !readJSON(w, r, &b) {
		return
	}
	if p.dev.claimed() {
		errJSON(w, 409, "This server is already set up. Pair this browser from a device that's signed in.")
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if time.Now().Before(p.lockUntil) {
		errJSON(w, 429, fmt.Sprintf("Too many wrong codes. Try again in %d minutes.", int(time.Until(p.lockUntil).Minutes())+1))
		return
	}
	var f setupFile
	raw, err := os.ReadFile(p.setupPath)
	if err == nil {
		err = json.Unmarshal(raw, &f)
	}
	ok := err == nil && time.Now().Unix() < f.Expires &&
		subtle.ConstantTimeCompare([]byte(f.Hash), []byte(hashToken(normCode(b.Code)))) == 1
	if !ok {
		p.tries++
		if p.tries >= 10 {
			p.tries, p.lockUntil = 0, time.Now().Add(15*time.Minute)
		}
		errJSON(w, 403, "That code doesn't match. On the server, run: media-core-engine setup-code")
		return
	}
	tok, d, err := p.dev.add(cleanName(b.Name, "Browser"), "web", roleAdmin)
	if err != nil {
		errJSON(w, 500, err.Error())
		return
	}
	os.Remove(p.setupPath)
	p.tries = 0
	log.Printf("devices: set up; %s is the first admin", d.Name)
	setSessionCookie(w, tok)
	writeJSON(w, 200, map[string]any{"ok": true, "name": d.Name})
}

// ---------- routes ----------

// mountPairing adds what a device uses to pair (both ports) and, with admin, setup and device management (the site).
func (p *pairing) mount(mux *http.ServeMux, admin bool) {
	mux.HandleFunc("POST /api/v1/pair/start", p.start)
	mux.HandleFunc("POST /api/v1/pair/poll", p.poll)
	// me: which device this is, and what it may do (a home device let in by MEDIA_CORE_OPEN_HOME counts as admin)
	mux.HandleFunc("GET /api/v1/me", func(w http.ResponseWriter, r *http.Request) {
		d := deviceFrom(r)
		if d == nil {
			writeJSON(w, 200, map[string]any{"name": "This device", "role": roleAdmin, "kind": "", "open": true})
			return
		}
		writeJSON(w, 200, map[string]any{"id": d.ID, "name": d.Name, "role": d.Role, "kind": d.Kind})
	})
	if !admin {
		return
	}
	mux.HandleFunc("GET /api/setup/state", p.setupState)
	mux.HandleFunc("POST /api/setup/claim", p.claim)
	mux.HandleFunc("GET /api/pair/{code}", p.info)
	mux.HandleFunc("POST /api/pair/approve", func(w http.ResponseWriter, r *http.Request) {
		var b struct{ Code, Name, Role string }
		if !readJSON(w, r, &b) {
			return
		}
		by := "?"
		if d := deviceFrom(r); d != nil {
			by = d.Name
		}
		pp, err := p.approve(b.Code, b.Name, b.Role, by)
		if err != nil {
			errJSON(w, 404, err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "name": pp.Name, "role": pp.role})
	})
	mux.HandleFunc("POST /api/pair/deny", func(w http.ResponseWriter, r *http.Request) {
		var b struct{ Code string }
		if !readJSON(w, r, &b) {
			return
		}
		writeJSON(w, 200, map[string]any{"ok": p.deny(b.Code)})
	})
	mux.HandleFunc("GET /api/devices", func(w http.ResponseWriter, r *http.Request) {
		me := ""
		if d := deviceFrom(r); d != nil {
			me = d.ID
		}
		p.mu.Lock()
		p.expireLocked()
		waiting := []map[string]any{}
		for _, v := range p.byCode {
			if v.state == "pending" {
				waiting = append(waiting, map[string]any{"code": showCode(v.Code), "name": v.Name, "kind": v.Kind, "address": v.IP})
			}
		}
		p.mu.Unlock()
		writeJSON(w, 200, map[string]any{"devices": p.dev.views(me), "waiting": waiting})
	})
	mux.HandleFunc("POST /api/devices/{id}/revoke", func(w http.ResponseWriter, r *http.Request) {
		if !p.dev.revoke(r.PathValue("id")) {
			errJSON(w, 404, "no such device")
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true})
	})
}
