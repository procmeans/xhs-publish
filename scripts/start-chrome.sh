#!/usr/bin/env bash
# Start Chrome with a dedicated profile and remote debugging enabled, then log
# into Xiaohongshu AND Douyin ONCE in the tabs that open. Leave this Chrome
# running; xhspublish attaches to it over CDP and reuses the sessions for both
# platforms (-platform xhs|douyin).
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
echo "→ Log into BOTH 小红书 and 抖音 in the tabs that open, then keep this window open."

exec "$CHROME" \
  --remote-debugging-port="$PORT" \
  --user-data-dir="$PROFILE" \
  --no-first-run \
  --no-default-browser-check \
  "https://creator.xiaohongshu.com/login" \
  "https://creator.douyin.com/"
