package main

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/xml"
	"io/fs"
	"log"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// A read-only WebDAV view of the library at /dav/ on the player port, so
// Infuse (or Finder, VLC, IINA) can browse and stream it. Only OPTIONS,
// PROPFIND, GET and HEAD; dot-folders (unpack scratch) are hidden. The
// password lives in ~/.config/media-core/webdav.pass (user "media-core").

const davPrefix = "/dav"
const davUser = "media-core"

type davHandler struct {
	root *os.Root
	pass string
}

func mountDAV(mux *http.ServeMux) {
	root, err := os.OpenRoot(Media)
	if err != nil {
		log.Printf("webdav: %v", err)
		return
	}
	h := &davHandler{root: root, pass: davPassword()}
	mux.Handle(davPrefix, h)
	mux.Handle(davPrefix+"/", h)
}

// davPassword reads the password file, creating a random one the first time.
func davPassword() string {
	f := filepath.Join(ConfDir, "webdav.pass")
	if b, err := os.ReadFile(f); err == nil {
		if s := strings.TrimSpace(string(b)); s != "" {
			return s
		}
	}
	b := make([]byte, 9)
	rand.Read(b)
	s := hex.EncodeToString(b)
	os.MkdirAll(ConfDir, 0o700)
	if err := os.WriteFile(f, []byte(s+"\n"), 0o600); err != nil {
		log.Printf("webdav: %v", err)
	}
	return s
}

func (h *davHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	u, p, ok := r.BasicAuth()
	if !ok || u != davUser || subtle.ConstantTimeCompare([]byte(p), []byte(h.pass)) != 1 {
		w.Header().Set("WWW-Authenticate", `Basic realm="Media Core"`)
		http.Error(w, "login required", http.StatusUnauthorized)
		return
	}
	name := strings.TrimPrefix(path.Clean("/"+strings.TrimPrefix(r.URL.Path, davPrefix)), "/")
	if name == "" {
		name = "."
	}
	for _, part := range strings.Split(name, "/") {
		if strings.HasPrefix(part, ".") && part != "." {
			http.NotFound(w, r)
			return
		}
	}
	switch r.Method {
	case "OPTIONS":
		w.Header().Set("DAV", "1")
		w.Header().Set("Allow", "OPTIONS, PROPFIND, GET, HEAD")
		w.WriteHeader(http.StatusOK)
	case "PROPFIND":
		h.propfind(w, r, name)
	case "GET", "HEAD":
		h.get(w, r, name)
	default:
		w.Header().Set("Allow", "OPTIONS, PROPFIND, GET, HEAD")
		http.Error(w, "read-only", http.StatusMethodNotAllowed)
	}
}

func (h *davHandler) get(w http.ResponseWriter, r *http.Request, name string) {
	f, err := h.root.Open(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.IsDir() {
		http.Error(w, "is a folder", http.StatusMethodNotAllowed)
		return
	}
	if VideoExt[ext(name)] {
		touchPlaying(r, "dav/"+name) // counts as playing, so background work pauses
	}
	http.ServeContent(w, r, st.Name(), st.ModTime(), f)
}

type davResponse struct {
	Href  string  `xml:"D:href"`
	Prop  davProp `xml:"D:propstat>D:prop"`
	State string  `xml:"D:propstat>D:status"`
}

type davProp struct {
	Name     string   `xml:"D:displayname"`
	Type     *davColl `xml:"D:resourcetype"`
	Length   *int64   `xml:"D:getcontentlength,omitempty"`
	MimeType string   `xml:"D:getcontenttype,omitempty"`
	Modified string   `xml:"D:getlastmodified"`
}

type davColl struct {
	Coll *struct{} `xml:"D:collection,omitempty"`
}

func davEntry(name string, fi fs.FileInfo) davResponse {
	href := davPrefix + "/"
	if name != "." {
		href += (&url.URL{Path: name}).EscapedPath()
	}
	p := davProp{Name: fi.Name(), Type: &davColl{}, Modified: fi.ModTime().UTC().Format(http.TimeFormat)}
	if name == "." {
		p.Name = "Media Core"
	}
	if fi.IsDir() {
		p.Type.Coll = &struct{}{}
		if !strings.HasSuffix(href, "/") {
			href += "/"
		}
	} else {
		n := fi.Size()
		p.Length = &n
		p.MimeType = mime.TypeByExtension(filepath.Ext(name))
		if p.MimeType == "" {
			p.MimeType = "application/octet-stream"
		}
	}
	return davResponse{Href: href, Prop: p, State: "HTTP/1.1 200 OK"}
}

func (h *davHandler) propfind(w http.ResponseWriter, r *http.Request, name string) {
	fi, err := h.root.Stat(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	out := []davResponse{davEntry(name, fi)}
	if fi.IsDir() && r.Header.Get("Depth") != "0" {
		ents, err := fs.ReadDir(h.root.FS(), name)
		if err != nil {
			http.Error(w, "can't read folder", http.StatusInternalServerError)
			return
		}
		for _, e := range ents {
			if strings.HasPrefix(e.Name(), ".") {
				continue
			}
			ci, err := e.Info()
			if err != nil {
				continue
			}
			out = append(out, davEntry(path.Join(name, e.Name()), ci))
		}
	}
	w.Header().Set("Content-Type", `application/xml; charset="utf-8"`)
	w.WriteHeader(http.StatusMultiStatus)
	w.Write([]byte(xml.Header))
	xml.NewEncoder(w).Encode(struct {
		XMLName xml.Name      `xml:"D:multistatus"`
		NS      string        `xml:"xmlns:D,attr"`
		R       []davResponse `xml:"D:response"`
	}{NS: "DAV:", R: out})
}
