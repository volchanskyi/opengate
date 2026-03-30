# Enable Web UI in Staging and Production

## Context

The web UI (React SPA, Vite build) is already compiled and copied into the Docker image at `/srv/web` (Dockerfile line 23: `COPY --from=web-build /build/web/dist /srv/web`). However, **nothing serves these files** — the Go server only handles `/api/v1/*` and `/ws/relay/*`, and Caddy just reverse-proxies everything to the Go server. Visiting `https://opengate.cloudisland.net/` returns a 404.

## Approach: Serve from Caddy via shared volume

Caddy is the right place to serve static files — it already handles TLS, gzip, and security headers. The web assets live inside the server container image, so we use a one-shot init service to copy them into a shared Docker volume that Caddy mounts read-only.

## Changes

### 1. `deploy/docker-compose.yml` — add `web-init` service and shared volume

Add a `web-init` one-shot service that copies `/srv/web` from the server image into a shared `web-assets` volume. Caddy mounts this volume and depends on the copy completing.

- Add `web-init` service using same image as `server`, entrypoint: `sh -c "rm -rf /web-assets/* && cp -a /srv/web/. /web-assets/"`
- `restart: "no"` (run once per deploy)
- Add `web-assets` named volume
- Mount `web-assets:/srv/web:ro` in `caddy` service
- Add `depends_on: web-init: condition: service_completed_successfully` to `caddy`

### 2. `deploy/docker-compose.staging.yml` — add staging container name

Add `web-init` override with `container_name: opengate-web-init-staging` to avoid name collision with production (both run on same VPS).

### 3. `deploy/caddy/Caddyfile` — route API/WS to server, serve SPA for everything else

Replace the blanket `reverse_proxy` with `handle` blocks:

```
handle /api/* → reverse_proxy server:8080 (with health check)
handle /ws/*  → reverse_proxy server:8080
handle        → root * /srv/web, try_files {path} /index.html, file_server
```

Cache headers:
- `/assets/*` (Vite hashed filenames): `Cache-Control: public, max-age=31536000, immutable`
- Everything else (index.html): `Cache-Control: no-cache, no-store, must-revalidate`

Keep existing security headers, gzip, logging.

### 4. `deploy/caddy/Caddyfile.staging` — same routing, no security headers

Same `handle` structure as production but on `:80` without HSTS/security headers (HTTP-only staging).

### 5. `deploy/scripts/smoke-test.sh` — add web UI smoke tests

Add tests that run in **both** modes (after health check, before staging-only tests):

- `GET /` returns 200 with `<div id="root">` (proves index.html is served)
- `GET /devices` returns 200 (proves SPA fallback works — React Router path, no real file)
- `GET /vite.svg` returns 200 (proves static file serving works)

## Files to modify

| File | Change |
|------|--------|
| `deploy/docker-compose.yml` | Add `web-init` service, `web-assets` volume, mount in caddy |
| `deploy/docker-compose.staging.yml` | Add `web-init` container name for staging |
| `deploy/caddy/Caddyfile` | Replace blanket proxy with handle blocks + SPA file_server |
| `deploy/caddy/Caddyfile.staging` | Same pattern as production, HTTP-only |
| `deploy/scripts/smoke-test.sh` | Add 3 web UI smoke tests (both modes) |

No changes needed: Dockerfile (already copies web to `/srv/web`), Go server, cd.yml.

## Verification

1. `make lint-deploy` — validates compose config, Caddy format, Trivy scan, integration tests
2. `~/go/bin/actionlint` — workflow lint (no workflow changes, but run anyway)
3. After deploy: `curl -sf https://opengate.cloudisland.net/` should return HTML with `<div id="root">`
4. After deploy: `curl -sf https://opengate.cloudisland.net/devices` should return same HTML (SPA fallback)
5. After deploy: `curl -sf https://opengate.cloudisland.net/api/v1/health` should still return JSON
6. Smoke tests pass in both staging and production
