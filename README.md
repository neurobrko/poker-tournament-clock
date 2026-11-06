# Poker Tournament Clock

A desktop GUI for running self-hosted Texas Hold’em tournaments. The application is a Go/Fyne desktop app with an embedded default tournament template, rules reference, Ubuntu fonts, and pure-Go SQLite database. No terminal or server is required at runtime. The welcome screen shows the MIT licence and maintainer contact: Marek Paulik · elvis@elvis.sk · github.com/neurobrko/poker-tournament-clock.

## Requirements

- Go 1.24 or newer to build. Go 1.24.4 was used for development.
- Linux build: C compiler, `pkg-config`, OpenGL development libraries, and X11 development headers. On Debian/Ubuntu: `sudo apt install gcc pkg-config libgl1-mesa-dev xorg-dev`.
- A supported desktop graphics environment. macOS and Windows builds need native toolchains for Fyne's desktop driver.
- SQLite-compatible filesystem access for the tournament database.

## Download

Get the latest standalone binaries from [GitHub Releases](https://github.com/neurobrko/poker-tournament-clock/releases). Choose the file matching your OS and CPU: `poker-clock-linux-amd64`, `poker-clock-linux-arm64`, `poker-clock-darwin-amd64`, `poker-clock-darwin-arm64`, `poker-clock-windows-amd64.exe`, or `poker-clock-windows-arm64.exe`.

On Linux and macOS, make the downloaded binary executable before launching it (for example, `chmod +x poker-clock-linux-amd64`). Linux also needs runtime OpenGL/X11 libraries. Releases are raw executables, not installers; they are currently unsigned and macOS builds are not notarized, so the OS may show a security warning. Windows startup/shutdown errors are appended to `%APPDATA%/poker-clock/errors.log`.

## Run

```sh
go run ./cmd/poker-clock
```

Or build a local binary:

```sh
go build -trimpath -o poker-clock ./cmd/poker-clock
./poker-clock
```

The app opens fullscreen by default. Resize the window freely or use **F11** (also available as bottom-right button) to toggle fullscreen. Tournament state appears beneath the clock. The dark Spacedust-inspired palette and embedded Ubuntu Sans, Sans Mono, and Condensed fonts work offline; clocks use Mono and footer notices use Condensed. Colors and typography live in `internal/ui/desktop_theme.go` and can be changed centrally. Startup offers **New tournament**, **Load tournament**, and **Quit**. Release builds display their version tag in the clock footer between local time and the F11 button. Use on-screen controls or keyboard shortcuts. New tournaments can use the embedded default or import YAML; choose a destination folder and enter a new database filename. Existing files are visible but never overwritten. **Load tournament** lists SQLite databases; to use YAML, choose **New tournament → Browse YAML template**.

A normal Linux desktop build links Fyne's OpenGL/X11 driver. Headless CI/tests use Fyne's software driver with `-tags ci`.

## Tournament template

The embedded default includes 12 blind levels, three breaks, €2 buy-ins/rebuys/add-ons, 1,000/1,000/5,000 chips, no ante, and a default 60/25/15 payout tier. Durations use values such as `20m`, `10m`, and `90s`; prices are euro amounts represented exactly, such as `2`, `2.0`, or `2.00`.

Template schema and rules are documented in [`docs/configuration.md`](docs/configuration.md). Importing an edited YAML template is available in Settings before play starts. SQLite becomes the source of truth after import. Configuration is locked once play starts, including after reopening an ended tournament. Export writes a reusable template without the tournament name.

## Keyboard controls

| Key | Action |
| --- | --- |
| `Space` | Start (with settings-lock confirmation), pause, or resume |
| `B` / `R` / `A` | Buy-in / rebuy / add-on |
| `P` | Add, view player purchase/bust history, rename/remove a player, or void a purchase (history only after ending) |
| `O` | Sit out or return an active player |
| `L` | Select a configured level or break (confirmation required); optionally set remaining time |
| `X` | Record a bust, void a bust event, or correct one participant |
| `S` | Import/export settings, rename tournament, choose sound file, reopen ended tournament |
| `N` | Toggle sound on/off |
| `F` | View latest completed standings and saved player payouts |
| `?` | Texas Hold’em tournament rules reference |
| `E` | End tournament; incomplete endings require an explanation |
| `Q` | Confirm exit; running clock pauses and saves before exit |

Controls work with mouse and keyboard. Welcome screen: `N` new, `L` load, `Q` quit; Left/Right arrows and Tab move selection, Enter activates. File browser hides dotfiles and unsupported items by default; `H` toggles hidden files, `U` toggles unselectable files, `Home`/`End` jump to list bounds, `Bck` moves to parent, `N` creates new file, and `Esc` cancels. Player info shows purchase and bust history; running clocks stay visible and update beneath dialogs. Text-entry dialogs treat Space as input, not as the global pause shortcut. Use F11 or window controls to leave fullscreen and resize.

## Persistence and reporting

Each tournament is one SQLite file. An OS-backed `.lock` sidecar prevents simultaneous application instances and releases automatically after a crash. The sidecar file is intentionally retained; deleting it while the tournament exists can undermine locking. Ordinary external read-only SQLite reports remain possible while the application is open.

Audit events preserve local timestamps with UTC offsets and, after start, elapsed tournament-clock and real time. Purchases, bust participants, corrections, voids, settings changes, and results are stored transactionally. The app does not track individual chip stacks or automatically subtract blinds/antes. Break `reminder` text is informational. A structured `chipup` value marks a denomination as removed from the visible chip set and adjusts average-stack rounding; it still does not alter recorded chip totals.

## Sleep and sound

The app makes a best-effort attempt to inhibit system sleep and display blanking while a tournament is open, including when paused. It uses `systemd-inhibit` plus X11 `xset` where available on Linux, `caffeinate` on macOS, and Windows execution-state requests. Desktop environments may ignore or lack these mechanisms; the app reports unavailable inhibition. A wall/monotonic clock comparison pauses at the last saved checkpoint if sleep is detected.

Transition sound is enabled by default and saved in the user's OS configuration directory, outside tournament settings. The GUI embeds `src/end.mp3` for level changes and a WAV for the one-minute warning; choosing a custom WAV/MP3 overrides the corresponding sound. On Linux, WAV playback needs `paplay`, `aplay`, or `ffplay`; MP3 playback needs `ffplay`, `mpv`, or `mpg123`. Playback failures appear in the GUI status area, and the visual notice remains.

## Build and test

Install [pre-commit](https://pre-commit.com/#install), then enable the repository hooks and run them across all files:

```sh
pre-commit install
pre-commit run --all-files
```

The hooks format Go files, run `go test -tags ci ./...`, and run `go vet -tags ci ./...`. To run the checks and build a native binary directly:

```sh
./scripts/build-native.sh
```

This writes `dist/poker-clock-<os>-<arch>` (for example, `dist/poker-clock-linux-amd64`; Windows binaries end in `.exe`). Fyne's desktop driver uses CGO and native graphics libraries, so build on each target OS with its native toolchain. GitHub Actions runs checks and a native Linux build on pushes and pull requests; pull requests also compile the supported OS/architecture matrix. Pushing a `v*` tag builds the same matrix and publishes a GitHub Release. See [release instructions](docs/releasing.md) before tagging. Gitea pull request events do not trigger GitHub Actions. Sound availability depends on the system audio backend.
