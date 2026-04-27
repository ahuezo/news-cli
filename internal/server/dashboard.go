package server

import (
	_ "embed"
	"net/http"
)

//go:embed static/dashboard.html
var dashboardHTML []byte

func (a *App) dashboard(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(dashboardHTML)
}
