# X11 Screen Capture + Chat Echo Implementation

## Context

After fixing display detection (socket probing) and SELinux OTA context, the GUI host now correctly reports `RemoteDesktop` capability and shows Desktop/Chat tabs. However:
- **Desktop shows black screen** — `X11Capture::next_frame()` is a stub returning `Err("not yet implemented")`
- **Chat messages not delivered** — agent receives `ChatMessage`, logs it, but never echoes back

Both are Phase 9 scaffolding stubs. The entire pipeline (relay, codec, browser rendering) is wired and tested — only the agent-side capture and chat echo are missing.

**Reference**: MeshCentral uses tile-based (32x32) JPEG encoding with dirty-region detection, quality adaptation, and WebRTC fallback. We adopt the same JPEG approach but start with full-frame capture (tile-based optimization deferred).

---

## Phase 1: Chat Echo (small, high-impact, proves relay pipeline)

**Goal**: Browser sends ChatMessage → agent receives → agent echoes back with `sender: "agent"` → browser displays it.

### Files to modify

| File | Change |
|------|--------|
| [handler.rs](agent/crates/mesh-agent-core/src/session/handler.rs) L93-95 | Replace log-only with echo via `send_frame` |

### Implementation

In `handle_control`, replace:
```rust
ControlMessage::ChatMessage { text, sender } => {
    info!(sender, text, "chat message received");
}
```
With:
```rust
ControlMessage::ChatMessage { text, sender } => {
    info!(sender, text, "chat message received");
    let echo = ControlMessage::ChatMessage {
        text,
        sender: "agent".to_string(),
    };
    if let Err(e) = send_frame(frame_tx, &Frame::Control(echo)).await {
        warn!("failed to echo chat message: {e}");
    }
}
```

This follows the exact pattern of `handle_file_list` (L151-177) which sends `Frame::Control(response)` via the same `frame_tx` channel.

### Tests (TDD)

Add to `handler.rs` tests:

1. **`test_handle_control_chat_echoes_back`** — send ChatMessage, verify frame_rx receives a ChatMessage with `sender: "agent"` and same text
2. **`test_handle_control_chat_preserves_text`** — verify text content is preserved exactly (including unicode, empty string)

### Why this works end-to-end

Browser sends → relay pipes → agent handler receives → `send_frame(frame_tx, echo)` → `ws_writer_loop` sends binary → relay pipes back → browser `ws-transport.ts` decodes → routes to `onControlMessage` → `MessengerView` calls `addMessage({text, sender: "agent"})` → renders in gray bubble (L63).

No server, protocol, or web changes needed. The browser already handles incoming ChatMessage frames.

---

## Phase 2: X11 Frame Capture with JPEG Encoding

**Goal**: Capture X11 root window, encode as JPEG, stream at ~10 FPS to browser.

### 2A: Add workspace dependencies

| File | Change |
|------|--------|
| [agent/Cargo.toml](agent/Cargo.toml) (workspace) | Add `image = "0.25"` to `[workspace.dependencies]` |
| [platform-linux/Cargo.toml](agent/crates/platform-linux/Cargo.toml) | Add `image = { workspace = true, optional = true }` under `[dependencies]`, add to `x11` feature: `x11 = ["dep:x11rb", "dep:image"]` |
| [mesh-agent/Cargo.toml](agent/crates/mesh-agent/Cargo.toml) | Change L13 to `platform-linux = { path = "../platform-linux", features = ["x11"] }` |

**Why `image` crate**: Well-maintained (100M+ downloads), supports JPEG encoding via `image::codecs::jpeg::JpegEncoder`, already handles RGB/RGBA buffers. No need for turbojpeg/mozjpeg bindings. MeshCentral uses JPEG too — it's the proven approach for remote desktop.

### 2B: Add `Jpeg` to `FrameEncoding` enum

| File | Change |
|------|--------|
| [frame.rs](agent/crates/mesh-protocol/src/types/frame.rs) L8-14 | Add `Jpeg` variant to enum (it's `#[non_exhaustive]`, safe to extend) |
| [types.ts](web/src/lib/protocol/types.ts) L22 | Add `'Jpeg'` to FrameEncoding union |

### 2C: Implement X11Capture::next_frame()

| File | Change |
|------|--------|
| [x11_capture.rs](agent/crates/platform-linux/src/capture/x11_capture.rs) | Full rewrite of struct + next_frame |

**Design** (modeled after MeshCentral's approach):

```rust
pub struct X11Capture {
    conn: x11rb::rust_connection::RustConnection,
    root: u32,       // root window Drawable
    width: u32,
    height: u32,
}

impl X11Capture {
    pub fn new() -> Result<Self, CaptureError> {
        let display = std::env::var("DISPLAY").map_err(|_| CaptureError::NoDisplay)?;
        let (conn, screen_num) = x11rb::connect(Some(&display))
            .map_err(|e| CaptureError::Backend(e.to_string()))?;
        let screen = &conn.setup().roots[screen_num];
        let root = screen.root;
        let width = screen.width_in_pixels as u32;
        let height = screen.height_in_pixels as u32;
        Ok(Self { conn, root, width, height })
    }
}
```

**`next_frame()` implementation**:

```rust
async fn next_frame(&mut self) -> Result<RawFrame, CaptureError> {
    // XGetImage in ZPixmap format — returns BGRX on little-endian x86
    use x11rb::protocol::xproto::{ConnectionExt, ImageFormat};

    let reply = self.conn
        .get_image(
            ImageFormat::Z_PIXMAP,
            self.root,
            0, 0,
            self.width as u16,
            self.height as u16,
            !0, // all planes
        )
        .map_err(|e| CaptureError::Backend(e.to_string()))?
        .reply()
        .map_err(|e| CaptureError::Backend(e.to_string()))?;

    // X11 ZPixmap on 24/32-bit depth = BGRX (4 bytes/pixel, little-endian)
    // Convert BGRX → RGBA in-place for browser compatibility
    let mut data = reply.data;
    for chunk in data.chunks_exact_mut(4) {
        let (b, g, r) = (chunk[0], chunk[1], chunk[2]);
        chunk[0] = r;
        chunk[1] = g;
        chunk[2] = b;
        chunk[3] = 255; // alpha
    }

    Ok(RawFrame {
        width: self.width,
        height: self.height,
        data,
    })
}
```

**Key decisions**:
- Use `get_image` (not MIT-SHM) for Phase 1 — simpler, correct, works everywhere. SHM optimization can come later.
- Convert BGRX→RGBA on the agent side so the browser can render directly via `putImageData`. This matches what the browser expects (comment in `desktop-worker.ts` says "RGBA").
- Update `RawFrame` doc comment from "BGRA" to "RGBA" to match reality.

### 2D: JPEG encoding in the capture loop

| File | Change |
|------|--------|
| [relay.rs](agent/crates/mesh-agent-core/src/session/relay.rs) L51-92 | Encode RawFrame as JPEG before wrapping in DesktopFrame |

**Changes to `capture_loop`**:

```rust
// After getting raw_frame from capture.next_frame():
let jpeg_data = encode_jpeg(&raw_frame, 70)?; // Q70 like MeshCentral default

let desktop_frame = DesktopFrame {
    sequence: seq,
    x: 0, y: 0,
    width: raw_frame.width as u16,
    height: raw_frame.height as u16,
    encoding: FrameEncoding::Jpeg,
    data: jpeg_data,
};
```

Add helper in `relay.rs`:
```rust
fn encode_jpeg(frame: &RawFrame, quality: u8) -> Result<Vec<u8>, SessionError> {
    use image::codecs::jpeg::JpegEncoder;
    let mut buf = Vec::new();
    let mut encoder = JpegEncoder::new_with_quality(&mut buf, quality);
    encoder.encode(
        &frame.data,
        frame.width,
        frame.height,
        image::ExtendedColorType::Rgba8,
    ).map_err(|e| SessionError::Capture(e.to_string()))?;
    Ok(buf)
}
```

**Frame rate**: Reduce from 33ms (30 FPS) to 100ms (~10 FPS) initially. XGetImage is a blocking full-frame copy — 30 FPS would saturate the X server. MeshCentral also targets lower FPS with dirty-region optimization. We can increase FPS later with SHM + dirty detection.

**Frame size**: 1920x1080 JPEG at Q70 ≈ 50-150 KB vs 7.9 MB raw. Well within 16 MiB MAX_FRAME_SIZE. Even 4K JPEG is ~200-500 KB.

### 2E: Browser JPEG decoding

| File | Change |
|------|--------|
| [desktop-worker.ts](web/src/features/remote-desktop/desktop-worker.ts) | Add Jpeg branch that decodes via Blob + createImageBitmap |

```typescript
export async function paintFrame(ctx: CanvasContext, frame: DesktopFrame): Promise<void> {
  if (frame.encoding === 'Raw') {
    const imageData = ctx.createImageData(frame.width, frame.height);
    imageData.data.set(frame.data.subarray(0, frame.width * frame.height * 4));
    ctx.putImageData(imageData, frame.x, frame.y);
  } else if (frame.encoding === 'Jpeg') {
    const blob = new Blob([frame.data], { type: 'image/jpeg' });
    const bitmap = await createImageBitmap(blob);
    ctx.drawImage(bitmap, frame.x, frame.y);
    bitmap.close();
  }
}
```

**Note**: `paintFrame` signature changes from sync to async. Update call site in `use-remote-desktop.ts` accordingly (the callback can be async — it's fire-and-forget rendering).

Also update:
- [use-remote-desktop.ts](web/src/features/remote-desktop/use-remote-desktop.ts) — make frame callback async-safe
- [desktop-worker.ts](web/src/features/remote-desktop/desktop-worker.ts) — update `CanvasContext` interface to include `drawImage`

### 2F: Fix RawFrame doc + platform.rs

| File | Change |
|------|--------|
| [platform.rs](agent/crates/mesh-agent-core/src/platform.rs) L10-18 | Update doc comment from "BGRA" to "RGBA" since we now convert on capture |

---

## Phase 3: Tests (TDD throughout, listed here for overview)

### Rust tests

| Test | File | What it validates |
|------|------|-------------------|
| `test_handle_control_chat_echoes_back` | handler.rs | ChatMessage echo via frame_tx |
| `test_handle_control_chat_preserves_text` | handler.rs | Unicode/empty text preserved |
| `test_x11_capture_stores_connection` | x11_capture.rs | Connection persists in struct |
| `test_encode_jpeg_valid_rgba` | relay.rs | JPEG encoding produces valid data |
| `test_encode_jpeg_small_output` | relay.rs | JPEG output << raw size |
| `test_frame_encoding_jpeg_roundtrip` | codec_test.rs | Jpeg variant serializes/deserializes |

### Web tests

| Test | File | What it validates |
|------|------|-------------------|
| `test_paint_frame_jpeg` | desktop-worker.test.ts | Jpeg frames decoded correctly |
| `test_frame_encoding_jpeg_type` | codec.test.ts | Jpeg encoding type in protocol |

### Golden file test

| Test | What it validates |
|------|-------------------|
| Add Jpeg FrameEncoding to golden test | Rust ↔ Go ↔ TS compatibility for new variant |

---

## File Change Summary

| File | Phase | Type |
|------|-------|------|
| `agent/crates/mesh-agent-core/src/session/handler.rs` | 1 | Chat echo |
| `agent/Cargo.toml` | 2A | Add `image` dep |
| `agent/crates/platform-linux/Cargo.toml` | 2A | Enable `image` in x11 feature |
| `agent/crates/mesh-agent/Cargo.toml` | 2A | Enable `x11` feature |
| `agent/crates/mesh-protocol/src/types/frame.rs` | 2B | Add `Jpeg` variant |
| `agent/crates/platform-linux/src/capture/x11_capture.rs` | 2C | Implement frame capture |
| `agent/crates/mesh-agent-core/src/platform.rs` | 2F | Fix BGRA→RGBA doc |
| `agent/crates/mesh-agent-core/src/session/relay.rs` | 2D | JPEG encode + FPS |
| `web/src/lib/protocol/types.ts` | 2B | Add 'Jpeg' type |
| `web/src/features/remote-desktop/desktop-worker.ts` | 2E | JPEG decode |
| `web/src/features/remote-desktop/use-remote-desktop.ts` | 2E | Async frame callback |
| Various test files | 3 | Tests |

---

## Verification

1. **Chat**: Start session → Chat tab → type message → see it echo back in gray bubble
2. **Desktop**: Start session from GUI host → Desktop tab → see live screen at ~10 FPS
3. **Headless**: Start session from WSL2 host → no Desktop/Chat tabs (RemoteDesktop not in capabilities)
4. **Frame size**: Check agent logs — JPEG frames should be 50-200 KB, not 7+ MB
5. `make test` + `make e2e` — all pass
6. `make golden` — cross-language compat with new Jpeg variant

## Future Optimization (not in scope)

- **Tile-based dirty detection** (like MeshCentral's 32x32 tiles) — only encode/send changed regions
- **MIT-SHM** — zero-copy frame capture via shared memory
- **WebP encoding** — ~50% smaller than JPEG per MeshCentral data
- **Adaptive quality** — lower JPEG quality on slow connections
- **Adaptive FPS** — increase when idle, decrease under load
- **Input injection** — X11/evdev mouse/keyboard injection (NullInput → real input)
