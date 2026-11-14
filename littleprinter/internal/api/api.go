// Package api exposes the print/status HTTP surface.
package api

import (
	"crypto/subtle"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"

	"littleprinter/internal/bridge"
	"littleprinter/internal/config"
	"littleprinter/internal/pixels"
)

type Server struct {
	cfg *config.Config
	hub *bridge.Hub
}

func New(cfg *config.Config, hub *bridge.Hub) *Server {
	return &Server{cfg: cfg, hub: hub}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /print", s.handlePrint)
	mux.HandleFunc("GET /status", s.handleStatus)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	return mux
}

func (s *Server) auth(r *http.Request) bool {
	tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if tok == "" {
		tok = r.Header.Get("X-Token")
	}
	if tok == "" {
		tok = r.URL.Query().Get("token")
	}
	return subtle.ConstantTimeCompare([]byte(tok), []byte(s.cfg.Token)) == 1
}

// handlePrint accepts a print job.
//
// Content types:
//   - image/png, image/jpeg: scaled/cropped to the paper width, dithered
//   - text/plain: wrapped and rendered with a bitmap font
//
// Query parameters:
//   - token: alternative to the Authorization header
//
// The job is queued automatically when the printer is offline and
// flushed when it reconnects; the response says which happened.
func (s *Server) handlePrint(w http.ResponseWriter, r *http.Request) {
	if !s.auth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 16<<20))
	if err != nil {
		http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if len(body) == 0 {
		http.Error(w, "empty body", http.StatusBadRequest)
		return
	}

	ct := r.Header.Get("Content-Type")
	var pix []bool
	var width int
	switch {
	case strings.HasPrefix(ct, "image/"):
		pix, width, err = pixels.FromImage(body)
	case strings.HasPrefix(ct, "text/"), ct == "":
		pix, width, err = pixels.FromText(string(body))
	default:
		http.Error(w, "unsupported content type "+ct, http.StatusUnsupportedMediaType)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	sent, id, err := s.hub.Print(pix, width)
	if err != nil {
		http.Error(w, "send: "+err.Error(), http.StatusInternalServerError)
		return
	}

	status := "sent"
	if !sent {
		status = "queued"
	}
	log.Printf("api: print id=%d status=%s", id, status)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status": status,
		"id":     id,
	})
}

type statusResponse struct {
	BridgeConnected bool   `json:"bridge_connected"`
	DeviceConnected bool   `json:"device_connected"`
	DeviceAddress   string `json:"device_address,omitempty"`
	LastSeen        string `json:"last_seen,omitempty"`
	QueuedPrint     bool   `json:"queued_print"`
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if !s.auth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	st := s.hub.Status()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(statusResponse{
		BridgeConnected: st.BridgeConnected,
		DeviceConnected: st.DeviceAddress != "",
		DeviceAddress:   st.DeviceAddress,
		LastSeen:        st.LastSeen,
		QueuedPrint:     st.QueuedPrint,
	})
}
