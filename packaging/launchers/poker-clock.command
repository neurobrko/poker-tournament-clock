#!/bin/sh
# macOS Finder launcher. Fullscreen activation can require Accessibility permission.
set -eu
APP="${POKER_CLOCK_BIN:-$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)/poker-clock}"
if [ ! -x "$APP" ]; then
  echo "Poker Clock binary not found: $APP" >&2
  read -r _
  exit 1
fi

if ! osascript - "$APP" <<'APPLESCRIPT'
on run argv
  set appPath to item 1 of argv
  tell application "Terminal" to activate
  tell application "Terminal" to do script quoted form of appPath
  delay 0.5
  try
    tell application "System Events"
      tell process "Terminal"
        set bounds of front window to {0, 0, 1280, 900}
      end tell
    end tell
  end try
  try
    tell application "System Events"
      tell process "Terminal"
        set value of attribute "AXFullScreen" of front window to true
      end tell
    end tell
  end try
end run
APPLESCRIPT
then
  echo "Terminal opened, but fullscreen could not be requested. Use the green window control or run in an existing terminal."
fi
