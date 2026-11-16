package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMainCLIFetchStatusAndPostJSONErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/jobs":
			if r.URL.Query().Get("badjson") == "1" {
				w.Write([]byte("{bad"))
				return
			}
			w.WriteHeader(500)
		case "/api/quick":
			w.WriteHeader(400)
		}
	}))
	defer srv.Close()

	if _, err := fetchStatus(srv.URL); err == nil {
		t.Fatal("fetchStatus succeeded on HTTP 500")
	}
	badServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("{bad-json"))
	}))
	defer badServer.Close()
	if _, err := fetchStatus(badServer.URL); err == nil {
		t.Fatal("fetchStatus succeeded on invalid JSON")
	}
	var out map[string]any
	if err := postJSON(srv.URL, "/api/quick", map[string]string{"url": "x"}, &out); err == nil {
		t.Fatal("postJSON succeeded on HTTP 400")
	}
	if err := postJSON(badServer.URL, "/api/quick", map[string]string{"url": "x"}, &out); err == nil {
		t.Fatal("postJSON succeeded on invalid JSON")
	}
}
