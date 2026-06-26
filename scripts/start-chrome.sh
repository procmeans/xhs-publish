#!/usr/bin/env bash
# Start Chrome with a dedicated profile and remote debugging enabled, then log
# into Xiaohongshu ONCE in the window that opens. Leave this Chrome running;
# xhspublish attaches to it over CDP and reuses the session.
set -euo pipefail

PORT="${CDP_PORT:-9222}"
PROFILE="${CHROME_PROFILE:-$HOME/.xhs-chrome-profile}"

# Locate Chrome (macOS default first, then PATH).
CHROME="/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
if [[ ! -x "$CHROME" ]]; then
  CHROME="$(command -v google-chrome || command -v chromium || true)"
fi
if [[ -z "${CHROME:-}" || ! -x "$CHROME" ]]; then
  echo "Could not find Google Chrome / Chromium. Install it or set CHROME=." >&2
  exit 1
fi

mkdir -p "$PROFILE"
echo "Launching Chrome on debug port $PORT with profile $PROFILE"
echo "→ Log into https://creator.xiaohongshu.com once, then keep this window open."

exec "$CHROME" \
  --remote-debugging-port="$PORT" \
  --user-data-dir="$PROFILE" \
  --no-first-run \
  --no-default-browser-check \
  "https://creator.xiaohongshu.com/login"
