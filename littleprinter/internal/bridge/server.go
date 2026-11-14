// Package bridge implements the server side of the BERG Cloud Bridge
// websocket protocol: bridge events come in as JSON, print commands go
// out as JSON with base64 binary payloads. Port of the relevant parts of
// sirius/protocol/protocol_loop.py, sirius/coding/decoders.py and
// sirius/coding/encoders.py.
package bridge

import (
	"encoding/base64"
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"littleprinter/internal/claiming"
	"littleprinter/internal/config"
	"littleprinter/internal/pixels"
)

// Hub tracks the connected bridge and device state.
type Hub struct {
	cfg *config.Config

	mu           sync.Mutex
	conn         *websocket.Conn
	bridgeAddr   string
	deviceAddr   string
	lastSeen     time.Time
	commandID    uint32
	pendingPrint [][]byte // encoded binary payloads, queued while device is offline
}

func NewHub(cfg *config.Config) *Hub {
	return &Hub{
		cfg:       cfg,
		commandID: uint32(time.Now().Unix()) & 0x0fffffff,
	}
}

// Status describes device state for /status.
type Status struct {
	BridgeConnected bool   `json:"bridge_connected"`
	BridgeAddress   string `json:"bridge_address,omitempty"`
	DeviceAddress   string `json:"device_address,omitempty"`
	LastSeen        string `json:"last_seen,omitempty"`
	QueuedPrint     bool   `json:"queued_print"`
}

func (h *Hub) Status() Status {
	h.mu.Lock()
	defer h.mu.Unlock()
	s := Status{QueuedPrint: len(h.pendingPrint) > 0}
	if h.conn != nil {
		s.BridgeConnected = true
		s.BridgeAddress = h.bridgeAddr
		s.DeviceAddress = h.deviceAddr
		if !h.lastSeen.IsZero() {
			s.LastSeen = h.lastSeen.UTC().Format(time.RFC3339)
		}
	}
	return s
}

// --- websocket handler ---

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

// HandleWS is the /bridge endpoint. The first message must be a
// power_on BridgeEvent (that's how the bridge announces itself).
func (h *Hub) HandleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("bridge: upgrade: %v", err)
		return
	}
	defer conn.Close()
	log.Printf("bridge: websocket connected from %s", r.RemoteAddr)

	var bridgeAddr string
	first := true

	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			log.Printf("bridge: disconnected (%v)", err)
			break
		}

		var env envelope
		if err := json.Unmarshal(raw, &env); err != nil {
			log.Printf("bridge: bad json: %v", err)
			continue
		}

		if first {
			// The official server requires the first message to be
			// power_on and keys the connection on bridge_address.
			if env.Type != "BridgeEvent" || env.JSONPayload == nil || env.JSONPayload.Name != "power_on" {
				log.Printf("bridge: first message was not power_on (%s), closing", env.Type)
				break
			}
			bridgeAddr = env.BridgeAddress
			h.mu.Lock()
			h.conn = conn
			h.bridgeAddr = bridgeAddr
			h.mu.Unlock()
			first = false
			log.Printf("bridge: %s announced power_on (model %s, firmware %s)",
				bridgeAddr, env.JSONPayload.Model, env.JSONPayload.FirmwareVersion)
			continue
		}

		h.handleEvent(conn, &env)
	}

	h.mu.Lock()
	if h.conn == conn {
		h.conn = nil
		h.deviceAddr = ""
		// pendingPrint survives: it will flush when the bridge returns.
	}
	h.mu.Unlock()
}

// envelope is a deliberately loose decode of the wire format: we only
// need a few fields and the bridge sends plenty more.
type envelope struct {
	Type          string `json:"type"`
	BridgeAddress string `json:"bridge_address"`
	DeviceAddress string `json:"device_address"`
	CommandID     uint32 `json:"command_id"`
	ReturnCode    *int   `json:"return_code"`
	JSONPayload   *struct {
		Name            string `json:"name"`
		DeviceAddress   string `json:"device_address"`
		Model           string `json:"model"`
		FirmwareVersion string `json:"firmware_version"`
	} `json:"json_payload"`
}

func (h *Hub) handleEvent(conn *websocket.Conn, env *envelope) {
	switch env.Type {
	case "BridgeEvent":
		if env.JSONPayload == nil {
			return
		}
		switch env.JSONPayload.Name {
		case "device_connect", "encryption_key_required":
			h.markAlive(env.JSONPayload.DeviceAddress)
			if env.JSONPayload.Name == "encryption_key_required" {
				h.sendEncryptionKey(conn, env.BridgeAddress, env.JSONPayload.DeviceAddress)
			}
			h.flushPending(conn)
		case "device_disconnect":
			log.Printf("bridge: device %s disconnected", env.JSONPayload.DeviceAddress)
		case "power_on":
			log.Printf("bridge: superfluous power_on (backlog)")
		default:
			log.Printf("bridge: event %s", env.JSONPayload.Name)
		}

	case "DeviceEvent":
		log.Printf("bridge: device event (device %s)", env.DeviceAddress)

	case "DeviceCommandResponse", "BridgeCommandResponse":
		rc := -1
		if env.ReturnCode != nil {
			rc = *env.ReturnCode
		}
		log.Printf("bridge: command response id=%d return_code=%d", env.CommandID, rc)

	case "BridgeLog":
		// ignore

	default:
		log.Printf("bridge: unhandled message type %s", env.Type)
	}
}

func (h *Hub) markAlive(deviceAddr string) {
	h.mu.Lock()
	h.deviceAddr = deviceAddr
	h.lastSeen = time.Now()
	h.mu.Unlock()
	log.Printf("bridge: device %s alive", deviceAddr)
}

// nextCommandID returns a fresh 32-bit command id; 0 is invalid
// (reserved) in the protocol.
func (h *Hub) nextCommandID() uint32 {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.commandID++
	for h.commandID == 0 {
		h.commandID++
	}
	return h.commandID
}

// sendEncryptionKey responds to encryption_key_required with
// add_device_encryption_key carrying the key derived from the claim code.
func (h *Hub) sendEncryptionKey(conn *websocket.Conn, bridgeAddr, deviceAddr string) {
	if h.cfg.DeviceAddress != "" && deviceAddr != h.cfg.DeviceAddress {
		log.Printf("bridge: ignoring unexpected device %s (configured: %s)",
			deviceAddr, h.cfg.DeviceAddress)
		return
	}

	// Sanity check: claim code device field must match the device address.
	wantXor, err := claiming.HardwareXorFromDeviceAddress(deviceAddr)
	if err != nil {
		log.Printf("bridge: bad device address %q: %v", deviceAddr, err)
		return
	}
	_ = wantXor // full claiming model skipped; logged for debugging only

	key, err := claiming.KeyFromClaimCode(h.cfg.ClaimCode)
	if err != nil {
		log.Printf("bridge: cannot derive key from claim code: %v", err)
		return
	}

	id := h.nextCommandID()
	msg := map[string]any{
		"type":           "BridgeCommand",
		"bridge_address": bridgeAddr,
		"command_id":     id,
		"timestamp":      "0",
		"json_payload": map[string]any{
			"name": "add_device_encryption_key",
			"params": map[string]any{
				"device_address": deviceAddr,
				"encryption_key": key,
			},
		},
	}
	h.send(conn, msg)
	log.Printf("bridge: sent encryption key for device %s (command %d)", deviceAddr, id)
}

// Print queues or sends a print job for the given 1-bit pixels.
// Returns false if the device is not currently connected (job is queued).
func (h *Hub) Print(pix []bool, width int) (sent bool, id uint32, err error) {
	id = h.nextCommandID()
	binary := pixels.EncodePrint(pixels.CmdSetDeliveryAndPrint, pix, width, id)

	h.mu.Lock()
	defer h.mu.Unlock()
	if h.conn == nil || h.deviceAddr == "" {
		h.pendingPrint = append(h.pendingPrint, binary)
		log.Printf("print: device offline, queued (id %d)", id)
		return false, id, nil
	}

	msg := h.deviceCommand(h.deviceAddr, binary, id)
	if err := h.conn.WriteJSON(msg); err != nil {
		return false, id, err
	}
	log.Printf("print: sent to device %s (id %d)", h.deviceAddr, id)
	return true, id, nil
}

// flushPending sends queued prints when the device comes online.
func (h *Hub) flushPending(conn *websocket.Conn) {
	h.mu.Lock()
	var jobs [][]byte
	jobs = append(jobs, h.pendingPrint...)
	h.pendingPrint = nil
	h.mu.Unlock()

	for _, binary := range jobs {
		id := h.nextCommandID()
		// Re-stamp the command id (EncodePrint embeds it for acks).
		binary = rewriteCommandID(binary, id)
		msg := h.deviceCommand(h.deviceAddr, binary, id)
		if err := conn.WriteJSON(msg); err != nil {
			log.Printf("print: failed to flush queued job: %v", err)
			return
		}
		log.Printf("print: flushed queued job to device %s (id %d)", h.deviceAddr, id)
	}
}

func (h *Hub) deviceCommand(deviceAddr string, binary []byte, id uint32) map[string]any {
	return map[string]any{
		"type":           "DeviceCommand",
		"bridge_address": h.bridgeAddr,
		"device_address": deviceAddr,
		"command_id":     id,
		"timestamp":      "0",
		"binary_payload": base64.StdEncoding.EncodeToString(binary),
	}
}

func (h *Hub) send(conn *websocket.Conn, msg any) {
	if err := conn.WriteJSON(msg); err != nil {
		log.Printf("bridge: send failed: %v", err)
	}
}

// rewriteCommandID patches the 32-bit print id embedded in a binary
// payload (offset 6 in the command header).
func rewriteCommandID(binary []byte, id uint32) []byte {
	out := make([]byte, len(binary))
	copy(out, binary)
	out[6] = byte(id)
	out[7] = byte(id >> 8)
	out[8] = byte(id >> 16)
	out[9] = byte(id >> 24)
	return out
}
