# Handoff: Fyne desktop GUI rebuild

**Date:** 2026-11-04T10:58:34+01:00  
**Focus for next session:** Replace the Bubble Tea terminal interface with a Fyne desktop GUI, Linux first, preserving portability to macOS and Windows.

## Context and decisions

The existing application is a keyboard-operated, self-hosted poker tournament clock. The user wants a fully graphical application rather than a terminal UI, and selected Fyne. Linux is the initial build target; keep all three desktop OSes in mind without making macOS/Windows delivery a prerequisite for the initial Linux work. The earlier recommendation was to preserve current tournament behavior, workflows and databases; treat that as the migration baseline.

Visual reference: `src/visual_1.png` (also `src/visual.vectorcraft` exists). The image is a proposed design, not an established specification for new business rules. It shows a title across the top, tournament statistics and prizes on the left, current blinds/ante, a huge timer and status in the center, next-event information at the bottom, and actions on the right.

User requirements:
- Timer is the largest element; blinds and ante are second largest.
- One screen serves players and operator. Separate displays may come later.
- Mouse and keyboard interactions are both required.
- Dark theme and fullscreen by default, with a way to leave fullscreen and resize; no fixed target resolution. Plan for light and color schemes in later stages; keep colors easy to change in code, but do not assume a theme selector is required now.
- Image is a flexible layout reference, not a pixel-perfect target. User invites creative font/color choices and emphasizes readability; Google Fonts may be proposed or downloaded. Bundle chosen fonts for offline use where practical and retain applicable license attribution.
- Colors should be similar to the user's Spacedust desktop theme. A local Kitty Spacedust palette was found at `/home/elvis/.config/kitty/Spacedust.conf` (not verified as the precise desktop theme): background `#0a1e24`, foreground `#ecefc1`, orange `#e35a00`, green `#5cab96`, cyan `#06afc7`, yellow `#e3cd7b`, blue `#67a0cd`. Use as inspiration, not a mandatory exact palette; ensure readable contrast.
- Right-column actions must be buttons supporting disabled and on/off states. Hand Resolver and Timeline remain disabled for now.
- Keep visual sections/components separate so future layout redesigns are easy.
- Mockup timing values are illustrative. Keep existing player-count semantics: sitting-out players remain active; the mockup's counts were a mistake.

## Current state

- Working directory: `/data/work/code/poker_clock`.
- Branch / commit: `main @ f5de80f`.
- Last action: implemented the first Fyne desktop GUI and updated this handoff. No commit created.
- Build/test status: `go test -race ./...`, `go vet ./...`, `go build -o /tmp/poker-clock-gui ./cmd/poker-clock`, and `git diff --check` pass after user installed Linux OpenGL/X11 packages. Fyne window smoke-tested on Wayland/XWayland; window title visible fullscreen at 1920×1080.
- Untracked files include `.gitignore`, this handoff, GUI source/font assets, `scripts/build-native.sh`, and the user-provided `src/visual_1.png`, `src/visual.vectorcraft`. Do not remove or overwrite the original assets.

## Relevant code and artifacts

- `README.md` — existing features, keyboard commands, persistence, clock behavior, build targets.
- `cmd/poker-clock/main.go:1` — Fyne app entry point.
- `internal/tui/desktop.go:1` — Fyne window, fullscreen, key routing, tick scheduling and app lifecycle. It currently reuses the existing model's tested workflow methods.
- `internal/tui/desktop_views.go:1` — composable dashboard sections, action states, timer scaling and responsive layout.
- `internal/tui/desktop_dialogs.go:1` — mouse-operable GUI wrappers for existing forms, selections, confirmations and file browser.
- `internal/tui/desktop_theme.go:1` — central Spacedust-inspired palette and embedded Ubuntu Sans, Mono, and Condensed font setup; `internal/tui/fonts/UFL.txt` is font license.
- `internal/tui/desktop_icon.go:1` and `internal/tui/icons/poker-clock.png` — embedded app/window icon. Editable SVG master: `src/poker-clock-icon.svg`.
- `internal/tui/desktop_test.go:1` — headless Fyne UI tests.
- `internal/tui/model.go:36` — existing UI/workflow state, refresh, timer recovery and tournament operations. This remains Bubble Tea-coupled and is future extraction debt.
- `internal/tui/view.go:1`, `internal/tui/dialogs.go:1` — legacy terminal renderers and key-driven workflow logic still compile but are not launched as a terminal UI.
- `internal/tui/dialogs.go:1`, `internal/tui/browser.go`, `internal/tui/view_dialogs.go` — current workflows, validations, existing key-driven dialogs and file selection reused by the GUI adapter.
- `internal/tournament/clock.go:1`, `internal/tournament/service.go:1`, `internal/domain/`, `internal/store/` — reusable service, business rules and SQLite persistence; check actual boundaries before refactoring.
- `internal/sleepinhibit/`, `internal/sound/`, `internal/preferences/` — existing platform behavior and preferences. GUI now uses `sound.PlayDesktop` to avoid the old terminal-bell fallback.
- `scripts/build-native.sh` tests headlessly and builds for current host; `scripts/build-cross-platform.sh` now delegates with a native-build warning. Do not claim macOS/Windows builds are verified without testing.
- `.handoff/261103-1745_poker-clock-stage-2.md` — separate, previously planned hand resolver and player timeline. These are not currently implemented: `[H]` is a placeholder and `[T]` timeline is absent. User says keep both disabled for now.

## What's done

- Replaced `cmd/poker-clock` terminal launch with a Fyne desktop window, fullscreen by default, resizable, with F11 toggle.
- Built Spacedust-inspired dark dashboard: large timer; single-line blinds/ante; compact left-column stats/purchases/prizes/state; scrollable right action buttons; semantic disabled/on/off states; centered title; state directly under clock; F11 bottom-right; no shortcut strip or shadows. Left column fits four payout rows at fullscreen. Kept views/components and mutable palette separate.
- Fixed mouse/keyboard interaction with zero-size focus sink: welcome Tab/left-right arrows update highlight, N/L/Q shortcuts work, Enter activates, and modal key events stay inside modal without background focus. Browser hides dotfiles and unselectable files by default, H/U toggles, button hints, selected colors, parent control at bottom and auto-scroll. Clocks continue refreshing under overlays.
- Bundled Ubuntu Sans regular/bold/italic for UI, Ubuntu Sans Mono for timer/countdown lines, and Ubuntu Sans Condensed for footer logs/warnings, offline with Ubuntu Font Licence from `src/Ubuntu/`.
- Created white-background spade/heart icon, with spade rotated 30° CCW and overlapping heart rotated 30° CW; embedded PNG is applied to Fyne app/window.
- Reused current tournament flows for GUI buttons/keyboard: startup, database/YAML/sound file browsers, settings, player/bust management, levels, rules, end, quit and confirmations. Added Player Info history (purchases/busts), removed duplicate sit-out and sound-toggle settings entries, and added level-change confirmation. Bust dialogs support arrow navigation and Space toggling. Hand Resolver and Timeline stay disabled.
- Added current/total bust counts, right-aligned tournament values, compact Stack & pool rows with `5 @ 2.00 €`-style pricing, Euro suffix formatting, and strike-through for paid places. Error messages strip event-wrapper prefixes and normalize capitalization/ending punctuation.
- Main, next-break, and local clock lines use Ubuntu Sans Mono. Removed the custom fixed-cell clock layout per latest feedback; state includes current level while running/paused.
- Added desktop audio path that suppresses terminal bell; GUI surfaces sound errors as status.
- Updated README, native build script and old terminal-launcher documentation.
- Added headless UI startup/dashboard tests and desktop sound regression test.

## What's next

1. User should review updated GUI appearance and interactions at their normal display size. Headless Fyne screenshot was checked at 1920×1080; app smoke-tested fullscreen on this Wayland/XWayland session.
2. Test mouse/key behavior through every existing workflow and tune any remaining clipping/wrapping on real screens.
3. Verify native builds on macOS and Windows separately; cross-compilation is not configured.
4. Consider extracting workflow/controller and clock lifecycle from `internal/tui/model.go` so GUI no longer depends on Bubble Tea key messages and legacy terminal view files/dependencies can eventually be removed.
5. Future stages: light/alternate palettes and requested deferred resolver/timeline, only with clarified scope.

## Open questions / decisions pending

No product decisions block current work. User installed native Linux prerequisites. A real window launch was confirmed; detailed interaction review across all tournament workflows remains pending.

## Gotchas

- The first GUI intentionally reuses `internal/tui.Model` as its action/workflow controller and synthesizes Bubble Tea keys. No Bubble Tea program/terminal screen launches, but the package and terminal renderers remain. Refactor this boundary later rather than assuming the model is GUI-independent.
- Existing UI model does more than rendering: it coordinates clock checkpoints, dialogs, file access, audio, sleep inhibition and shutdown. Replacing only `view.go` loses behavior.
- Existing database locking, event audit, recovery and no-overwrite protections are important; preserve them rather than building parallel state in GUI.
- Paused/hold/break/ended states and disabled action conditions are more nuanced than a single timer toggle. Avoid relying on color alone for state.
- Existing file-picker and keyboard interactions have deliberate safety behavior; current GUI wraps the original filesystem browser and workflow transitions. Preserve no-overwrite protections.
- Current screenshot uses example counts and values which may not reflect real domain semantics.

## Suggested skills

- `caveman` — concise conversational communication.
- `figma-use` — only if the user later requests work in Figma; local reference image alone does not require it.
