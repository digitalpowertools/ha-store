#!/usr/bin/env bashio
set -euo pipefail

TOKEN=$(bashio::config 'token')
CLAIM=$(bashio::config 'claim_code')
PORT=$(bashio::config 'port')

if bashio::var.is_empty "${TOKEN}"; then
    bashio::exit.nok "Set a token in the add-on options (e.g. output of: openssl rand -hex 24)"
fi
if bashio::var.is_empty "${CLAIM}"; then
    bashio::exit.nok "Set claim_code in the add-on options (printed by the printer on reset)"
fi

# Render the config the binary expects. Listen on all interfaces: with
# host_network the port is on the HA host, and the Tailscale add-on's
# Serve feature targets http://127.0.0.1:<port> from the host netns.
jq -n \
    --arg listen "0.0.0.0:${PORT}" \
    --arg token "${TOKEN}" \
    --arg claim "${CLAIM}" \
    --arg device "$(bashio::config 'device_address')" \
    --arg name "$(bashio::config 'name')" \
    --argjson debug "$(bashio::config 'log_debug')" \
    '{listen: $listen, token: $token, claim_code: $claim,
      device_address: $device, name: $name, log_debug: $debug}' \
    > /data/config.json

bashio::log.info "Starting littleprinter on 0.0.0.0:${PORT} ..."
exec littleprinter -config /data/config.json
