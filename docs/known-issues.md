# Known issues

## Linux fractional display scaling

Fractional scaling can make the UI incorrectly sized across monitors on a Linux GNOME desktop. This has been reported with both X11 and Wayland. One tested setup had a built-in 1920×1200 display at 125% and two 1920×1080 external displays at 100%; the issue remained when all displays were set to 125%. Setting every display to 100% restored expected sizing, regardless of which display the app started on.

The issue has not been reproduced across other distributions, desktop environments, or display configurations. There is no confirmed application-side fix yet; it may be an interaction between Fyne and the Linux display stack.

### Workarounds and diagnostics

- Set all displays to 100% scaling.
- `FYNE_SCALE=1.25 ./poker-clock-linux-amd64` can override Fyne's scale multiplier, but is unverified as a general fix and the appropriate value depends on the setup.
- Set `POKER_CLOCK_DEBUG=1` when launching from a terminal to log startup canvas scale and size, `FYNE_SCALE`, and fullscreen/F11 diagnostics to stderr. Include these logs when reporting a reproduction.
