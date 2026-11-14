package bridge

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"littleprinter/internal/config"
)

// TestBridgeHandshakeAndPrint runs a fake bridge against the hub:
//
//  1. bridge connects, sends power_on
//  2. device asks for its encryption key -> server must reply with the
//     key derived from the claim code (golden vector from sirius)
//  3. a print job queued while offline is flushed on device_connect
func TestBridgeHandshakeAndPrint(t *testing.T) {
	cfg := &config.Config{
		Token:     "testtoken",
		ClaimCode: "6xwh-441j-8115-zyrh", // golden vector from sirius tests
	}
	hub := NewHub(cfg)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hub.HandleWS(w, r)
	}))
	defer srv.Close()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")

	// --- fake bridge ---
	clientConn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer clientConn.Close()

	send := func(v any) {
		if err := clientConn.WriteJSON(v); err != nil {
			t.Fatalf("fake bridge send: %v", err)
		}
	}

	// 1. power_on
	send(map[string]any{
		"type":           "BridgeEvent",
		"bridge_address": "some-bridge-address",
		"json_payload": map[string]any{
			"name":             "power_on",
			"model":            "A",
			"firmware_version": "v2.3.1-test",
		},
	})

	// 2. encryption_key_required
	send(map[string]any{
		"type":           "BridgeEvent",
		"bridge_address": "some-bridge-address",
		"json_payload": map[string]any{
			"name":           "encryption_key_required",
			"device_address": "0001020304050607",
		},
	})

	// Expect the add_device_encryption_key reply.
	read := func() map[string]any {
		clientConn.SetReadDeadline(time.Now().Add(5 * time.Second))
		_, raw, err := clientConn.ReadMessage()
		if err != nil {
			t.Fatalf("fake bridge read: %v", err)
		}
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatalf("bad json from server: %v", err)
		}
		return m
	}

	keyMsg := read()
	if keyMsg["type"] != "BridgeCommand" {
		t.Fatalf("expected BridgeCommand, got %v", keyMsg["type"])
	}
	payload, ok := keyMsg["json_payload"].(map[string]any)
	if !ok {
		t.Fatalf("missing json_payload in %v", keyMsg)
	}
	if payload["name"] != "add_device_encryption_key" {
		t.Fatalf("expected add_device_encryption_key, got %v", payload["name"])
	}
	params := payload["params"].(map[string]any)
	if params["encryption_key"] != "F7D9bmztHV32+WJScGZR0g==" {
		t.Errorf("wrong encryption key: %v", params["encryption_key"])
	}
	if params["device_address"] != "0001020304050607" {
		t.Errorf("wrong device_address: %v", params["device_address"])
	}

	// 3. Queue a print while "offline", then connect the device.
	// (Length of a real EncodePrint payload; rewriteCommandID patches
	// the print id at offset 6.)
	binary := make([]byte, 40)
	copy(binary, []byte{0x01, 0x02, 0x03})
	hub.mu.Lock()
	hub.pendingPrint = append(hub.pendingPrint, binary)
	hub.mu.Unlock()

	send(map[string]any{
		"type":           "BridgeEvent",
		"bridge_address": "some-bridge-address",
		"json_payload": map[string]any{
			"name":           "device_connect",
			"device_address": "0001020304050607",
		},
	})

	printMsg := read()
	if printMsg["type"] != "DeviceCommand" {
		t.Fatalf("expected DeviceCommand, got %v", printMsg["type"])
	}
	b64 := printMsg["binary_payload"].(string)
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		t.Fatal(err)
	}
	// The queued binary with the command id re-stamped at offset 6.
	want := append([]byte(nil), binary...)
	id := uint32(printMsg["command_id"].(float64))
	want[6], want[7], want[8], want[9] = byte(id), byte(id>>8), byte(id>>16), byte(id>>24)
	if string(raw) != string(want) {
		t.Errorf("flushed payload mismatch:\n got  %x\n want %x", raw, want)
	}
	if printMsg["device_address"] != "0001020304050607" {
		t.Errorf("wrong device_address on print: %v", printMsg["device_address"])
	}
}
