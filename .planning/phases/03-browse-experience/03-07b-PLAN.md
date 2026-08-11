---
phase: 03-browse-experience
plan: 07b
type: execute
wave: 6
depends_on: ["03-07a"]
files_modified: []
autonomous: false
requirements: [BROWSE-01, BROWSE-03, BROWSE-07]
user_setup: []

must_haves:
  truths:
    - "On the real LCD (1920x440, Pi kiosk), a real duplicate album shows its badge — verified via a captured screenshot, not just a component test"
    - "On the real LCD, Mahler: The Symphonies appears as ONE tile, and its track list shows Disc N headers"
    - "A unique album on the real LCD shows no badge"
---

<objective>
Build and deploy the Volumio2-UI frontend implemented in 03-07a to the Pi, then verify the phase's
LCD-observable ROADMAP success criteria against the real kiosk screen — not just component tests —
via an automated CDP screenshot capture Claude inspects itself, followed by a human spot-check.

Purpose: ROADMAP Phase 3 criteria 1, 3, and 5 are explicitly "verified live" / "spot-checked" against
the real device. This plan closes that loop for the LCD.

Output: Deployed frontend on the Pi; a captured, Claude-inspected screenshot proving the badge and
multi-disc grouping render correctly; a final human confirmation.
</objective>

<execution_context>
@$HOME/.claude/get-shit-done/workflows/execute-plan.md
@$HOME/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/PROJECT.md
@.planning/ROADMAP.md
@.planning/phases/03-browse-experience/03-CONTEXT.md
@CLAUDE.md
</context>

<tasks>

<task type="auto">
  <name>Task 1: Build and deploy the frontend</name>
  <files></files>
  <read_first>
    - CLAUDE.md § Commands § Frontend (Svelte) — build/deploy sequence
  </read_first>
  <action>
    From `Volumio2-UI/`: `npm run test:run` and `npx tsc --noEmit` one more time as a final gate,
    then `npm run build` (produces `dist/` — the deploy artifact). Source Pi credentials per
    CLAUDE.md, then `scp -r dist/*` to `/home/eduardo/stellar-volumio` on the Pi via `sshpass -f
    <(printf '%s' "$RASPBERRY_PI_SSH_PASSWORD") scp ...` — never put the password in argv or echo
    it. There is no separate frontend service to restart (the backend serves `dist/` statically); a
    kiosk reload is required to pick up the new bundle — use the existing CDP reload recipe
    (`Page.reload` over the Pi's `localhost:9222` Chromium debug port, reached via SSH) rather than
    physically power-cycling the LCD.
  </action>
  <acceptance_criteria>
    - `npm run test:run` and `npx tsc --noEmit` both pass immediately before build (final gate).
    - `dist/` is deployed and the kiosk has reloaded (confirm via a fresh CDP screenshot showing the app, not a stale/blank page).
  </acceptance_criteria>
  <verify>
    <automated>npm run test:run && npx tsc --noEmit</automated>
  </verify>
  <done>The new frontend bundle is live on the Pi and the kiosk has reloaded it.</done>
</task>

<task type="auto">
  <name>Task 2: Capture and inspect LCD screenshots proving badge + multi-disc grouping</name>
  <files></files>
  <read_first>
    - .planning/phases/03-browse-experience/03-CONTEXT.md § Specific Ideas (the exact real duplicate/box-set albums to navigate to)
  </read_first>
  <action>
    Over SSH + the Pi's localhost:9222 CDP endpoint, use `Runtime.evaluate` (via the frontend's
    exposed `window.libraryActions`/navigation helpers, or by driving the swipe/tap flow
    programmatically) to navigate the Library screen to "The Future Is Now" (a real FLAC/WAV
    duplicate pair) and capture a screenshot via `Page.captureScreenshot`; read it with the Read tool
    and confirm the badge text is visibly rendered near the meta-strip. Repeat for "Mahler: The
    Symphonies" — confirm exactly one tile exists at that title (not 11 back-to-back identical
    slides) and that its track list shows "Disc 1"/"Disc 2"/... headers. Repeat for any unique album
    (e.g. one with no known duplicate) and confirm NO badge element renders. Save each screenshot
    under this plan's phase directory (e.g.
    `.planning/phases/03-browse-experience/03-07b-lcd-{case}.png`) for the SUMMARY.
  </action>
  <acceptance_criteria>
    - Three screenshots captured and visually confirmed by Claude (via Read): duplicate-badge-visible, mahler-one-tile-with-disc-headers, unique-album-no-badge.
    - Each screenshot's content is described precisely in the SUMMARY (what's visible, badge text, disc header count).
  </acceptance_criteria>
  <verify>
    <automated>ls .planning/phases/03-browse-experience/03-07b-lcd-*.png</automated>
  </verify>
  <done>Three screenshots exist and Claude has confirmed each shows the expected phase-3 behavior on the real LCD.</done>
</task>

<task type="checkpoint:human-verify" gate="blocking">
  <what-built>
    The LCD's Library screen now shows a disambiguation badge on duplicate albums, renders
    multi-disc box sets as one tile with Disc N section headers in the track list, and hides the
    badge entirely on unique albums. Claude has already captured and inspected screenshots proving
    all three (see Task 2's SUMMARY entries).
  </what-built>
  <how-to-verify>
    On the physical LCD (or via the Pi kiosk), swipe to "The Future Is Now" and confirm you can see
    a quality badge (FLAC vs WAV) near the album info. Swipe to "Mahler: The Symphonies" and confirm
    it's ONE album (not 11 identical slides in a row) and its track list shows disc headers. Swipe to
    any other album you know is unique and confirm no badge appears.
  </how-to-verify>
  <resume-signal>Type "approved" or describe any visual issue.</resume-signal>
</task>

</tasks>

<threat_model>
## Trust Boundaries

| Boundary | Description |
|----------|--------------|
| Deploy operator (Claude, via SSH/CDP) -> Pi kiosk | Standard deploy + CDP-screenshot verification path already used elsewhere in this project (documented in user memory as an established recipe); no new exposure. |

## STRIDE Threat Register

| Threat ID | Category | Component | Disposition | Mitigation Plan |
|-----------|----------|-----------|-------------|-----------------|
| T-03-11 | Tampering | CDP localhost:9222 access | accept | Pre-existing, localhost-only debug port used by the established kiosk-verification recipe; this plan does not change its exposure. |

No new package-manager installs in this plan.
</threat_model>

<verification>
Three inspected screenshots plus a human "approved" confirm the LCD renders BROWSE-01/BROWSE-03/
BROWSE-07 correctly on the real device.
</verification>

<success_criteria>
ROADMAP Phase 3 success criteria 1, 3, and 5 are confirmed true on the physical LCD.
</success_criteria>

<output>
Create `.planning/phases/03-browse-experience/03-07b-SUMMARY.md` when done
</output>
