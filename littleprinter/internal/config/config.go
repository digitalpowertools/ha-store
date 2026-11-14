// Package config loads the littleprinter service configuration.
package config

import (
	"encoding/json"
	"fmt"
	"os"
)

type Config struct {
	// HTTP listen address. Keep this bound to localhost (or the tailnet
	// interface) — the bridge reaches the service via Tailscale, and the
	// print API is protected only by a bearer token.
	Listen string `json:"listen"`

	// Bearer token required on /print and /status.
	Token string `json:"token"`

	// The claim code printed by the printer on reset. Used to derive the
	// encryption key handed to the bridge during pairing.
	ClaimCode string `json:"claim_code"`

	// Hex device address (as it appears in bridge events, e.g.
	// "db708b77ae2ee5b5"). If empty, the first device that pairs is
	// accepted.
	DeviceAddress string `json:"device_address"`

	// Optional: name used in log lines and status output.
	Name string `json:"name"`

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
	if c.Token == "" {
		return nil, fmt.Errorf("config: token must be set")
	}
	if c.ClaimCode == "" {
		return nil, fmt.Errorf("config: claim_code must be set (printed by the printer on reset)")
	}
	return c, nil
}
