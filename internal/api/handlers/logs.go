package handlers

import (
	"net/http"
	"os"
)

// GetLogs serves the log file for download (GET /v1/logs). Requires auth.
// The log file path comes from config.LogFile; if empty, returns 404.
func (h *Handler) GetLogs(w http.ResponseWriter, r *http.Request) {
	path := h.Mgr.Config().LogFile
	if path == "" {
		http.Error(w, "log file not configured", http.StatusNotFound)
		return
	}
	f, err := os.Open(path)
	if err != nil {
		http.Error(w, "log file unavailable", http.StatusInternalServerError)
		return
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		http.Error(w, "log file unavailable", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="pii-gateway.log"`)
	http.ServeContent(w, r, "pii-gateway.log", info.ModTime(), f)
}