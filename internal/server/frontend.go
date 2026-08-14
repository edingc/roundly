package server

import (
	"errors"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// SPAHandler serves a built single-page app from fsys.
//
// Unknown paths fall back to index.html rather than 404, because client-side
// routes like /courses/abc exist only in the browser. Hashed asset filenames get
// a long cache lifetime; index.html is always revalidated so a deploy is picked
// up immediately.
func SPAHandler(fsys fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(fsys))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		requested := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if requested == "" || requested == "." {
			serveIndex(w, r, fsys)
			return
		}

		file, err := fsys.Open(requested)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				serveIndex(w, r, fsys)
				return
			}
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		stat, statErr := file.Stat()
		file.Close()
		if statErr != nil || stat.IsDir() {
			serveIndex(w, r, fsys)
			return
		}

		if strings.HasPrefix(requested, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		fileServer.ServeHTTP(w, r)
	})
}

func serveIndex(w http.ResponseWriter, r *http.Request, fsys fs.FS) {
	index, err := fs.ReadFile(fsys, "index.html")
	if err != nil {
		http.Error(w, "frontend not built", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(index)
	}
}
