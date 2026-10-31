package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Job is persisted as ~/.local/share/media-core/jobs/<id>.json (same shape as the
// Python version, plus a few numeric fields).
type Job struct {
	ID          string   `json:"id"`
	Created     float64  `json:"created"`
	Updated     float64  `json:"updated"`
	Started     float64  `json:"started,omitempty"`
	Finished    float64  `json:"finished,omitempty"`
	State       string   `json:"state"`
	URL         string   `json:"url"`
	Filename    string   `json:"filename"`
	Size        *int64   `json:"size"`
	Connections int      `json:"connections"`
	Plan        Plan     `json:"plan"`
	Progress    int      `json:"progress"`
	Speed       string   `json:"speed"`
	ETA         string   `json:"eta"`
	DoneH       string   `json:"done_h,omitempty"`
	TotalH      string   `json:"total_h,omitempty"`
	Message     string   `json:"message"`
	Log         []string `json:"log"`
	Files       []string `json:"files,omitempty"`
	Dest        string   `json:"dest,omitempty"` // computed for the API, not trusted from disk

	GID        string `json:"gid,omitempty"`
	DoneBytes  int64  `json:"done_bytes,omitempty"`
	TotalBytes int64  `json:"total_bytes,omitempty"`
	SpeedBps   int64  `json:"speed_bps,omitempty"`
	EtaSec     int64  `json:"eta_s,omitempty"`
	Conns      int    `json:"conns,omitempty"`
	Downloaded bool   `json:"downloaded,omitempty"` // aria2 finished; retry goes straight to processing
	Placed     bool   `json:"placed,omitempty"`     // unpacked/moved into the library
	Phase      string `json:"phase,omitempty"`      // what the progress bar means while processing
	QueuePos   int    `json:"queue_pos"`
}

const (
	Queued      = "queued"
	Downloading = "downloading"
	Paused      = "paused"
	Processing  = "processing"
	Converting  = "converting"
	Done        = "done"
	Failed      = "failed"
	Cancelled   = "cancelled"
)

func (j *Job) inAria() bool {
	return j.State == Queued || j.State == Downloading || j.State == Paused
}
func (j *Job) busy() bool { return j.inAria() || j.State == Processing || j.State == Converting }

func now() float64 { return float64(time.Now().UnixMilli()) / 1000 }

func newID() string {
	b := make([]byte, 4)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// Store keeps every job in memory. State changes are written to disk at once;
// progress ticks only mark the job dirty and are flushed every 30 s, so the
// HDD isn't hit every second while downloading.
type Store struct {
	mu    sync.Mutex
	jobs  map[string]*Job
	dirty map[string]bool
	subs  map[chan struct{}]bool
}

func NewStore() *Store {
	os.MkdirAll(JobsDir, 0o755)
	s := &Store{jobs: map[string]*Job{}, dirty: map[string]bool{}, subs: map[chan struct{}]bool{}}
	ents, _ := os.ReadDir(JobsDir)
	for _, e := range ents {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(JobsDir, e.Name()))
		var j Job
		if err != nil || json.Unmarshal(b, &j) != nil || j.ID == "" {
			log.Printf("skipping bad job file %s", e.Name())
			continue
		}
		if j.State == "starting" { // Python-era state
			j.State = Queued
		}
		s.jobs[j.ID] = &j
	}
	go func() {
		for range time.Tick(30 * time.Second) {
			s.Flush()
		}
	}()
	return s
}

func (s *Store) write(j *Job) {
	j.Updated = now()
	b, _ := json.MarshalIndent(j, "", " ")
	p := filepath.Join(JobsDir, j.ID+".json")
	if err := os.WriteFile(p+".tmp", b, 0o644); err != nil {
		log.Printf("save %s: %v", j.ID, err)
		return
	}
	os.Rename(p+".tmp", p)
	delete(s.dirty, j.ID)
}

func (s *Store) Flush() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id := range s.dirty {
		if j := s.jobs[id]; j != nil {
			s.write(j)
		}
	}
}

func (s *Store) Add(j *Job) {
	s.mu.Lock()
	s.jobs[j.ID] = j
	s.write(j)
	s.mu.Unlock()
	s.Notify()
}

// Update applies fn to a job. persist=false is for progress ticks.
func (s *Store) Update(id string, persist bool, fn func(*Job)) (Job, bool) {
	s.mu.Lock()
	j, ok := s.jobs[id]
	if !ok {
		s.mu.Unlock()
		return Job{}, false
	}
	fn(j)
	if persist {
		s.write(j)
	} else {
		s.dirty[id] = true
	}
	cp := *j
	s.mu.Unlock()
	s.Notify()
	return cp, true
}

func (s *Store) Remove(id string) {
	s.mu.Lock()
	delete(s.jobs, id)
	delete(s.dirty, id)
	os.Remove(filepath.Join(JobsDir, id+".json"))
	s.mu.Unlock()
	s.Notify()
}

func (s *Store) Get(id string) (Job, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, ok := s.jobs[id]
	if !ok {
		return Job{}, false
	}
	return *j, true
}

// List returns copies, newest first.
func (s *Store) List() []Job {
	s.mu.Lock()
	out := make([]Job, 0, len(s.jobs))
	for _, j := range s.jobs {
		cp := *j
		cp.Log = append([]string(nil), j.Log...)
		out = append(out, cp)
	}
	s.mu.Unlock()
	sort.Slice(out, func(a, b int) bool { return out[a].Created > out[b].Created })
	return out
}

func (s *Store) Subscribe() chan struct{} {
	ch := make(chan struct{}, 1)
	s.mu.Lock()
	s.subs[ch] = true
	s.mu.Unlock()
	return ch
}

func (s *Store) Unsubscribe(ch chan struct{}) {
	s.mu.Lock()
	delete(s.subs, ch)
	s.mu.Unlock()
}

// Notify wakes every SSE client; a pending wake-up is never duplicated.
func (s *Store) Notify() {
	s.mu.Lock()
	for ch := range s.subs {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
	s.mu.Unlock()
}

func logLine(j *Job, msg string) {
	log.Printf("[%s] %s", j.ID, msg)
	j.Log = append(j.Log, time.Now().Format("15:04 ")+msg)
	if len(j.Log) > 40 {
		j.Log = j.Log[len(j.Log)-40:]
	}
	j.Message = msg
}
