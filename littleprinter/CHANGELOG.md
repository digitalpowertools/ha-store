# Changelog

## 1.2.0

- Text printed through the web UI and `/print` (text body) is now rendered
  at **30px** like the original sirius server
  (`sirius/coding/default_template.html` used font-size 30px Helvetica);
  the previous 7×13 bitmap font was ~2mm tall on paper.
- Uses the embedded Go Regular typeface (no new dependencies); word-wrap
  is based on measured text width.
- New option `text_size` (optional, default 30) to adjust text size
  without rebuilding.

## 1.1.2

- Fix: the bridge's WebSocket upgrade was rejected with **403 Forbidden**.
  The bridge's ws4py client sends an `Origin` header that includes the
  port while its `Host` header omits it, so gorilla's default same-origin
  check always failed. All origins are now accepted — the endpoint is
  LAN-only and carries no credentials.

## 1.1.1

- Fix: the bridge-facing WebSocket endpoint was never registered, so the
  bridge (which hard-codes `/api/v1/connection`; configured URL paths are
  ignored by its firmware) got a 404 and could never connect.
  Both `/api/v1/connection` and `/bridge` are now served.

## 1.1.0

- New **web UI** (server root): print text and images from a browser,
  no curl needed.
- Removed token auth on `/print` and `/status` — the service is meant to
  be reached over the LAN/tailnet only.

## 1.0.0

- Initial add-on packaging of the littleprinter Go server.
- Config rendered from HA add-on options into `/data/config.json` at start.
- Host networking with configurable port (default 8080), designed to pair
  with the Tailscale add-on Serve feature.
