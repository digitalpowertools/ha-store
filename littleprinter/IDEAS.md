# Ideas & future work

Backlog of feature ideas for the Little Printer add-on. Nothing here is
scheduled — grab one when it sounds fun.

---

## Quip message (`0x0202 SetQuip`)

**Status:** idea — not implemented. `CmdSetQuip (0x0202)` is declared in
`internal/pixels/encoder.go` but there is no encoder function, API or
automation for it.

**What it is:** a short one-liner stored in the printer's flash. The
original BERG behavior: pressing the printer's button prints the stored
**personality face + quip** — entirely from flash, no server contact
(which is also why the bridge firmware only forwards
`EVENT_HEARTBEAT`, `EVENT_DID_PRINT`, `EVENT_DID_POWER_ON` — the button
never reaches the server).

**Implementation sketch:**

1. `pixels.EncodeQuip(pix []bool, width int, printID uint32) []byte`
   — payload is presumably rendered-text pixels like the personality
   message in `EncodePersonalityWithMessage`. Verify payload format
   against the printer (try RLE pixels first; fall back to raw UTF-8
   if the print comes out wrong).
2. `Hub.PushQuip` in `internal/bridge` — mirror `PushPersonality`.
3. `POST /quip` (plain text body) → render with the `htmlr` font
   machinery → push. Store the quip PNG under `PersonalityDir` and
   re-push on reconnect like the faces.
4. `GET /quip` to show the current one.
5. HA: a tiny automation (or the printer itself) that rotates the quip
   daily — yesterday's sudoku solution, a countdown to the weekend,
   the weather one-liner.

**Prereq/verification:** press the printer's button first and confirm
it prints the stored face (proving the flash-based flow works on this
unit). If the button does something else, re-check the firmware dump
(`berg-bridge-dump-master/`) before building.

**Related:** `0x0101 SetPersonalityWithMessage` (face + single message)
is implemented in the encoder but not exposed by the API either; could
share the same plumbing.

## Other backlog

- `/status`: add `last_print` timestamp — easier delivery confirmation
  than scraping add-on logs (came up while debugging the morning report).
- Chart endpoint: `GET /chart?entity=<ha-entity>&hours=24` — pull HA
  history server-side, draw a sparkline, no client-side PNG needed.
  (Alternative: HA-side matplotlib → base64 → `<img>` in HTML.)
- Button test: press the printer button while tailing add-on logs to
  see what (if anything) the bridge sends. Outcome decides whether the
  button can ever trigger server-side content (e.g. sudoku solution).
- Content generators register as `POST /print/{name}` siblings
  (`internal/xkcd`, `internal/wordle`, …) — only if 5+ accumulate;
  see the "separate add-on?" discussion from the sudoku work.
