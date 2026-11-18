// Package config loads the littleprinter service configuration.
package config

import (
	"encoding/json"
	"fmt"
	"os"

	"littleprinter/internal/pixels"
)

type Config struct {
	// HTTP listen address. Keep this bound to localhost (or the tailnet
	// interface) — the bridge reaches the service via Tailscale. The API is
	// unauthenticated by design; do not expose it publicly.
	Listen string `json:"listen"`

	// The claim code printed by the printer on reset. Used to derive the
	// encryption key handed to the bridge during pairing.
	ClaimCode string `json:"claim_code"`

	// Hex device address (as it appears in bridge events, e.g.
	// "db708b77ae2ee5b5"). If empty, the first device that pairs is
	// accepted.
	DeviceAddress string `json:"device_address"`

	// Optional: name used in log lines and status output.
	Name string `json:"name"`

	// TextSize is the text pixel size for /print text jobs (sirius's
	// template used 30). 0 selects the default.
	TextSize float64 `json:"text_size,omitempty"`

	LogDebug bool `json:"log_debug"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	c := &Config{}
	if err := json.Unmarshal(data, c); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	if c.Listen == "" {
		c.Listen = "127.0.0.1:5000"
	}
	if c.ClaimCode == "" {
		return nil, fmt.Errorf("config: claim_code must be set (printed by the printer on reset)")
	}
	if c.TextSize <= 0 {
		c.TextSize = pixels.DefaultTextSize
	}
	return c, nil
}
