#!/usr/bin/env bash
# The smoke run that goes through the public edge, held to the thing it claims.
#
# Two of its checks assert an absence — that the exposition and the profiler are
# not what the edge answers with. An absence is what a refused request looks
# like too, so both of them pass when nothing answered at all: an unresolvable
# name, a closed port, a TLS handshake against a plain-HTTP edge. That is the
# false green ci-cd-determinism rules against, and it is not hypothetical — the
# staging edge is HTTP-only and its Ingress matches a host with no public record,
# so the run reached nothing and reported the boundary green twice.
#
# So the script is driven against a stub edge here: one that answers soundly,
# one that answers with the exposition, and one that does not answer at all.
# A boundary check has to pass on the first, fail on the second, and fail on the
# third.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
SMOKE="$REPO_ROOT/deploy/scripts/smoke-test.sh"

PASS=0
FAIL=0
FAILURES=()

pass() {
  PASS=$((PASS + 1))
  printf '  ok   %s\n' "$1"
}

fail() {
  FAIL=$((FAIL + 1))
  FAILURES+=("$1")
  printf '  FAIL %s\n' "$1" >&2
}

command -v python3 >/dev/null 2>&1 \
  || {
    echo "python3 is required to stand up the stub edge" >&2
    exit 1
  }

WORK="$(mktemp -d)"
STUB_PID=""
# shellcheck disable=SC2329 # invoked by the EXIT trap below
cleanup() {
  [ -n "$STUB_PID" ] && kill "$STUB_PID" 2>/dev/null
  rm -rf "$WORK"
}
trap cleanup EXIT

# --- the stub edge ------------------------------------------------------------
#
# Mirrors the shape the real ingress presents: one catch-all rule that sends
# every unrouted path to the SPA, which is why a boundary check has to read the
# body rather than the status.

cat >"$WORK/edge.py" <<'PYEOF'
import sys
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

MODE = sys.argv[1]
PORT_FILE = sys.argv[2]

SPA = b'<!doctype html>\n<html><head><title>web</title></head>\n<body><div id="root"></div></body></html>\n'
EXPOSITION = (
    b"# HELP opengate_http_requests_total Total HTTP requests.\n"
    b"# TYPE opengate_http_requests_total counter\n"
    b'opengate_http_requests_total{code="200"} 42\n'
)
# The same registry before it has answered a single request. A labelled counter
# publishes no sample until one of its label sets is incremented, so this is
# what the exposition looks like for the first moments of a server's life — and
# a check keyed on the request counter reads it as no exposition at all.
QUIET_EXPOSITION = (
    b"# HELP opengate_relay_active_sessions Number of active relay sessions.\n"
    b"# TYPE opengate_relay_active_sessions gauge\n"
    b"opengate_relay_active_sessions 0\n"
    b"# HELP go_goroutines Number of goroutines that currently exist.\n"
    b"# TYPE go_goroutines gauge\n"
    b"go_goroutines 24\n"
)
PROFILER = b"<html><body>Types of profiles available:<br>heap<br>goroutine</body></html>\n"


class Edge(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def log_message(self, *args):
        pass

    def _send(self, status, body, ctype="text/html; charset=utf-8"):
        self.send_response(status)
        self.send_header("Content-Type", ctype)
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        path = self.path.split("?", 1)[0]
        if path == "/api/v1/health":
            self._send(200, b'{"status":"ok"}', "application/json")
        elif path == "/api/v1/sites":
            self._send(200, b"[]", "application/json")
        elif path == "/vite.svg":
            self._send(200, b"<svg xmlns='http://www.w3.org/2000/svg'></svg>", "image/svg+xml")
        elif path == "/metrics":
            # A breached edge routes the internal listener; a sound one has no
            # rule for the path and falls through to the SPA. "quiet" is a
            # breached edge in front of a server that has answered nothing yet.
            if MODE == "breached":
                self._send(200, EXPOSITION, "text/plain; charset=utf-8")
            elif MODE == "quiet":
                self._send(200, QUIET_EXPOSITION, "text/plain; charset=utf-8")
            else:
                self._send(200, SPA, "text/html")
        elif path == "/debug/pprof/":
            self._send(200, PROFILER if MODE in ("breached", "quiet") else SPA)
        else:
            self._send(200, SPA)

    def do_POST(self):
        length = int(self.headers.get("Content-Length") or 0)
        self.rfile.read(length)
        if self.path.split("?", 1)[0] == "/api/v1/auth/register":
            self._send(201, b'{"token":"stub-jwt-value"}', "application/json")
        else:
            self._send(200, SPA)


server = ThreadingHTTPServer(("127.0.0.1", 0), Edge)
with open(PORT_FILE, "w", encoding="utf-8") as fh:
    fh.write(str(server.server_address[1]))
server.serve_forever()
PYEOF

start_stub() {
  local mode="$1"
  [ -n "$STUB_PID" ] && kill "$STUB_PID" 2>/dev/null || true
  rm -f "$WORK/port"
  # Detached from the caller's stdout: this runs inside a command
  # substitution, which waits for the pipe to close and would otherwise wait on
  # a server that never exits.
  python3 "$WORK/edge.py" "$mode" "$WORK/port" >/dev/null 2>>"$WORK/stub.err" &
  STUB_PID=$!
  for _ in $(seq 1 100); do
    [ -s "$WORK/port" ] && {
      cat "$WORK/port"
      return 0
    }
    sleep 0.1
  done
  echo "stub edge never reported a port" >&2
  return 1
}

# A port nothing listens on: taken the same way the stub takes one, then given
# straight back. This is the shape of the CI failure — a name that resolves
# somewhere with nothing behind it.
closed_port() {
  python3 - <<'PYEOF'
import socket

s = socket.socket()
s.bind(("127.0.0.1", 0))
port = s.getsockname()[1]
s.close()
print(port)
PYEOF
}

# run_smoke OUT_FILE ARGS... — runs the script, records its output, returns its
# exit status.
run_smoke() {
  local out="$1"
  shift
  bash "$SMOKE" "$@" >"$out" 2>&1
}

# check_line OUT VERDICT NAME — did the named check report the given verdict?
check_line() {
  grep -qF "$2: $3" "$1"
}

echo "smoke test through the edge:"

# --- a sound edge: every check passes -----------------------------------------

PORT="$(start_stub sound)"
OUT="$WORK/sound.log"
if run_smoke "$OUT" --domain edge.test --mode staging \
  --scheme http --edge-address "127.0.0.1:${PORT}"; then
  pass "a run through a sound edge passes"
else
  fail "a run through a sound edge must pass (see $OUT)"
  cat "$OUT" >&2
fi

if check_line "$OUT" PASS "GET /metrics through the ingress is not the exposition" \
  && check_line "$OUT" PASS "GET /debug/pprof/ through the ingress is not the profiler"; then
  pass "the boundary checks pass against an edge that keeps the boundary"
else
  fail "the boundary checks must pass against an edge that keeps the boundary"
fi

# The name resolves nowhere; only --edge-address can have reached the stub, so a
# passing health check is the flag working.
if check_line "$OUT" PASS "GET /api/v1/health returns 200"; then
  pass "--edge-address reaches an edge whose host has no public record"
else
  fail "--edge-address must reach the edge behind a name DNS cannot resolve"
fi

# --- a breached edge: the boundary checks fail --------------------------------

PORT="$(start_stub breached)"
OUT="$WORK/breached.log"
if run_smoke "$OUT" --domain edge.test --mode staging \
  --scheme http --edge-address "127.0.0.1:${PORT}"; then
  fail "a run through an edge that serves the exposition must not pass"
else
  pass "a run through an edge that serves the exposition fails"
fi

if check_line "$OUT" FAIL "GET /metrics through the ingress is not the exposition"; then
  pass "the exposition on the edge is reported as a failure"
else
  fail "an edge serving the exposition must fail its boundary check"
fi

if check_line "$OUT" FAIL "GET /debug/pprof/ through the ingress is not the profiler"; then
  pass "the profiler on the edge is reported as a failure"
else
  fail "an edge serving the profiler must fail its boundary check"
fi

# --- an edge that does not answer: the boundary checks must not pass ----------
#
# The whole point. Nothing is listening, so every request comes back empty, and
# an absence-shaped check reads that as the absence it wanted.

kill "$STUB_PID" 2>/dev/null || true
STUB_PID=""
DEAD_PORT="$(closed_port)"
OUT="$WORK/refused.log"
if run_smoke "$OUT" --domain edge.test --mode staging \
  --scheme http --edge-address "127.0.0.1:${DEAD_PORT}"; then
  fail "a run that reached no edge at all must not pass"
else
  pass "a run that reached no edge at all fails"
fi

if check_line "$OUT" PASS "GET /metrics through the ingress is not the exposition"; then
  fail "the exposition boundary reported a pass on a request nothing answered"
else
  pass "the exposition boundary does not pass on a request nothing answered"
fi

if check_line "$OUT" PASS "GET /debug/pprof/ through the ingress is not the profiler"; then
  fail "the profiler boundary reported a pass on a request nothing answered"
else
  pass "the profiler boundary does not pass on a request nothing answered"
fi

if check_line "$OUT" PASS "GET /ws/relay route exists (non-404)"; then
  fail "the relay route reported a pass on a request nothing answered"
else
  pass "the relay route does not pass on a request nothing answered"
fi

# --- the scheme --------------------------------------------------------------
#
# Production's edge terminates TLS and is reached by name alone, so a domain run
# that is told nothing must still ask for https. Asserted against a plain-HTTP
# stub, which such a run cannot talk to.

PORT="$(start_stub sound)"
OUT="$WORK/scheme.log"
if run_smoke "$OUT" --domain edge.test --mode staging \
  --edge-address "127.0.0.1:${PORT}"; then
  fail "a domain run with no --scheme must ask for https, and must not reach a plain-HTTP edge"
else
  pass "a domain run with no --scheme asks for https"
fi

if check_line "$OUT" PASS "GET /metrics through the ingress is not the exposition"; then
  fail "the exposition boundary reported a pass on a handshake that failed"
else
  pass "the exposition boundary does not pass on a handshake that failed"
fi

# --- the flag belongs to the domain run --------------------------------------

OUT="$WORK/misuse.log"
if run_smoke "$OUT" --host 127.0.0.1 --port 1 --metrics-port 2 \
  --mode local --edge-address 127.0.0.1; then
  fail "--edge-address without --domain must be refused"
else
  if grep -qF -- "--edge-address names the edge for --domain" "$OUT"; then
    pass "--edge-address without --domain is refused by name"
  else
    fail "--edge-address without --domain must say why it was refused"
  fi
fi

# --- the port-forward run still reads the listener directly -------------------
#
# The same stub stands in for both halves of the boundary. Through the edge, an
# exposition on /metrics is the breach; on the forwarded internal port it is
# exactly what the run is there to find. So the run that names --host has to
# pass against the very responses the run that names --domain fails on, and this
# is what says the shared curl arguments did not leak from one into the other.

PORT="$(start_stub breached)"
OUT="$WORK/forwarded.log"
if run_smoke "$OUT" --host 127.0.0.1 --port "$PORT" --metrics-port "$PORT" \
  --mode local --scheme http; then
  pass "a run against the forwarded listener passes"
else
  fail "a run against the forwarded listener must pass (see $OUT)"
  cat "$OUT" >&2
fi

if check_line "$OUT" PASS "GET /metrics returns Prometheus metrics" \
  && check_line "$OUT" PASS "GET /debug/pprof/ returns the profiler index"; then
  pass "the forwarded run reads the exposition and the profiler off the listener"
else
  fail "the forwarded run must read the exposition and the profiler off the listener"
fi

# --- a server that has answered nothing yet ----------------------------------
#
# A labelled counter publishes no sample until one of its label sets is
# incremented, so a freshly started server's exposition carries its gauges and
# none of its request counters. Both halves of the boundary have to survive
# that: the forwarded run must still recognise the exposition it is looking at,
# and the edge run must still call it a breach. Keying either on the request
# counter gets the first wrong on a restarted server and the second wrong on a
# newly rolled-out one — the more dangerous of the two, since it is the
# security boundary reporting green.

PORT="$(start_stub quiet)"
OUT="$WORK/quiet-forwarded.log"
if run_smoke "$OUT" --host 127.0.0.1 --port "$PORT" --metrics-port "$PORT" \
  --mode local --scheme http; then
  pass "the forwarded run passes against a server that has answered nothing yet"
else
  fail "the forwarded run must pass against a server that has answered nothing yet (see $OUT)"
  cat "$OUT" >&2
fi

if check_line "$OUT" PASS "GET /metrics returns Prometheus metrics"; then
  pass "the exposition is recognised before any request has been counted"
else
  fail "the exposition must be recognised before any request has been counted"
fi

OUT="$WORK/quiet-edge.log"
if run_smoke "$OUT" --domain edge.test --mode staging \
  --scheme http --edge-address "127.0.0.1:${PORT}"; then
  fail "an edge serving a freshly started server's exposition must not pass"
else
  pass "an edge serving a freshly started server's exposition fails"
fi

if check_line "$OUT" FAIL "GET /metrics through the ingress is not the exposition"; then
  pass "the boundary catches an exposition with no request counter in it"
else
  fail "the boundary must catch an exposition with no request counter in it"
fi

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
exit 0
