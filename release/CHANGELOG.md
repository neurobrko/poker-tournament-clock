# Changelog

Notable changes for each release. This project follows the spirit of [Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versions follow `vMAJOR.MINOR.PATCH`.

## [0.0.1] - 2026-11-05

Initial release.

### Added

- Desktop tournament clock with blind levels, breaks, antes, registration windows, break reminders, and tier-based prize calculations.
- Player and tournament recordkeeping for purchases, sit-outs, busts, bust corrections, results, and payout finalization.
- Versioned, validated YAML templates with import/export and a built-in default tournament structure.
- Per-tournament SQLite persistence, transactional audit events, exclusive sidecar locking, and recovery that pauses interrupted clocks at the last saved checkpoint.
- Mouse and keyboard GUI controls, fullscreen toggle, transition notices and sounds, and an embedded Texas Hold’em rules reference.
- Poker Tournament Clock welcome-screen branding with MIT licence and maintainer contact details; release version shown in the clock footer.
- Best-effort OS sleep inhibition for Linux, macOS, and Windows.
- Automated formatting, tests, vet checks, and GitHub Actions builds/releases for Linux, macOS, and Windows on amd64 and arm64.

### Changed

- Replaced the former terminal interface with a resizable Fyne desktop application.

### Fixed

- Pause the clock when corrections leave fewer than two active entrants.
- Handle X11 environments that do not report display-power-management state; report sleep-inhibition limitations as warnings.
