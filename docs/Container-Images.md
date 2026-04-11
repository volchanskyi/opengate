# Container Images

OpenGate server images are built and published to GitHub Container Registry (GHCR) on every push to `main`.

## Registry

```
ghcr.io/volchanskyi/opengate-server
```

## Image Tags

| Tag pattern | When created | Example |
|---|---|---|
| `sha-<short>` | Every push to `main` | `sha-a1b2c3d` |
| `latest` | Every push to `main` (overwritten) | `latest` |
| `v<major>.<minor>.<patch>` | Semver tags | `v1.0.0` |
| `v<major>.<minor>` | Semver tags | `v1.0` |

## Architectures

Images are built for **linux/amd64** and **linux/arm64** using Docker Buildx with QEMU emulation. ARM64 is required for deployment on Oracle Cloud A1 Flex (Ampere) and Hetzner CAX (Ampere) instances.

## Dockerfile

Multi-stage build in the repository root (`/Dockerfile`):

| Stage | Base image | Purpose |
|---|---|---|
| `web-build` | `node:24-alpine` | Install npm dependencies, build React SPA (`npm run build`) |
| `server-build` | `golang:1.26-alpine` | Download Go modules, compile `meshserver` binary (`CGO_ENABLED=0`) |
| Final | `alpine:3.20` | Minimal runtime with the binary and web assets |

The Go binary uses `modernc.org/sqlite` (pure Go), so `CGO_ENABLED=0` produces a fully static binary.

### Final image contents

```
/usr/local/bin/meshserver    # Go server binary
/srv/web/                    # Built web assets (React SPA)
```

### Runtime configuration

The container runs as a non-root `opengate` user by default.

**Exposed ports:**
- `8080` (TCP) — HTTP REST API
- `4433` (TCP) — MPS (Intel AMT CIRA)
- `9090/udp` — QUIC agent connections (mTLS)

**Required environment:**
- `JWT_SECRET` — JWT signing secret

**Default entrypoint:**
```
meshserver -listen :8080 -quic-listen :9090 -mps-listen :4433 -data-dir /data -web-dir /srv/web
```

**Volumes:**
- `/data` — persistent storage for SQLite database (`opengate.db`) and CA certificates (`ca.crt`, `ca.key`). Auto-created on first startup.

### Running locally

```bash
# Build
docker build -t opengate-server .

# Run
docker run -e JWT_SECRET=changeme \
  -p 8080:8080 -p 4433:4433 -p 9090:9090/udp \
  -v opengate-data:/data \
  opengate-server

# Verify
curl http://localhost:8080/api/v1/health
# → {"status":"ok"}
```

## Build Pipeline

The `build-image.yml` GitHub Actions workflow triggers on pushes to `main`:

```
dev push → CI (19 gates) → merge-to-main → pushes to main
  → build-image.yml fires
    → multi-arch build (amd64 + arm64) with SLSA provenance + SBOM
    → push to GHCR with sha + latest tags
    → cosign keyless signing (Sigstore OIDC)
    → SBOM attestation (SPDX JSON via anchore/sbom-action)
    → Trivy vulnerability scan
```

### Supply Chain Security

Every pushed image is signed and attested using [Sigstore](https://sigstore.dev/) keyless signing:

1. **Cosign keyless signing** — `sigstore/cosign-installer@v3` + `cosign sign --yes` using GitHub Actions OIDC identity. No secret keys to manage — the signature is tied to the workflow identity and verified against the Sigstore transparency log.
2. **SLSA provenance** — `docker/build-push-action@v7` with `provenance: true` embeds [SLSA Build Level 2](https://slsa.dev/) provenance as an OCI attestation.
3. **SBOM generation** — `anchore/sbom-action@v0` generates an SPDX JSON SBOM from the built image, then `cosign attest` attaches it as a signed attestation to the image digest.

#### Verifying an image

```bash
# Install cosign: https://docs.sigstore.dev/cosign/system_config/installation/

# Verify signature (keyless — checks Sigstore transparency log)
cosign verify \
  --certificate-identity-regexp="https://github.com/volchanskyi/.*" \
  --certificate-oidc-issuer="https://token.actions.githubusercontent.com" \
  ghcr.io/volchanskyi/opengate-server:latest

# Verify SBOM attestation
cosign verify-attestation \
  --type spdxjson \
  --certificate-identity-regexp="https://github.com/volchanskyi/.*" \
  --certificate-oidc-issuer="https://token.actions.githubusercontent.com" \
  ghcr.io/volchanskyi/opengate-server:latest
```

### Trivy Vulnerability Scan

After signing, the workflow runs a Trivy container image scan (`aquasecurity/trivy-action@0.35.0`) targeting CRITICAL and HIGH severity vulnerabilities. The scan blocks the workflow on any findings.

### Caching

Docker layer caching uses GitHub Actions cache (`type=gha`) to avoid rebuilding unchanged layers. The Go module download and npm install stages benefit most from this.

### Pulling images

```bash
# Latest
docker pull ghcr.io/volchanskyi/opengate-server:latest

# Specific commit
docker pull ghcr.io/volchanskyi/opengate-server:sha-a1b2c3d
```

## .dockerignore

The `.dockerignore` excludes Git metadata, build artifacts, node_modules, test data, documentation, deploy configs, and IDE files to keep the build context minimal and avoid leaking sensitive data into image layers.
