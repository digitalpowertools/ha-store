# Little Printer add-on

> [!WARNING]
> **No authentication.** Anyone who can reach the add-on's port can print to
> your printer. It listens on `0.0.0.0` (via host networking) precisely so
> the Tailscale add-on can reach it — which means **it is also reachable
> from your LAN**.
>
> **Do not expose it to the public internet** (no port forwarding, no public
> reverse proxy). Keep access limited to your tailnet/LAN.

A Home Assistant add-on packaging of the [littleprinter](https://github.com/digitalpowertools/ha-store)
Go server — a single-device replacement for the Sirius / BERG Cloud
backend. The source in this folder **is** the add-on: HA builds the Docker
image straight from it.

## Install

This add-on lives in the [`digitalpowertools/ha-store`](https://github.com/digitalpowertools/ha-store)
add-on repository — no file copying needed:

1. On the Pi: **Settings → Add-ons → ⋮ → Repositories** → add
   `https://github.com/digitalpowertools/ha-store`
2. **Little Printer** appears in the store → **Install** (compiles the Go
   binary on first build — be patient on a Pi)
3. Open **Configuration** and set:
   - `claim_code` — the code the printer prints on reset
   - `device_address` — optional; empty accepts the first device that pairs
   - `port` — defaults to `8080` (bound on the host via host networking)
4. Start the add-on. Check its logs for
   `littleprinter: listening on 0.0.0.0:8080`.

**Updates:** edit the source, bump `version` in `config.yaml`, commit and
push — GitHub Actions builds and publishes the images, then check
**Settings → Add-ons → Updates** in the HA UI (wait for the action to
finish; updates are a quick image pull now, no compiling).
See `CHANGELOG.md` for version history.

The **bridge** itself, being on the same LAN, talks to the HA device
directly — point it at the HA host (IP or a hostname your local DNS
provides). If the flashed bridge insists on `wss://` with a valid cert,
see the revival guide's DNS-override + TLS notes (in the private
planning repo).

## Expose it to the tailnet (Tailscale add-on Serve)

With the official Tailscale add-on installed (and MagicDNS + HTTPS certs
enabled in the Tailscale admin console), add a service in the Tailscale
add-on's **Serve** page:

| Field | Value |
|---|---|
| Service name | `svc:sirius` |
| Target | `http://127.0.0.1:8080` |
| Protocol | `https` |
| Port | `443` |

The add-on runs on the host network, so `127.0.0.1:8080` from the Tailscale
add-on reaches this service. You get `https://<host>.tailxxxx.ts.net` with
an auto-provisioned cert — reachable from any tailnet device.

## Quick test

```bash
# from a tailnet device:
curl https://<host>.tailxxxx.ts.net/healthz
# or on the LAN:
curl http://<ha-host>:8080/healthz

# print something:
curl -X POST -H "Content-Type: text/plain" \
     --data "hello from Home Assistant" \
     http://<ha-host>:8080/print

# print HTML (see "HTML printing" below):
curl -X POST -H "Content-Type: text/html" \
     --data '<h1>Good Morning</h1><p>11°C 🌧</p>' \
     http://<ha-host>:8080/print
```

The API and the web UI at `/` are unauthenticated by design — they're only
reachable inside the tailnet (or the LAN). Don't port-forward them.

## HTML printing

`POST /print` with `Content-Type: text/html` renders a pragmatic subset of
HTML **directly to the pixel buffer** — no browser, no PNG intermediate. The
layout model is a vertical stack of blocks; there is no flex, grid, floats
or positioning, and colors are meaningless on 1-bit paper (unsupported CSS
is ignored, unknown elements are transparent containers).

Supported:

- **Structure:** `h1`–`h4`, `p`, `br`, `hr`, `ul`/`ol`/`li`,
  `blockquote`, `pre`, and simple `table`s (no colspan/rowspan; header
  rows via `<th>` get a rule underneath; columns auto-shrink their font
  when content doesn't fit the paper)
- **Inline:** `b`/`strong`, `i`/`em`, `code`/`tt`, entities, and
  **emoji** (embedded monochrome Noto Emoji; ZWJ sequences render as
  their components)
- **Images:** `<img src="data:image/png;base64,...">` — embedded data
  only, external URLs are never fetched; images are scaled to the paper
  width and Atkinson-dithered
- **CSS** (via `style` attributes): `text-align`, `font-size: Npx`,
  `margin(-top/bottom/left)`, `padding-left`,
  `border-bottom: 1px dashed|solid` — the classic BERG rule look

Example HA automation (see `examples/` for a full morning report):

```yaml
rest_command:
  little_printer_html:
    url: http://localhost:8080/print
    method: POST
    content_type: text/html
    payload: >-
      <h1 style="border-bottom:1px dashed #333">Good Morning</h1>
      <p>{{ states('weather.home') | capitalize }},
      {{ state_attr('weather.home', 'temperature') }}°C</p>
```

## Notes

- The service listens on `0.0.0.0:<port>` inside the add-on because the
  config file is rendered from the HA options.
- `/share/littleprinter` is mapped into the container if you ever need
  persistent scratch space; state is otherwise in memory.
