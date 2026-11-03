#!/bin/sh
# Open Poker Clock in a fullscreen terminal when the desktop supports it.
set -eu

APP="${POKER_CLOCK_BIN:-}"
if [ -z "$APP" ]; then
  if command -v poker-clock >/dev/null 2>&1; then
    APP="$(command -v poker-clock)"
  else
    APP="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)/poker-clock"
  fi
fi

if [ ! -x "$APP" ]; then
  printf '%s\n' "Poker Clock binary not found: $APP" >&2
  exit 1
fi

if command -v gnome-terminal >/dev/null 2>&1; then
  exec gnome-terminal --geometry=120x36 --full-screen -- "$APP"
elif command -v konsole >/dev/null 2>&1; then
  exec konsole --geometry 120x36 --fullscreen -e "$APP"
elif command -v xfce4-terminal >/dev/null 2>&1; then
  exec xfce4-terminal --geometry=120x36 --fullscreen --command="$APP"
elif command -v xterm >/dev/null 2>&1; then
  exec xterm -fullscreen -geometry 120x36 -e "$APP"
fi

printf '%s\n' "No supported terminal launcher found. Run '$APP' in a terminal at least 120×36." >&2
exit 1
