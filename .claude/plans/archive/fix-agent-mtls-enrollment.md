# Fix: Agent mTLS Enrollment (CSR-based Certificate Signing)

## Context

The agent crashes on every QUIC connection with TLS error 48 (`unknown_ca`). Root cause: the agent generates a **self-signed** certificate (`identity.rs:81` — `params.self_signed(&key_pair)`), but the server requires client certs signed by its CA (`cert.go:142` — `ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: pool`). There is no mechanism for the agent to get a CA-signed cert. The existing enrollment endpoint only returns the CA PEM — it never signs anything.

## Approach: Agent-driven CSR enrollment on first boot

The agent handles enrollment itself using new CLI flags. On first boot (no identity files exist), it generates a key pair, creates a PKCS#10 CSR, POSTs it to the enrollment endpoint, and saves the CA-signed cert.

## Changes

### 1. OpenAPI spec — `api/openapi.yaml`
- Add optional JSON request body to `POST /api/v1/enroll/{token}` with `csr_pem` (string, required)
- Add `cert_pem` (string) to `EnrollResponse`

### 2. Server: CSR signing — `server/internal/cert/cert.go`
- Add `SignAgentCSR(csrDER []byte) ([]byte, error)` method to `Manager`
  - Parses CSR, validates signature
  - Extracts public key and subject from CSR
  - Creates CA-signed cert (same template as `SignAgent` but using CSR's public key)
  - Returns DER-encoded signed cert

### 3. Server: enrollment handler — `server/internal/api/handlers_enrollment.go`
- Parse `csr_pem` from request body (PEM-decode → DER)
- Call `cert.SignAgentCSR()` to sign it
- PEM-encode the signed cert and include as `cert_pem` in response

### 4. Server: regenerate types
- Run `oapi-codegen` to regenerate `server/internal/api/openapi_gen.go`
- `EnrollRequestObject` will gain a `Body` field; `EnrollResponse` gains `CertPem`

### 5. Agent: CSR generation — `agent/crates/mesh-agent-core/src/identity.rs`
- Add `generate_csr(data_dir) -> Result<(Vec<u8>, Vec<u8>, DeviceId)>` (returns CSR DER, key DER, device_id)
  - Generates ECDSA P-256 key pair
  - Creates CSR with CN=device_id using `rcgen::CertificateSigningRequestParams`
  - Saves `device_id.txt` and `agent.key` to disk (cert saved after enrollment)
  - Returns CSR DER for submission

### 6. Agent: enrollment flow — `agent/crates/mesh-agent/src/main.rs`
- Add optional CLI flags: `--enroll-url` (`OPENGATE_ENROLL_URL`) and `--enroll-token` (`OPENGATE_ENROLL_TOKEN`)
- Before QUIC connect, if no identity exists AND enroll flags are set:
  1. Call `generate_csr()` to get CSR DER + key
  2. PEM-encode CSR
  3. HTTP POST to `{enroll_url}/api/v1/enroll/{token}` with `{"csr_pem": "..."}`
  4. Parse response: extract `ca_pem` and `cert_pem`
  5. Save `ca_pem` to `--server-ca` path, PEM-decode `cert_pem` and save DER to `agent.crt`
  6. Continue with normal QUIC connect using the CA-signed cert
- If no identity exists and no enroll flags: fail with clear error message
- Add `reqwest` crate for HTTP client (or use hyper — reqwest is simpler)

### 7. Install script — `server/internal/api/install.sh`
- Remove the manual `curl` enrollment call
- Pass `--enroll-url` and `--enroll-token` to agent in the systemd ExecStart
- Make `--server-ca` and `--server-addr` optional (derived from enrollment response)
- On first boot, agent self-enrolls; on restarts, uses saved identity

## Files to modify
1. `api/openapi.yaml` — add CSR request body + cert_pem response
2. `server/internal/cert/cert.go` — add `SignAgentCSR()`
3. `server/internal/cert/cert_test.go` — test CSR signing
4. `server/internal/api/openapi_gen.go` — regenerate
5. `server/internal/api/handlers_enrollment.go` — sign CSR in enrollment
6. `server/internal/api/install.sh` — use agent-driven enrollment
7. `agent/crates/mesh-agent-core/src/identity.rs` — add CSR generation
8. `agent/crates/mesh-agent/src/main.rs` — add enroll flags + first-boot flow
9. `agent/crates/mesh-agent/Cargo.toml` — add `reqwest` dep

## Verification
1. `make build` — all components compile
2. `make test` — unit tests pass (new tests for CSR signing + identity CSR gen)
3. Server integration tests: enrollment with CSR returns signed cert
4. Manual E2E: install script → agent enrolls → QUIC connects → no error 48
