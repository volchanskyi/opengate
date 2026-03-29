# Quality Gates: Performance Benchmarks with Regression Detection

## Context

No performance benchmarks exist in the project. We need to track performance of hot paths
(codec, cert signing, handshake, DB operations) across commits to catch regressions early.
`github-action-benchmark` stores results in gh-pages, renders charts, and can fail builds
on threshold violations.

## 1. Go Benchmarks — `server/internal/*/bench_test.go`

Write benchmarks using Go's built-in `testing.B` framework. Focus on hot paths:

### `server/internal/protocol/bench_test.go`
- `BenchmarkCodec_WriteFrame` — frame encoding with realistic payload sizes
- `BenchmarkCodec_ReadFrame` — frame decoding
- `BenchmarkCodec_EncodeControl` — MessagePack encode of ControlMessage
- `BenchmarkCodec_DecodeControl` — MessagePack decode of ControlMessage
- `BenchmarkEncodeServerHello` — binary handshake encoding
- `BenchmarkDecodeServerHello` — binary handshake decoding

### `server/internal/cert/bench_test.go`
- `BenchmarkManager_SignAgent` — ECDSA P-256 cert generation (crypto-heavy)

### `server/internal/db/bench_test.go`
- `BenchmarkStore_UpsertDevice` — INSERT OR CONFLICT
- `BenchmarkStore_GetDevice` — SELECT by ID
- `BenchmarkStore_ListDevices` — SELECT with row scanning loop
- `BenchmarkStore_SetDeviceStatus` — UPDATE with timestamp

### `server/internal/agentapi/bench_test.go`
- `BenchmarkHandshaker_PerformHandshake` — full handshake over net.Pipe

All Go benchmarks use `b.ReportAllocs()` to track memory allocations.

## 2. Rust Benchmarks — `agent/crates/mesh-protocol/benches/codec_bench.rs`

Add `criterion` as a dev dependency and create benchmarks:

### `agent/crates/mesh-protocol/benches/codec_bench.rs`
- `bench_frame_encode_control` — encode a Control frame with AgentRegister payload
- `bench_frame_decode_control` — decode a pre-encoded Control frame
- `bench_frame_encode_ping` — encode Ping (fast path, single byte)
- `bench_encode_server_hello` — binary handshake encoding
- `bench_decode_server_hello` — binary handshake decoding

Add to `agent/crates/mesh-protocol/Cargo.toml`:
```toml
[dev-dependencies]
criterion = { version = "0.5", features = ["html_reports"] }

[[bench]]
name = "codec_bench"
harness = false
```

## 3. CI Jobs — add to `.github/workflows/ci.yml`

### `bench-go` job
- Runs `go test -bench=. -benchmem -count=1 -run=^$ ./internal/...`
- Pipes output to `github-action-benchmark` with `tool: go`
- Stores results in `gh-pages` branch under `dev/bench/go/`
- Alert threshold: 110% (fail if 10% slower than baseline)
- Only runs on `push` to `dev` (not on PRs — benchmarks need consistent hardware)

### `bench-rust` job
- Runs `cargo bench --package mesh-protocol -- --output-format bencher`
- Criterion's bencher output is parsed by `github-action-benchmark` with `tool: cargo`
- Stores results in `gh-pages` branch under `dev/bench/rust/`
- Alert threshold: 110%
- Only runs on `push` to `dev`

### GitHub Pages setup
- Both jobs use `github-action-benchmark` with `auto-push: true`
- Charts viewable at `https://volchanskyi.github.io/opengate/dev/bench/`
- Historical data stored as JSON in the `gh-pages` branch

### Benchmark jobs do NOT block merge-to-main (initially)
- Benchmarks are tracked for trend analysis
- `alert-threshold: '110%'` — fail if 10% slower than baseline
- `fail-on-alert: true` fails the bench job itself if regression exceeds threshold
- Not added to merge-to-main `needs:` initially — add later once baselines stabilize

## TODO: Follow-up after baselines stabilize
- [ ] Add `bench-go` and `bench-rust` to `merge-to-main.needs` list so regressions block the merge

## Critical Files

| File | Action |
|------|--------|
| `server/internal/protocol/bench_test.go` | Create |
| `server/internal/cert/bench_test.go` | Create |
| `server/internal/db/bench_test.go` | Create |
| `server/internal/agentapi/bench_test.go` | Create |
| `agent/crates/mesh-protocol/Cargo.toml` | Add criterion dev-dep + bench target |
| `agent/crates/mesh-protocol/benches/codec_bench.rs` | Create |
| `.github/workflows/ci.yml` | Add bench-go and bench-rust jobs |

## Verification

1. Run Go benchmarks locally: `cd server && go test -bench=. -benchmem -run=^$ ./internal/...`
2. Run Rust benchmarks locally: `cd agent && cargo bench -p mesh-protocol`
3. Push to dev → bench jobs run → results stored in gh-pages
4. Second push → charts show comparison, regression detection active
