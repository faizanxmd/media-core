package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// Aria2 talks JSON-RPC to the long-running aria2c daemon (media-core-aria2.service).
// Downloads live there, so they survive restarts of the web app.
type Aria2 struct {
	endpoint, token string
	hc              *http.Client
}

func NewAria2() (*Aria2, error) {
	f, err := os.Open(Aria2Conf)
	if err != nil {
		return nil, fmt.Errorf("aria2 config: %w", err)
	}
	defer f.Close()
	a := &Aria2{hc: &http.Client{Timeout: 10 * time.Second}}
	port := "6800"
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		k, v, _ := strings.Cut(strings.TrimSpace(sc.Text()), "=")
		switch k {
		case "rpc-secret":
			a.token = v
		case "rpc-listen-port":
			port = v
		}
	}
	if ariaPort > 0 {
		port = strconv.Itoa(ariaPort)
	}
	a.endpoint = "http://127.0.0.1:" + port + "/jsonrpc"
	return a, nil
}

type rpcErr struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcErr) Error() string { return e.Message }

func (a *Aria2) call(ctx context.Context, method string, out any, params ...any) error {
	body, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": "m", "method": "aria2." + method,
		"params": append([]any{"token:" + a.token}, params...),
	})
	req, _ := http.NewRequestWithContext(ctx, "POST", a.endpoint, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var r struct {
		Result json.RawMessage `json:"result"`
		Error  *rpcErr         `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return err
	}
	if r.Error != nil {
		return r.Error
	}
	if out != nil {
		return json.Unmarshal(r.Result, out)
	}
	return nil
}

type AStatus struct {
	GID             string `json:"gid"`
	Status          string `json:"status"` // active waiting paused error complete removed
	TotalLength     string `json:"totalLength"`
	CompletedLength string `json:"completedLength"`
	DownloadSpeed   string `json:"downloadSpeed"`
	Connections     string `json:"connections"`
	ErrorCode       string `json:"errorCode"`
	ErrorMessage    string `json:"errorMessage"`
	Files           []struct {
		Path string `json:"path"`
	} `json:"files"`
	pos int // place in aria2's waiting queue
}

func i64(s string) int64 { n, _ := strconv.ParseInt(s, 10, 64); return n }

func (s AStatus) Path() string {
	if len(s.Files) > 0 {
		return s.Files[0].Path
	}
	return ""
}

var statusKeys = []string{"gid", "status", "totalLength", "completedLength", "downloadSpeed",
	"connections", "errorCode", "errorMessage", "files"}

// All returns every download aria2 knows about, keyed by gid.
func (a *Aria2) All(ctx context.Context) (map[string]AStatus, error) {
	out := map[string]AStatus{}
	for _, q := range []struct {
		m string
		p []any
	}{{"tellActive", []any{statusKeys}}, {"tellWaiting", []any{0, 1000, statusKeys}}, {"tellStopped", []any{0, 1000, statusKeys}}} {
		var list []AStatus
		if err := a.call(ctx, q.m, &list, q.p...); err != nil {
			return nil, err
		}
		for i, s := range list {
			s.pos = i
			out[s.GID] = s
		}
	}
	return out, nil
}

func (a *Aria2) Add(ctx context.Context, url string, opts map[string]string) (string, error) {
	var gid string
	err := a.call(ctx, "addUri", &gid, []string{url}, opts)
	return gid, err
}

func (a *Aria2) Status(ctx context.Context, gid string) (AStatus, error) {
	var s AStatus
	err := a.call(ctx, "tellStatus", &s, gid, statusKeys)
	return s, err
}

func (a *Aria2) Do(ctx context.Context, method, gid string) error {
	return a.call(ctx, method, nil, gid)
}

func (a *Aria2) Forget(ctx context.Context, gid string) {
	a.call(ctx, "removeDownloadResult", nil, gid)
}

func (a *Aria2) SetGlobal(ctx context.Context, opts map[string]string) error {
	return a.call(ctx, "changeGlobalOption", nil, opts)
}

func (a *Aria2) Top(ctx context.Context, gid string) error {
	return a.call(ctx, "changePosition", nil, gid, 0, "POS_SET")
}

// Session changes every time aria2 starts.
func (a *Aria2) Session(ctx context.Context) string {
	var r struct {
		SessionID string `json:"sessionId"`
	}
	a.call(ctx, "getSessionInfo", &r)
	return r.SessionID
}

func (a *Aria2) Speed(ctx context.Context) int64 {
	var st struct {
		DownloadSpeed string `json:"downloadSpeed"`
	}
	if a.call(ctx, "getGlobalStat", &st) != nil {
		return 0
	}
	return i64(st.DownloadSpeed)
}

// gone reports that aria2 no longer knows the gid.
func gone(err error) bool {
	var re *rpcErr
	return errors.As(err, &re) && strings.Contains(re.Message, "not found")
}

// errorHint turns an aria2 error into something a person can act on.
func errorHint(s AStatus) string {
	codes := map[string]string{"1": "unknown error", "2": "timed out", "3": "file not found (404)",
		"6": "network problem", "9": "not enough disk space", "13": "file already exists",
		"19": "DNS lookup failed", "22": "server refused (403/429 or bad response)",
		"24": "authorization needed"}
	hint := codes[s.ErrorCode]
	if m := reStatus.FindStringSubmatch(s.ErrorMessage); m != nil {
		hint = "server answered HTTP " + m[1]
	} else if hint == "" {
		hint = strings.TrimSpace(s.ErrorMessage)
		if hint == "" {
			hint = "aria2 error " + s.ErrorCode
		}
	}
	if reExpired.MatchString(hint) {
		hint += " (link probably expired: paste a fresh link on this job to resume)"
	}
	return hint
}
