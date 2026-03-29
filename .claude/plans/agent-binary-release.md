# Agent Binary + Auto-Tag + Changelog

## Context

Two blockers prevent agent releases:
1. **Agent binary is a stub** — `main.rs` logs "QUIC control loop not yet implemented" and `exit(1)`. All building blocks exist in `mesh-agent-core` but aren't wired.
2. **No version tags** — `release-agent.yml` triggers on `v*` tags but nothing creates them.

The agent binary must work first, then auto-tagging will produce releases.

---

## Part 1: Wire the QUIC Control Loop (Prerequisite)

### Files to modify

- [agent/crates/mesh-agent/src/main.rs](agent/crates/mesh-agent/src/main.rs) — replace stub with full control loop
- [agent/crates/mesh-agent/Cargo.toml](agent/crates/mesh-agent/Cargo.toml) — add `quinn`, `rustls` deps for QUIC client

### Existing APIs to reuse (no new code in mesh-agent-core)

| API | Location | Purpose |
|-----|----------|---------|
| `AgentConnection::new(stream, config)` | [connection.rs](agent/crates/mesh-agent-core/src/connection.rs) | Wrap QUIC stream |
| `AgentConnection::send_control(msg)` | same | Send AgentRegister, heartbeats |
| `AgentConnection::receive_control()` | same | Receive SessionRequest, AgentUpdate |
| `AgentConnection::handle_session_request(...)` | same | Accept session, spawn SessionHandler |
| `AsyncControlStream::new(stream)` | same | Wrap quinn::SendStream+RecvStream |
| `reconnect_with_backoff(fn, max)` | same | Exponential backoff for reconnection |
| `AgentIdentity::load_or_create(dir)` | [identity.rs](agent/crates/mesh-agent-core/src/identity.rs) | Already called in main.rs |
| `apply_update(config, ver, url, sha, sig)` | [update.rs](agent/crates/mesh-agent-core/src/update.rs) | Download + verify + replace binary |
| `platform_linux::create_screen_capture()` | [platform-linux/src/lib.rs](agent/crates/platform-linux/src/lib.rs) | Factory → Box<dyn ScreenCapture> |
| `platform_linux::create_input_injector()` | same | Factory → Box<dyn InputInjector> |
| `platform_linux::create_service_lifecycle()` | same | Factory → Box<dyn ServiceLifecycle> |

### Implementation: main.rs control flow

Replace the TODO stub (lines 101-124) with:

```rust
// 1. Build platform implementations
let lifecycle = platform_linux::create_service_lifecycle();

// 2. QUIC client setup
let server_certs = rustls_pemfile::certs(&mut ca_pem.as_bytes())
    .collect::<Result<Vec<_>, _>>()
    .context("parse CA PEM")?;
let mut root_store = rustls::RootCertStore::empty();
for cert in server_certs {
    root_store.add(cert).context("add CA cert")?;
}

let client_cert = rustls::pki_types::CertificateDer::from(identity.cert_der.clone());
let client_key = rustls::pki_types::PrivateKeyDer::try_from(identity.key_der.clone())
    .map_err(|e| anyhow::anyhow!("parse private key: {e}"))?;

let tls_config = rustls::ClientConfig::builder()
    .with_root_certificates(root_store)
    .with_client_auth_cert(vec![client_cert], client_key)
    .context("build TLS config")?;

let quinn_config = quinn::ClientConfig::new(Arc::new(
    quinn::crypto::rustls::QuicClientConfig::try_from(tls_config)?
));

let endpoint = quinn::Endpoint::client("0.0.0.0:0".parse()?)?;

// 3. Notify systemd we're ready
lifecycle.notify_ready();

// 4. Connect with backoff + control loop (reconnect on error)
loop {
    let connect_result = mesh_agent_core::reconnect_with_backoff(
        || async {
            let conn = endpoint
                .connect_with(quinn_config.clone(), args.server_addr.parse()?, "server")?
                .await?;
            let (send, recv) = conn.open_bi().await?;
            Ok((send, recv))
        },
        10,
    ).await;

    let (send, recv) = match connect_result {
        Ok(streams) => streams,
        Err(e) => {
            error!(error = %e, "all reconnect attempts failed, exiting");
            lifecycle.notify_stopping();
            return Err(e.into());
        }
    };

    // Wrap QUIC streams into AsyncControlStream
    let stream = mesh_agent_core::AsyncControlStream::new(
        tokio::io::join(recv, send)
    );
    let mut conn = mesh_agent_core::AgentConnection::new(stream, config.clone());

    // Register with server
    conn.send_control(mesh_protocol::ControlMessage::AgentRegister {
        capabilities: vec![
            mesh_protocol::AgentCapability::Desktop,
            mesh_protocol::AgentCapability::Terminal,
            mesh_protocol::AgentCapability::FileTransfer,
        ],
        hostname: gethostname::gethostname().to_string_lossy().to_string(),
        os: std::env::consts::OS.to_string(),
        arch: std::env::consts::ARCH.to_string(),
        version: env!("CARGO_PKG_VERSION").to_string(),
    }).await.context("send AgentRegister")?;

    info!("registered with server, entering control loop");

    // 5. Control loop — dispatch messages until disconnect
    loop {
        match conn.receive_control().await {
            Ok(mesh_protocol::ControlMessage::SessionRequest {
                token, relay_url, permissions,
            }) => {
                let capture = platform_linux::create_screen_capture();
                let injector = platform_linux::create_input_injector();
                match conn.handle_session_request(
                    token, relay_url, permissions, capture, injector,
                ).await {
                    Ok(handle) => { let _ = handle; } // session runs independently
                    Err(e) => warn!(error = %e, "failed to accept session"),
                }
            }
            Ok(mesh_protocol::ControlMessage::AgentUpdate {
                version, url, signature,
            }) => {
                if let Some(ref uc) = update_config {
                    // AgentUpdate doesn't include sha256 — compute from downloaded binary
                    match mesh_agent_core::update::apply_update(
                        uc, &version, &url, "", &signature,
                    ).await {
                        Ok(true) => {
                            info!(version, "update applied, restarting");
                            lifecycle.notify_stopping();
                            std::process::exit(EXIT_CODE_RESTART);
                        }
                        Ok(false) => info!("update skipped (same version)"),
                        Err(e) => error!(error = %e, "update failed"),
                    }
                }
            }
            Ok(_other) => { /* ignore unknown messages */ }
            Err(mesh_agent_core::ConnectionError::Io(_)) => {
                warn!("connection lost, will reconnect");
                break; // break inner loop, outer loop reconnects
            }
            Err(e) => {
                // Ping/pong returns Io error — continue on those
                if e.to_string().contains("ping received") {
                    continue;
                }
                warn!(error = %e, "control error, will reconnect");
                break;
            }
        }
    }
}
```

### New dependencies for mesh-agent/Cargo.toml

```toml
quinn.workspace = true
rustls.workspace = true
rustls-pemfile = "2"
gethostname = "0.5"
```

Also add `rustls-pemfile` and `gethostname` to workspace deps in [agent/Cargo.toml](agent/Cargo.toml).

### Graceful shutdown

Add `tokio::signal` handler for SIGTERM/SIGINT that calls `lifecycle.notify_stopping()` and breaks the loop. Wrap the main loop in `tokio::select!` with the signal.

### Tests (TDD)

**Unit tests** (in main.rs or a separate test module):
1. CLI arg parsing with valid/invalid inputs
2. Verify `EXIT_CODE_RESTART` constant is 42

**Integration test** (new file `agent/crates/mesh-agent/tests/connection_test.rs`):
1. Spawn a mock QUIC server (quinn listener with test certs)
2. Agent connects, sends AgentRegister
3. Server sends SessionRequest → agent responds with SessionAccept
4. Server sends AgentUpdate → agent processes it
5. Verify reconnection after server drops connection

**Existing tests** already cover:
- `AgentConnection::send_control` / `receive_control` (connection.rs tests)
- `SessionHandler::run` (session.rs tests)
- `apply_update` (update.rs tests)
- Platform factories (platform-linux tests)

---

## Part 2: Auto-Tag Job with Changelog

### Design: Separate `auto-tag` Job

A separate job (not a step inside merge-to-main) for these reasons:
- **Separation of concerns** — merge logic stays clean, tagging/changelog logic is isolated
- **Independent failure** — a tagging failure doesn't block or roll back the merge
- **Better visibility** — distinct job in the Actions UI shows tag creation status

### Critical: GITHUB_TOKEN Won't Trigger Downstream Workflows

GitHub Actions security: pushes made with `GITHUB_TOKEN` **silently don't trigger** other workflows. If `auto-tag` pushes a `v*` tag with `GITHUB_TOKEN`, `release-agent.yml` will never fire.

**Solution:** Use `secrets.SYNC_TOKEN` (PAT) — already exists and is used by `sync-branches` for exactly this reason.

## Implementation

### Step 1: Create CHANGELOG.md (new file)

Follow [Keep a Changelog](https://keepachangelog.com) format:

```markdown
# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).
```

### Step 2: Add `auto-tag` job to [.github/workflows/ci.yml](.github/workflows/ci.yml)

Insert after `merge-to-main` job. The ordering within the job is:
1. Determine version bump from conventional commits
2. Generate changelog entry from merged commits
3. Commit changelog to main
4. Tag the changelog commit (so the tag includes the changelog update)
5. Push commit + tag together

```yaml
  auto-tag:
    name: Auto-tag release
    needs: [merge-to-main]
    if: needs.merge-to-main.outputs.merged == 'true'
    runs-on: ubuntu-latest
    concurrency:
      group: auto-tag
      cancel-in-progress: false
    permissions:
      contents: write
    steps:
      - uses: actions/checkout@v6
        with:
          fetch-depth: 0
          ref: main
          token: ${{ secrets.SYNC_TOKEN }}

      - name: Configure git
        run: |
          git config user.name "Ivan Volchanskyi"
          git config user.email "ivan.volchanskyi@gmail.com"

      - name: Determine version bump
        id: bump
        run: |
          # Find latest semver tag, default to v0.0.0
          LATEST=$(git tag -l 'v*' --sort=-v:refname | head -n1)
          LATEST="${LATEST:-v0.0.0}"
          SEMVER="${LATEST#v}"
          IFS='.' read -r MAJOR MINOR PATCH <<< "$SEMVER"

          # Get commits merged in the last merge commit (--no-ff merge)
          MERGE_SHA=$(git rev-parse HEAD)
          COMMITS=$(git log --format='%s' "${MERGE_SHA}^1..${MERGE_SHA}^2" 2>/dev/null || echo "")

          if [ -z "$COMMITS" ]; then
            echo "No new commits to analyze"
            echo "skip=true" >> "$GITHUB_OUTPUT"
            exit 0
          fi

          # Determine bump level (highest wins)
          BUMP=""
          if echo "$COMMITS" | grep -qiE 'BREAKING CHANGE|^[a-z]+(\(.*\))?!:'; then
            BUMP="major"
          elif echo "$COMMITS" | grep -qE '^feat(\(.*\))?:'; then
            BUMP="minor"
          elif echo "$COMMITS" | grep -qE '^fix(\(.*\))?:'; then
            BUMP="patch"
          fi

          if [ -z "$BUMP" ]; then
            echo "No release-worthy commits (ci/style/refactor/docs/test only)"
            echo "skip=true" >> "$GITHUB_OUTPUT"
            exit 0
          fi

          case "$BUMP" in
            major) MAJOR=$((MAJOR + 1)); MINOR=0; PATCH=0 ;;
            minor) MINOR=$((MINOR + 1)); PATCH=0 ;;
            patch) PATCH=$((PATCH + 1)) ;;
          esac

          NEW_TAG="v${MAJOR}.${MINOR}.${PATCH}"
          echo "Bump: $BUMP → $NEW_TAG (was $LATEST)"
          echo "tag=$NEW_TAG" >> "$GITHUB_OUTPUT"
          echo "version=${MAJOR}.${MINOR}.${PATCH}" >> "$GITHUB_OUTPUT"
          echo "prev=$LATEST" >> "$GITHUB_OUTPUT"
          echo "skip=false" >> "$GITHUB_OUTPUT"

      - name: Generate changelog entry
        if: steps.bump.outputs.skip != 'true'
        run: |
          TAG="${{ steps.bump.outputs.tag }}"
          MERGE_SHA=$(git rev-parse HEAD)
          DATE=$(date -u +%Y-%m-%d)

          # Collect commits by category
          ADDED=""
          FIXED=""
          CHANGED=""

          while IFS= read -r line; do
            [ -z "$line" ] && continue
            # Strip type prefix, keep scope and description
            MSG=$(echo "$line" | sed -E 's/^[a-z]+(\([^)]*\))?!?:\s*//')
            SCOPE=$(echo "$line" | sed -nE 's/^[a-z]+\(([^)]*)\).*$/\1/p')
            [ -n "$SCOPE" ] && MSG="**$SCOPE:** $MSG"

            if echo "$line" | grep -qE '^feat(\(.*\))?:'; then
              ADDED="${ADDED}\n- ${MSG}"
            elif echo "$line" | grep -qE '^fix(\(.*\))?:'; then
              FIXED="${FIXED}\n- ${MSG}"
            elif echo "$line" | grep -qE '^(refactor|perf)(\(.*\))?:'; then
              CHANGED="${CHANGED}\n- ${MSG}"
            fi
          done <<< "$(git log --format='%s' "${MERGE_SHA}^1..${MERGE_SHA}^2" 2>/dev/null)"

          # Build the new entry
          ENTRY="## [${TAG}] - ${DATE}"
          [ -n "$ADDED" ]  && ENTRY="${ENTRY}\n\n### Added$(echo -e "$ADDED")"
          [ -n "$FIXED" ]  && ENTRY="${ENTRY}\n\n### Fixed$(echo -e "$FIXED")"
          [ -n "$CHANGED" ] && ENTRY="${ENTRY}\n\n### Changed$(echo -e "$CHANGED")"

          # Insert after the header (after the "adheres to Semantic Versioning" line)
          if [ -f CHANGELOG.md ]; then
            # Insert new entry after the header block (line 5, after the blank line following header)
            awk -v entry="$(echo -e "$ENTRY")" '
              /^## \[/ && !inserted { print entry; print ""; inserted=1 }
              { print }
              ENDFILE { if (!inserted) { print ""; print entry } }
            ' CHANGELOG.md > CHANGELOG.tmp
            mv CHANGELOG.tmp CHANGELOG.md
          else
            # Create CHANGELOG.md from scratch
            {
              echo "# Changelog"
              echo ""
              echo "All notable changes to this project will be documented in this file."
              echo ""
              echo "The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),"
              echo "and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html)."
              echo ""
              echo -e "$ENTRY"
              echo ""
            } > CHANGELOG.md
          fi

      - name: Commit changelog and tag
        if: steps.bump.outputs.skip != 'true'
        run: |
          TAG="${{ steps.bump.outputs.tag }}"

          # Check if tag already exists (idempotency)
          if git rev-parse "$TAG" >/dev/null 2>&1; then
            echo "Tag $TAG already exists — skipping"
            exit 0
          fi

          git add CHANGELOG.md
          git commit -m "docs: update CHANGELOG for $TAG [skip ci]"
          git tag "$TAG"
          git push origin main "$TAG"
          echo "Pushed CHANGELOG commit + $TAG → release-agent.yml will trigger"
```

### Step 3: Update dependent jobs in ci.yml

**`sync-branches`**: Change `needs: [merge-to-main]` → `needs: [merge-to-main, auto-tag]`
- Ensures branch sync happens after changelog commit + tag (avoids race on main ref)
- Also update the `if` condition to reference auto-tag

**`notify-failure`**: Add `auto-tag` to the `needs:` array so failures are reported.

## Key Design Decisions

| Decision | Rationale |
|----------|-----------|
| **Keep a Changelog format** | Industry standard, human-readable, structured sections |
| **Changelog committed before tag** | Tag points to a commit that includes its own changelog entry |
| **`[skip ci]` on changelog commit** | Prevents infinite loop (push to main → CI → merge → tag → push…) |
| **Pure bash, no external tools** | Zero supply chain risk, no npm/cargo install in CI |
| **Only `feat:`, `fix:`, `refactor:`, `perf:` in changelog** | `ci:`, `style:`, `docs:`, `test:`, `chore:` are not user-facing |
| **Scope extraction** (`feat(auth):`) | Displayed as bold prefix: `**auth:** description` |
| **Three sections: Added/Fixed/Changed** | Maps to feat/fix/refactor+perf — minimal, clean |
| **Single `git push origin main "$TAG"`** | Atomic push of commit + tag avoids partial state |
| **SYNC_TOKEN for checkout + push** | PAT triggers downstream workflows (release-agent.yml) |

## End-to-End Flow

```
dev push
  → CI jobs (17 checks)
  → merge-to-main (merges dev→main with GITHUB_TOKEN)
  → auto-tag:
      1. Scan merged commits for feat:/fix: prefixes
      2. Compute next semver (v0.0.0 → v0.1.0)
      3. Generate changelog entry (Added/Fixed/Changed sections)
      4. Commit CHANGELOG.md [skip ci]
      5. Tag the changelog commit
      6. Push commit + tag (SYNC_TOKEN)
  → release-agent.yml (triggered by v* tag push)
      → Builds mesh-agent-linux-{amd64,arm64}
      → Creates GitHub Release with binaries + checksums
  → build-image.yml semver metadata activates (tags server container with version)
  → sync-branches (syncs main back to dev/dependabot-dev)
```

## Race Condition Protection

- `concurrency: group: auto-tag, cancel-in-progress: false` — serializes concurrent runs
- Idempotency: `git rev-parse "$TAG"` check before creating tag
- `fetch-depth: 0` ensures full history for tag enumeration
- `[skip ci]` on changelog commit prevents re-triggering the pipeline

## Verification

### Part 1: Agent binary
1. `cd agent && cargo build --release -p mesh-agent` — compiles without errors
2. `cargo test --workspace` — all existing + new tests pass
3. `./target/release/mesh-agent --help` — shows CLI with version 0.1.0
4. `./target/release/mesh-agent --server-addr 127.0.0.1:9090 --server-ca /tmp/ca.pem` — attempts QUIC connection (fails to connect but doesn't exit(1) immediately)
5. Integration test: mock QUIC server → agent connects, registers, handles SessionRequest

### Part 2: Auto-tag + changelog
1. Push a `feat:` commit to dev → CI passes → merge-to-main → auto-tag creates CHANGELOG entry + `v0.1.0` → release-agent.yml fires → GitHub Release with binaries
2. Push a `fix:` commit → CHANGELOG gets "Fixed" section, tag `v0.1.1`
3. Push a `ci:` commit → auto-tag runs but skips (no release, no changelog update)
4. Push `feat:` + `fix:` together → CHANGELOG has both "Added" and "Fixed", minor bump wins
5. Verify CHANGELOG.md on main has proper Keep a Changelog format
6. Verify `gh release list` shows releases with binary assets

### End-to-end
7. Download agent binary from GitHub Release → run on staging VPS → connects to server via QUIC
