package server

import (
	"bytes"
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// The frontend build output (vite build writes to internal/server/dist).
// The artifacts are vendored in git so the Go build needs no node/npm;
// regenerate with `make frontend` when frontend sources change.
//
//go:embed all:dist
var distFS embed.FS

// site serves the embedded frontend build. HTML pages are rendered once at
// startup so they carry the deployment's base path (see Config.BasePath):
// a <base href> the frontend resolves its API, WebSocket and share URLs
// against, plus asset URLs rewritten to sit under the prefix.
type site struct {
	files      fs.FS
	fileServer http.Handler
	// pages holds the rendered HTML files keyed by their name in dist
	// ("index.html", "admin.html"). Empty when the frontend is not built.
	pages map[string][]byte
}

func newSite(basePath string) *site {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		panic(err)
	}
	s := &site{
		files:      sub,
		fileServer: http.FileServer(http.FS(sub)),
		pages:      map[string][]byte{},
	}
	entries, err := fs.ReadDir(sub, ".")
	if err != nil {
		return s
	}
	for _, e := range entries {
		if e.IsDir() || path.Ext(e.Name()) != ".html" {
			continue
		}
		data, err := fs.ReadFile(sub, e.Name())
		if err != nil {
			continue
		}
		s.pages[e.Name()] = renderPage(data, basePath)
	}
	return s
}

// renderPage rewrites one built HTML page for the base path. The <base href>
// is injected here rather than kept in the HTML sources so that the vite dev
// server, which serves those sources verbatim, resolves against the document
// URL instead.
func renderPage(data []byte, basePath string) []byte {
	out := bytes.Replace(data, []byte("<head>"), []byte(`<head><base href="`+basePath+`/">`), 1)
	if basePath != "" {
		// The build emits root-absolute asset URLs, which <base> does not
		// apply to.
		out = bytes.ReplaceAll(out, []byte(`="/assets/`), []byte(`="`+basePath+`/assets/`))
	}
	return out
}

// page returns a rendered HTML page, or nil when the frontend is not built.
func (s *site) page(name string) []byte { return s.pages[name] }

func (s *site) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if s.pages["index.html"] == nil {
		http.Error(w, "frontend not built: run `cd frontend && npm run build` and rebuild the server", http.StatusNotImplemented)
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/")
	if page := s.pages[name]; page != nil {
		writeHTML(w, page)
		return
	}
	if name != "" {
		if _, err := fs.Stat(s.files, name); err == nil {
			s.fileServer.ServeHTTP(w, r)
			return
		}
	}
	// The document id lives in the URL hash, so the root and any unknown
	// path serve the app shell.
	writeHTML(w, s.pages["index.html"])
}

func writeHTML(w http.ResponseWriter, page []byte) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(page)
}
