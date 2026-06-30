package gateway

import (
	"embed"
	"net/http"
)

//go:embed static
var staticFS embed.FS

func (s *Server) handleDashboard(w http.ResponseWriter, _ *http.Request) {
	s.serveEmbeddedFile(w, "static/index.html", "text/html; charset=utf-8")
}

func (s *Server) handleDashboardScript(w http.ResponseWriter, _ *http.Request) {
	s.serveEmbeddedFile(w, "static/app.js", "text/javascript; charset=utf-8")
}

func (s *Server) handleDashboardStyles(w http.ResponseWriter, _ *http.Request) {
	s.serveEmbeddedFile(w, "static/styles.css", "text/css; charset=utf-8")
}

func (s *Server) handleDashboardManifest(w http.ResponseWriter, _ *http.Request) {
	s.serveEmbeddedFile(w, "static/manifest.webmanifest", "application/manifest+json")
}

func (s *Server) handleServiceWorker(w http.ResponseWriter, _ *http.Request) {
	s.serveEmbeddedFile(w, "static/sw.js", "text/javascript; charset=utf-8")
}

func (s *Server) serveEmbeddedFile(w http.ResponseWriter, path string, contentType string) {
	data, err := staticFS.ReadFile(path)
	if err != nil {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("Internal Server Error: " + err.Error()))
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}
