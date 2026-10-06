#!/usr/bin/env bash
# Delivers one message to the Telegram chat the nightlies report into and fails when it is refused.
# Each failure path exits non-zero, so a step that could not deliver reports it.
#
# Environment:
#   TELEGRAM_BOT_TOKEN  (required) the bot the message is sent as
#   TELEGRAM_CHAT_ID    (required) the chat it is sent to
#   TELEGRAM_API_BASE             where to send it (default https://api.telegram.org)
#
# Usage: telegram-alert.sh <message>       or       ... | telegram-alert.sh
set -euo pipefail

API_BASE="${TELEGRAM_API_BASE:-https://api.telegram.org}"

# Telegram refuses a longer message; the head is cut because the tail says where to look.
CEILING=4000

refuse() {
  echo "::error::$1" >&2
  exit 1
}

# An argument is the message and no argument reads standard input; an empty argument is an empty
# message, so the send never blocks on a pipe.
if [ "$#" -gt 0 ]; then
  message="$1"
else
  message="$(cat)"
fi

[ -n "${TELEGRAM_BOT_TOKEN:-}" ] || refuse "TELEGRAM_BOT_TOKEN is not set, so this alert reached nobody."
[ -n "${TELEGRAM_CHAT_ID:-}" ] || refuse "TELEGRAM_CHAT_ID is not set, so this alert reached nobody."
[ -n "$message" ] || refuse "telegram-alert.sh was given no message to send."

if [ "${#message}" -gt "$CEILING" ]; then
  message="(the first $(("${#message}" - CEILING)) characters are cut; the run's log carries all of it)"$'\n\n'"${message: -CEILING}"
fi

body="$(mktemp)"
trap 'rm -f "$body"' EXIT

# A revoked token answers at getMe and a refused chat at the send, and the two have different fixes.
getme_http="$(curl -sS --max-time 10 -o "$body" -w '%{http_code}' \
  "${API_BASE}/bot${TELEGRAM_BOT_TOKEN}/getMe" 2>/dev/null || echo "000")"
if [ "$getme_http" != "200" ] || ! jq -e '.ok == true' "$body" >/dev/null 2>&1; then
  echo "Telegram answered: $(cat "$body" 2>/dev/null || echo '(nothing)')" >&2
  refuse "Telegram getMe returned HTTP ${getme_http} — the bot token is invalid, revoked, or unreachable."
fi

send_http="$(curl -sS --max-time 30 -o "$body" -w '%{http_code}' \
  -X POST "${API_BASE}/bot${TELEGRAM_BOT_TOKEN}/sendMessage" \
  --data-urlencode "chat_id=${TELEGRAM_CHAT_ID}" \
  --data-urlencode "text=${message}" \
  --data-urlencode "disable_web_page_preview=true" 2>/dev/null || echo "000")"

# Telegram answers `ok:false` inside a 200 when the bot left the chat, so the body is read.
if [ "$send_http" != "200" ] || ! jq -e '.ok == true' "$body" >/dev/null 2>&1; then
  echo "Telegram answered: $(cat "$body" 2>/dev/null || echo '(nothing)')" >&2
  refuse "Telegram sendMessage returned HTTP ${send_http} — the chat id may name a chat the bot is not in."
fi

echo "Telegram alert delivered."
