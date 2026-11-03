# Optional desktop launchers

The binary itself never resizes or fullscreens an already-open terminal. These wrappers request fullscreen terminal windows when supported; desktop behavior is best-effort.

## Linux

Install `poker-clock` and `poker-clock-launcher.sh` in a directory on `PATH`, then install `poker-clock.desktop` into `~/.local/share/applications/` or `/usr/share/applications/`. The launcher tries GNOME Terminal, Konsole, Xfce Terminal, then xterm. If none is available, run the binary from a terminal sized at least 120 × 36.

## macOS

Place executable `poker-clock` beside `poker-clock.command`, then launch the `.command` file in Finder. It asks Terminal to enter fullscreen. macOS may require Accessibility permission for fullscreen automation; if denied, Terminal still opens and can be made fullscreen manually.

## Windows

Run `poker-clock.exe` from a console. No GUI-terminal launcher is included; the executable remains a console application.
