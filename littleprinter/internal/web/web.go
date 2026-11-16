// Package web serves the minimal print UI (text and image).
package web

import (
	_ "embed"
	"net/http"
)

//go:embed index.html
var indexHTML []byte

// Routes registers the UI routes on the given mux.
func Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(indexHTML)
	})
}
