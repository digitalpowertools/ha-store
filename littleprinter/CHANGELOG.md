# Changelog

## 1.2.3

- **Custom personalities**: new Personality tab in the web UI (and
  `POST /personality` API) to upload the four images the printer stores
  in its flash — face, "nothing to print", "cannot see bridge",
  "cannot see internet". Ported from sirius's `SetPersonality` (0x0102)
  encoder branch (sirius had the protocol support but no upload UI).
- Stored under `/data/personality` and **re-pushed automatically every
  time the printer reconnects** (survives printer resets and add-on
  restarts). `POST /personality/push` re-pushes manually.
- Note: like the original BERG app, pushing replaces the BERG-era
  faces — upload the originals if you want to keep them.

## 1.2.2

- **Face toggle**: `POST /print?face=no` (and checkboxes in the web UI)
  send `SetDeliveryAndPrintNoFace` (0x11) — skip the printer's stored
  personality face for a single print. Default remains face-on.
- **Speech balloon**: `POST /print?bubble=1` (text only, plus a checkbox
  in the web UI) renders the message inside a bordered speech balloon
  with a tail, BERG-app style. Pure rasterization — nothing in the
  protocol requires it.

## 1.2.1

- Packaging: the supervisor now **pulls prebuilt images** from
  `ghcr.io/digitalpowertools/littleprinter-{arch}` instead of compiling on
  the device — updates no longer hammer the Pi's CPU/RAM/SD card (which
  could make Home Assistant unresponsive for many minutes).
- Images are built and pushed automatically by GitHub Actions on every
  push to `main` (`.github/workflows/docker.yml`); `build-images.sh` is
  the manual fallback.
- First-time setup: after the first push, make the ghcr packages public
  (GitHub → your profile → Packages → littleprinter-* → Package settings)
  so the supervisor can pull them anonymously.

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
