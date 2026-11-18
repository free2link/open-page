package main

import (
	"bytes"
	"crypto/hmac"
	"encoding/json"
	"fmt"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"
)

type liveVersion struct {
	Page    string `json:"page"`
	Version string `json:"version"`
}

func (a *Service) liveState(page string) liveVersion {
	s := a.store
	if page == "" {
		page = s.settings.DefaultPage
	}
	p, err := s.load(page)
	if err != nil || p.Private {
		return liveVersion{}
	}
	return liveVersion{p.ID, p.Current}
}

func (a *Service) events(w http.ResponseWriter, r *http.Request) {
	page := r.URL.Query().Get("page")
	if page != "" && !pageID.MatchString(page) {
		http.Error(w, "invalid page", http.StatusBadRequest)
		return
	}
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	rc := http.NewResponseController(w)
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	var previous liveVersion
	first := true
	for {
		a.store.mu.RLock()
		current := a.liveState(page)
		changed := a.store.changed
		a.store.mu.RUnlock()
		if first || current != previous {
			data, _ := json.Marshal(current)
			_ = rc.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if _, err := fmt.Fprintf(w, "event: version\ndata: %s\n\n", data); err != nil || rc.Flush() != nil {
				return
			}
			_ = rc.SetWriteDeadline(time.Time{})
			previous, first = current, false
		}
		select {
		case <-r.Context().Done():
			return
		case <-changed:
		case <-ticker.C:
			_ = rc.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if _, err := fmt.Fprint(w, ": keepalive\n\n"); err != nil || rc.Flush() != nil {
				return
			}
			_ = rc.SetWriteDeadline(time.Time{})
		}
	}
}

const liveScript = `(function () {
  var script = document.currentScript;
  if (!script || typeof EventSource === "undefined") return;
  var data = script.dataset;
  var events;
  function disconnect() {
    if (events) {
      events.close();
      events = null;
    }
  }
  function connect() {
    if (events || navigator.onLine === false) return;
    var connection = new EventSource("/events?page=" + encodeURIComponent(data.scope));
    events = connection;
    connection.addEventListener("version", function (event) {
      if (events !== connection) return;
      var active = JSON.parse(event.data);
      if (active.page !== data.page || active.version !== data.version) {
        disconnect();
        if (navigator.onLine !== false) location.reload();
      }
    });
  }
  connect();
  window.addEventListener("offline", disconnect);
  window.addEventListener("online", connect);
  window.addEventListener("pagehide", disconnect);
  window.addEventListener("pageshow", function (event) {
    if (event.persisted) connect();
  });
}());`

func addLiveScript(data []byte, state liveVersion, scope string) []byte {
	script := fmt.Sprintf(`<script src="/_openpage/live.js" data-page="%s" data-version="%s" data-scope="%s"></script>`, state.Page, state.Version, scope)
	tokens := html.NewTokenizer(bytes.NewReader(data))
	offset := 0
	for {
		t := tokens.Next()
		if t == html.ErrorToken {
			break
		}
		if t == html.EndTagToken {
			name, _ := tokens.TagName()
			if bytes.Equal(name, []byte("head")) || bytes.Equal(name, []byte("body")) {
				break
			}
		}
		offset += len(tokens.Raw())
	}
	out := make([]byte, 0, len(data)+len(script))
	out = append(out, data[:offset]...)
	out = append(out, script...)
	return append(out, data[offset:]...)
}

func isReserved(name string) bool {
	first, _, _ := strings.Cut(name, "/")
	switch first {
	case "admin", "api", "mcp", "events", "p", "d", "healthz", "_openpage":
		return true
	}
	return false
}

func (a *Service) registerPages(mux *http.ServeMux) {
	mux.HandleFunc("GET /d/{id}/{ticket}/{file...}", a.preview)
	mux.HandleFunc("GET /events", a.events)
	mux.HandleFunc("GET /_openpage/live.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		fmt.Fprint(w, liveScript)
	})
	mux.HandleFunc("GET /p/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !pageID.MatchString(r.PathValue("id")) {
			http.NotFound(w, r)
			return
		}
		http.Redirect(w, r, "/p/"+r.PathValue("id")+"/", http.StatusPermanentRedirect)
	})
	mux.HandleFunc("GET /p/{id}/{file...}", a.published)
	mux.HandleFunc("GET /{file...}", func(w http.ResponseWriter, r *http.Request) {
		if isReserved(r.PathValue("file")) {
			http.NotFound(w, r)
			return
		}
		a.published(w, r)
	})
}

func (a *Service) published(w http.ResponseWriter, r *http.Request) {
	scope := r.PathValue("id")
	s := a.store
	s.mu.RLock()
	state := a.liveState(scope)
	file := r.PathValue("file")
	var data []byte
	var err error
	if state.Version != "" {
		file = indexFile(file)
		data, err = s.readFile(pageDir(state.Page)+"/versions/"+state.Version, file)
	}
	s.mu.RUnlock()
	if state.Version == "" || err != nil {
		w.Header().Set("Cache-Control", "no-store")
		http.NotFound(w, r)
		return
	}
	if isHTML(file) {
		data = addLiveScript(data, state, scope)
	}
	serveResource(w, r, file, data)
}

func (a *Service) preview(w http.ResponseWriter, r *http.Request) {
	id, ticket := r.PathValue("id"), r.PathValue("ticket")
	expiry, signature, ok := strings.Cut(ticket, ".")
	unix, parseErr := strconv.ParseInt(expiry, 10, 64)
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Cache-Control", "no-store")
	if !ok || parseErr != nil || unix <= time.Now().Unix() {
		http.NotFound(w, r)
		return
	}
	file := indexFile(r.PathValue("file"))
	s := a.store
	s.mu.RLock()
	var data []byte
	p, err := s.load(id)
	if err == nil && hmac.Equal([]byte(signature), []byte(signPreview(s.settings.PreviewKey, p, expiry))) {
		data, err = s.readFile(pageDir(id)+"/"+p.Draft, file)
	} else {
		err = errMissing
	}
	s.mu.RUnlock()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	serveResource(w, r, file, data)
}

func indexFile(file string) string {
	if file == "" || strings.HasSuffix(file, "/") {
		return file + "index.html"
	}
	return file
}

func isHTML(file string) bool {
	ext := strings.ToLower(path.Ext(file))
	return ext == ".html" || ext == ".htm"
}

func serveResource(w http.ResponseWriter, r *http.Request, file string, data []byte) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.Header.Get("Service-Worker") == "script" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Security-Policy", "sandbox allow-scripts allow-forms allow-downloads")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("ETag", `"`+digest(data)+`"`)
	if w.Header().Get("Cache-Control") == "" {
		w.Header().Set("Cache-Control", "no-cache")
	}
	contentType := mime.TypeByExtension(path.Ext(file))
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", contentType)
	http.ServeContent(w, r, path.Base(file), time.Time{}, bytes.NewReader(data))
}
