// Package api exposes the print/status HTTP surface.
package api

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"littleprinter/internal/bridge"
	"littleprinter/internal/config"
	"littleprinter/internal/htmlr"
	"littleprinter/internal/pixels"
	"littleprinter/internal/sudoku"
	"littleprinter/internal/web"
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
	web.Routes(mux)
	mux.HandleFunc("POST /print", s.handlePrint)
	mux.HandleFunc("POST /print/sudoku", s.handleSudokuPrint)
	mux.HandleFunc("GET /print/sudoku", s.handleSudokuPrint) // convenience: GET also prints
	mux.HandleFunc("GET /sudoku.png", s.handleSudokuPNG)
	mux.HandleFunc("GET /status", s.handleStatus)
	mux.HandleFunc("POST /personality", s.handlePersonalityUpload)
	mux.HandleFunc("POST /personality/push", s.handlePersonalityPush)
	mux.HandleFunc("GET /personality", s.handlePersonalityStatus)
	// The BERG Cloud Bridge is hard-coded to connect to /api/v1/connection
	// (any path in its configured server URL is ignored). /bridge is the
	// documented alias.
	mux.HandleFunc("GET /bridge", s.hub.HandleWS)
	mux.HandleFunc("GET /api/v1/connection", s.hub.HandleWS)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	return mux
}

// handleSudokuPrint generates and prints the daily sudoku. The puzzle
// is derived from the date (and difficulty), so it is deterministic:
// same day, same puzzle. Optional query params: difficulty=easy|medium|hard,
// date=YYYY-MM-DD (for reprints), solution=1 (print the solution).
func (s *Server) handleSudokuPrint(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	diff, err := sudoku.DifficultyByName(q.Get("difficulty"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	date := q.Get("date")
	if date == "" {
		date = time.Now().Format("2006-01-02")
	} else if _, err := time.Parse("2006-01-02", date); err != nil {
		http.Error(w, "invalid date (want YYYY-MM-DD): "+err.Error(), http.StatusBadRequest)
		return
	}
	solution := q.Get("solution") == "1"

	puzzle, solved := sudoku.Generate(sudoku.SeedFor(date, diff.Name), diff.Givens)
	grid := puzzle
	if solution {
		grid = solved
	}
	pix, width, err := pixels.FromImage(sudoku.RenderPNG(&grid, date, diff.Name, solution))
	if err != nil {
		http.Error(w, "render: "+err.Error(), http.StatusInternalServerError)
		return
	}

	withFace := q.Get("face") != "no"
	sent, id, err := s.hub.Print(pix, width, withFace)
	if err != nil {
		http.Error(w, "send: "+err.Error(), http.StatusInternalServerError)
		return
	}
	status := "sent"
	if !sent {
		status = "queued"
	}
	log.Printf("api: sudoku date=%s difficulty=%s solution=%v id=%d status=%s",
		date, diff.Name, solution, id, status)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":     status,
		"id":         id,
		"date":       date,
		"difficulty": diff.Name,
		"puzzle":     sudoku.PuzzleNumber(date),
	})
}

// handleSudokuPNG serves the daily puzzle as a PNG (for previews in
// the web UI or debugging) — nothing is printed.
func (s *Server) handleSudokuPNG(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	diff, err := sudoku.DifficultyByName(q.Get("difficulty"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	date := q.Get("date")
	if date == "" {
		date = time.Now().Format("2006-01-02")
	}
	solution := q.Get("solution") == "1"
	puzzle, solved := sudoku.Generate(sudoku.SeedFor(date, diff.Name), diff.Givens)
	grid := puzzle
	if solution {
		grid = solved
	}
	w.Header().Set("Content-Type", "image/png")
	_, _ = w.Write(sudoku.RenderPNG(&grid, date, diff.Name, solution))
}

// handlePrint accepts a print job.
//
// Content types:
//
//   - image/png, image/jpeg: scaled/cropped to the paper width, dithered
//
//   - text/html: rendered from the supported HTML subset directly to
//     the pixel buffer (see internal/htmlr)
//
//   - text/plain: wrapped and rendered with a bitmap font
//
//   - (none): no auth; the service is meant to be reached over Tailscale only
//
// The job is queued automatically when the printer is offline and
// flushed when it reconnects; the response says which happened.
func (s *Server) handlePrint(w http.ResponseWriter, r *http.Request) {
	// face=no → SetDeliveryAndPrintNoFace (skip the printer's stored
	// personality face). bubble=1 → wrap text in a speech balloon.
	withFace := r.URL.Query().Get("face") != "no"
	bubble := r.URL.Query().Get("bubble") == "1"

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
	case strings.HasPrefix(ct, "text/html"):
		pix, width, err = htmlr.FromHTML(body)
	case strings.HasPrefix(ct, "text/"), ct == "":
		if bubble {
			pix, width, err = pixels.FromTextBubble(string(body), s.cfg.TextSize)
		} else {
			pix, width, err = pixels.FromTextSize(string(body), s.cfg.TextSize)
		}
	default:
		http.Error(w, "unsupported content type "+ct, http.StatusUnsupportedMediaType)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	sent, id, err := s.hub.Print(pix, width, withFace)
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

// --- personality ---

// personalitySlotFiles maps the wire slot names to storage file names.
var personalitySlotFiles = map[string]string{
	"face":                "face.png",
	"nothing_to_print":    "nothing_to_print.png",
	"cannot_see_bridge":   "cannot_see_bridge.png",
	"cannot_see_internet": "cannot_see_internet.png",
}

// handlePersonalityUpload accepts a multipart form with four images
// (face, nothing_to_print, cannot_see_bridge, cannot_see_internet),
// dithers them, pushes SetPersonality to the printer, and stores the
// PNGs so they survive restarts and are re-pushed on device connect.
func (s *Server) handlePersonalityUpload(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		http.Error(w, "parse multipart form: "+err.Error(), http.StatusBadRequest)
		return
	}

	stored := make(map[string][]byte)
	var slots [][]bool
	width := pixels.PaperWidth
	for _, slot := range []string{"face", "nothing_to_print", "cannot_see_bridge", "cannot_see_internet"} {
		headers, ok := r.MultipartForm.File[slot]
		if !ok || len(headers) != 1 {
			http.Error(w, "missing image for slot "+slot, http.StatusBadRequest)
			return
		}
		f, err := headers[0].Open()
		if err != nil {
			http.Error(w, "open "+slot+": "+err.Error(), http.StatusBadRequest)
			return
		}
		data, err := io.ReadAll(f)
		f.Close()
		if err != nil {
			http.Error(w, "read "+slot+": "+err.Error(), http.StatusBadRequest)
			return
		}

		pix, imgW, err := pixels.FromImage(data)
		if err != nil {
			http.Error(w, "decode "+slot+": "+err.Error(), http.StatusBadRequest)
			return
		}
		pix, imgW = pixels.PadWidth(pix, imgW, pixels.PaperWidth)
		width = imgW
		slots = append(slots, pix)
		stored[slot] = data
	}

	if err := s.hub.PushPersonality(slots[0], slots[1], slots[2], slots[3], width); err != nil {
		http.Error(w, "push: "+err.Error(), http.StatusConflict)
		return
	}

	if s.cfg.PersonalityDir != "" {
		if err := os.MkdirAll(s.cfg.PersonalityDir, 0o755); err != nil {
			http.Error(w, "create personality dir: "+err.Error(), http.StatusInternalServerError)
			return
		}
		for slot, data := range stored {
			name := personalitySlotFiles[slot]
			if err := os.WriteFile(filepath.Join(s.cfg.PersonalityDir, name), data, 0o644); err != nil {
				log.Printf("personality: store %s: %v", name, err)
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "pushed"})
}

// handlePersonalityPush re-pushes the stored personality (after a
// printer reset, say).
func (s *Server) handlePersonalityPush(w http.ResponseWriter, r *http.Request) {
	imgs, width, err := s.hub.LoadStoredPersonality()
	if err != nil {
		http.Error(w, "no stored personality: "+err.Error(), http.StatusNotFound)
		return
	}
	if err := s.hub.PushPersonality(imgs[0], imgs[1], imgs[2], imgs[3], width); err != nil {
		http.Error(w, "push: "+err.Error(), http.StatusConflict)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "pushed"})
}

// handlePersonalityStatus reports which personality images are stored.
func (s *Server) handlePersonalityStatus(w http.ResponseWriter, r *http.Request) {
	stored := map[string]bool{}
	if s.cfg.PersonalityDir != "" {
		for slot, name := range personalitySlotFiles {
			if _, err := os.Stat(filepath.Join(s.cfg.PersonalityDir, name)); err == nil {
				stored[slot] = true
			}
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"stored": stored})
}

type statusResponse struct {
	BridgeConnected bool   `json:"bridge_connected"`
	DeviceConnected bool   `json:"device_connected"`
	DeviceAddress   string `json:"device_address,omitempty"`
	LastSeen        string `json:"last_seen,omitempty"`
	QueuedPrint     bool   `json:"queued_print"`
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
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
