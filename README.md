# Poker Clock

A keyboard-operated, self-hosted Texas Hold’em tournament clock. The application is a single Go binary with an embedded default tournament template, an embedded rules reference, and a pure-Go SQLite database. No server is required.

## Requirements

- Go 1.24 or newer to build. Go 1.24.4 was used for development.
- A terminal at least **120 × 36** for the full interface.
- SQLite-compatible filesystem access for the tournament database.

## Run

```sh
go run ./cmd/poker-clock
```

Or build a local binary:

```sh
go build -trimpath -o poker-clock ./cmd/poker-clock
./poker-clock
```

Startup offers **New tournament**, **Load tournament**, and **Quit**. File selection uses the keyboard. New tournaments can use the embedded default or import YAML; the selected configuration and tournament name are stored in SQLite. Existing database or export paths are never overwritten.

The application uses Bubble Tea's alternate terminal screen. It does not change the size or fullscreen state of a terminal that was already open. Optional desktop launchers request a fullscreen terminal where the desktop supports it; see [`packaging/launchers`](packaging/launchers).

## Tournament template

The embedded default includes 12 blind levels, three breaks, €2 buy-ins/rebuys/add-ons, 1,000/1,000/5,000 chips, no ante, and a default 60/25/15 payout tier. Durations use values such as `20m`, `10m`, and `90s`; prices are euro amounts represented exactly, such as `2`, `2.0`, or `2.00`.

Template schema and rules are documented in [`docs/configuration.md`](docs/configuration.md). Importing an edited YAML template is available in Settings before play starts. SQLite becomes the source of truth after import. Configuration is locked once play starts, including after reopening an ended tournament. Export writes a reusable template without the tournament name.

## Keyboard controls

| Key | Action |
| --- | --- |
| `Space` | Start (with settings-lock confirmation), pause, or resume |
| `B` / `R` / `A` | Buy-in / rebuy / add-on |
| `P` | Add, rename, remove, or sit out/return a player; void a purchase with an explanation |
| `L` | Select a configured level or break; optionally set remaining time |
| `X` | Record a bust, void a bust event, or correct one participant |
| `O` | Sit out or return an active player |
| `S` | Import/export settings, rename tournament, manage sound, reopen ended tournament |
| `H` | Hand resolver placeholder; reserved for Stage 2 |
| `?` | Texas Hold’em tournament rules reference |
| `E` | End tournament; incomplete endings require an explanation |
| `Q` | Confirm exit; running clock pauses and saves before exit |

All modal input is keyboard-only. Text-entry dialogs treat Space as input, not as the global pause shortcut. At smaller terminal sizes the warning screen keeps Space and Q available; resizing restores any hidden dialog.

## Persistence and reporting

Each tournament is one SQLite file. An OS-backed `.lock` sidecar prevents simultaneous application instances and releases automatically after a crash. The sidecar file is intentionally retained; deleting it while the tournament exists can undermine locking. Ordinary external read-only SQLite reports remain possible while the application is open.

Audit events preserve local timestamps with UTC offsets and, after start, elapsed tournament-clock and real time. Purchases, bust participants, corrections, voids, settings changes, and results are stored transactionally. The app does not track individual chip stacks or automatically subtract blinds/antes.

## Sleep, display, and sound

The app makes a best-effort attempt to inhibit system sleep and display blanking while a tournament is open, including when paused. It uses `systemd-inhibit` plus X11 `xset` where available on Linux, `caffeinate` on macOS, and Windows execution-state requests. Desktop environments may ignore or lack these mechanisms; the app reports unavailable inhibition. A wall/monotonic clock comparison pauses at the last saved checkpoint if sleep is detected.

Transition sound is enabled by default and saved in the user's OS configuration directory, outside tournament settings. The binary embeds a short WAV. Playback uses available system players; a custom WAV/MP3 can be selected. If playback is unavailable, the app falls back to the terminal bell and keeps the visual notice.

## Build and test

```sh
go test -race ./...
go vet ./...
./scripts/build-cross-platform.sh
```

The cross-platform script builds Linux amd64/arm64, macOS amd64/arm64, and Windows amd64 binaries into `dist/`. TUI and sound behavior can still vary by terminal, audio backend, and desktop environment.
