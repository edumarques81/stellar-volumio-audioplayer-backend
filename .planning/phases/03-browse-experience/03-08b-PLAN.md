---
phase: 03-browse-experience
plan: 08b
type: execute
wave: 6
depends_on: ["03-08a"]
files_modified: []
autonomous: false
requirements: [BROWSE-02, BROWSE-03, BROWSE-07]
user_setup: []

must_haves:
  truths:
    - "On the iOS Simulator, a real duplicate album's tile shows its badge — verified via a captured simulator screenshot"
    - "On the iOS Simulator, a unique album's tile shows no badge"
    - "On the iOS Simulator, a multi-disc album's track list shows Disc N section headers"
---

<objective>
Build the stellar-ios app implemented in 03-08a against the simulator, install and launch it pointed
at the now-deployed backend (from 03-06), and verify the phase's iPhone-observable ROADMAP success
criteria via an automated simulator screenshot capture Claude inspects itself, followed by a human
spot-check.

Purpose: ROADMAP Phase 3 criterion 2 ("verified on the simulator against the live backend") is
explicit about this. This plan closes that loop for the iPhone app.

Output: A running simulator build proving the badge and multi-disc grouping render correctly against
live data; a final human confirmation.
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
  <name>Task 1: Build, install, and launch on the simulator against the live backend</name>
  <files></files>
  <read_first>
    - CLAUDE.md § Commands § iOS (build.sh / IOS_SIM_ID / xcodegen sequence)
  </read_first>
  <action>
    `export IOS_SIM_ID=&lt;the resolved iPhone 16 Pro simulator UDID&gt;` (two simulators share the
    name "iPhone 16 Pro" on this Mac — resolve the UDID explicitly, do not rely on a `name=` selector).
    Run `xcodegen generate --spec project.yml` (required after 03-08a's file additions/edits — do not
    skip even if no NEW files were added, only edited; run it anyway per the project's standing
    convention), then `scripts/build.sh`. Boot the resolved simulator if not already booted
    (`xcrun simctl boot $IOS_SIM_ID` if needed), install and launch the built app
    (`scripts/build.sh` may already do this — confirm; if not, `xcrun simctl install`/`launch`). In
    the app's Settings, point the backend host at `stellar.local` (or the Pi's resolved IP) so it's
    talking to the live, phase-3-deployed backend from 03-06, not a stub.
  </action>
  <acceptance_criteria>
    - The app launches on the simulator with no crash.
    - The Albums grid populates from the live backend (non-empty, matching the real 81-album library minus grouped duplicates).
  </acceptance_criteria>
  <verify>
    <automated>xcrun simctl list devices | grep "$IOS_SIM_ID"</automated>
  </verify>
  <done>The app is running on the simulator, connected to the live Pi backend.</done>
</task>

<task type="auto">
  <name>Task 2: Capture and inspect simulator screenshots proving badge + multi-disc grouping</name>
  <files></files>
  <read_first>
    - .planning/phases/03-browse-experience/03-CONTEXT.md § Specific Ideas (the exact real duplicate/box-set albums to navigate to)
  </read_first>
  <action>
    Using `xcrun simctl io $IOS_SIM_ID screenshot &lt;path&gt;.png` after navigating the simulator UI
    (via `xcrun simctl` UI automation, or manual `xcrun simctl openurl`/accessibility-driven taps if
    the project has a UI-testing harness — otherwise navigate via the same tap coordinates a human
    would use, driven through `simctl`'s available automation, and document exactly how in the
    SUMMARY): capture the Albums grid scrolled to show "The Future Is Now" (badge visible on its
    tile), the Artists tab drilled into an artist with a real duplicate or box set if one exists
    among the app's visible library (or the Albums grid showing "Mahler: The Symphonies" as ONE
    tile), and the Album Tracks screen for Mahler showing Disc N headers. Read each screenshot with
    the Read tool and confirm the expected content. Save under
    `.planning/phases/03-browse-experience/03-08b-ios-{case}.png`.
  </action>
  <acceptance_criteria>
    - Three screenshots captured and visually confirmed by Claude: duplicate-badge-visible, mahler-one-tile, mahler-disc-headers.
    - Each screenshot's content is described precisely in the SUMMARY.
  </acceptance_criteria>
  <verify>
    <automated>ls .planning/phases/03-browse-experience/03-08b-ios-*.png</automated>
  </verify>
  <done>Three screenshots exist and Claude has confirmed each shows the expected phase-3 behavior on the simulator against live data.</done>
</task>

<task type="checkpoint:human-verify" gate="blocking">
  <what-built>
    The iPhone app's Albums grid and Artist→Albums grid now show a disambiguation badge on duplicate
    albums, render multi-disc box sets as one tile with Disc N section headers in the track list, and
    hide the badge entirely on unique albums. Claude has already captured and inspected simulator
    screenshots proving all three (see Task 2's SUMMARY entries).
  </what-built>
  <how-to-verify>
    On the simulator (or a paired physical iPhone via scripts/deploy-to-device.sh if you prefer),
    open the app, scroll the Albums grid to "The Future Is Now" and confirm you can see a quality
    badge on its tile. Find "Mahler: The Symphonies" and confirm it's ONE tile (not 11), then tap
    into it and confirm the track list shows disc headers. Check any album you know is unique and
    confirm no badge appears on its tile.
  </how-to-verify>
  <resume-signal>Type "approved" or describe any visual issue.</resume-signal>
</task>

</tasks>

<threat_model>
## Trust Boundaries

| Boundary | Description |
|----------|--------------|
| Simulator app -> live Pi backend | Same LAN/Bonjour-discovered connection the app always uses; no new exposure from this verification step. |

## STRIDE Threat Register

| Threat ID | Category | Component | Disposition | Mitigation Plan |
|-----------|----------|-----------|-------------|-----------------|
| T-03-13 | n/a | Simulator screenshot capture | accept | Local developer-tooling operation (`xcrun simctl`), no network exposure change. |

No new package-manager installs in this plan.
</threat_model>

<verification>
Three inspected screenshots plus a human "approved" confirm the iPhone app renders BROWSE-02/
BROWSE-03/BROWSE-07 correctly against the live backend.
</verification>

<success_criteria>
ROADMAP Phase 3 success criteria 2 and 5 (badge on the iPhone; Mahler grouping on the iPhone) are
confirmed true on the simulator against the live backend.
</success_criteria>

<output>
Create `.planning/phases/03-browse-experience/03-08b-SUMMARY.md` when done
</output>
