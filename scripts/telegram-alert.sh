#!/usr/bin/env bash
# Deliver one message to the Telegram chat the nightlies report into, and say so
# when it could not be delivered.
#
# Every workflow carried its own copy of this send, and every copy ended its
# failure paths with `exit 0`: a step that tried to deliver and could not was
# green. The step's conclusion and the delivery were different facts and only one
# of them was ever read. The verdict each of these workflows publishes lives in a
# job of its own, so nothing depends on the send succeeding — which leaves the
# send free to report what actually happened.
#
# Environment:
#   TELEGRAM_BOT_TOKEN  (required) the bot the message is sent as
#   TELEGRAM_CHAT_ID    (required) the chat it is sent to
#   TELEGRAM_API_BASE             where to send it (default https://api.telegram.org)
#
# Usage: telegram-alert.sh <message>       or       ... | telegram-alert.sh
set -euo pipefail

API_BASE="${TELEGRAM_API_BASE:-https://api.telegram.org}"

# Telegram refuses a message past this, and losing the alert because the finding
# was long is the failure this whole script exists to close. The tail is what
# says where to look, so the head is what gets cut.
CEILING=4000

refuse() {
  echo "::error::$1" >&2
  exit 1
}

# An argument is the message. With no argument at all the message is read from
# standard input, so a caller can pipe a report in. An argument that is empty is
# an empty message rather than an invitation to wait on a pipe: a send that
# blocks forever is the alert not arriving, slowly.
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

# Does the token still work? A revoked one answers here rather than at the send,
# and the two have different fixes.
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

# A 200 is not a delivery. Telegram answers `ok:false` inside a 200 when the bot
# has been removed from the chat it is addressing, which is the one refusal a
# status check alone reports as success.
if [ "$send_http" != "200" ] || ! jq -e '.ok == true' "$body" >/dev/null 2>&1; then
  echo "Telegram answered: $(cat "$body" 2>/dev/null || echo '(nothing)')" >&2
  refuse "Telegram sendMessage returned HTTP ${send_http} — the chat id may name a chat the bot is not in."
fi

echo "Telegram alert delivered."
