package server

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

// The frontend build output (vite build writes to internal/server/dist).
// The artifacts are vendored in git so the Go build needs no node/npm;
// regenerate with `make frontend` when frontend sources change.
//
//go:embed all:dist
var distFS embed.FS

func staticHandler() http.Handler {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		panic(err)
	}
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "frontend not built: run `cd frontend && npm run build` and rebuild the server", http.StatusNotImplemented)
		})
	}
	fileServer := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path != "" {
			if _, err := fs.Stat(sub, path); err != nil {
				// SPA fallback: unknown paths serve the app shell.
				r.URL.Path = "/"
			}
		}
		fileServer.ServeHTTP(w, r)
	})
}
