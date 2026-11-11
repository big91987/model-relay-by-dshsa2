package api

import (
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// serveSPA serves the built React admin UI, falling back to index.html for
// client-side routes. When no build exists, it reports that clearly.
func (s *Server) serveSPA(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	root := s.cfg.WebDist
	indexPath := filepath.Join(root, "index.html")
	clean := path.Clean("/" + r.URL.Path)
	file := filepath.Join(root, filepath.FromSlash(clean))
	if rel, err := filepath.Rel(root, file); err != nil || strings.HasPrefix(rel, "..") {
		http.NotFound(w, r)
		return
	}
	if info, err := os.Stat(file); err == nil && !info.IsDir() {
		http.ServeFile(w, r, file)
		return
	}
	if _, err := os.Stat(indexPath); err == nil {
		http.ServeFile(w, r, indexPath)
		return
	}
	if clean == "/" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("web UI not built: run `cd web && pnpm build` or use the Vite dev server\n"))
		return
	}
	writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
}
