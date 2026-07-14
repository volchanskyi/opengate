//! Compact block codec — the Path-1 synthesis of Netdata dbengine + tsink ideas
//! that are *encoding*, not *substrate*, so they compose on top of any store.
//!
//! Three density levers, borrowed and measured:
//!
//! - **float32 values** (Netdata's custom 32-bit float): host telemetry —
//!   percentages, rates — does not need f64. Halves the value width. This is
//!   **lossy** for fractional gauges (bounded by f32 precision) and the codec
//!   makes that explicit.
//! - **implicit fixed-step timestamps** (Netdata's fixed-step design): a regular
//!   cadence stores only `first_ts` + `step`; per-point timestamps cost nothing.
//!   Out-of-cadence points (NTP steps, gaps) are kept as sparse exceptions, so
//!   the codec is still lossless in time.
//! - **adaptive per-block value codec** (tsink's adaptive selection): a
//!   monotonic integral series (a counter) is encoded **losslessly** with the
//!   shared integer delta-of-delta; a fractional series uses **XOR32 Gorilla**.
//!   The selector keeps whichever is smaller and tags the block.
//!
//! Plus an **inline anomaly bit** per sample (Netdata's storage-number anomaly
//! flag) — WS-14b's "anomaly scores stored inline" requirement — RLE-packed so a
//! sparse anomaly stream is nearly free.

use crate::bitio::{BitReader, BitWriter};
use crate::error::{Result, TsdbError};
use crate::gorilla::{decode_dod, encode_dod};
use crate::sample::Sample;

const CODEC_XOR32: u8 = 0;
const CODEC_FIXED_DOD: u8 = 1;
const CODEC_INT_DOD: u8 = 2;
const INT_SAFE: f64 = 9_007_199_254_740_992.0; // 2^53

/// Encode a block with the adaptive float32 / lossless-integer value path (no
/// fixed-point quantization). Timestamps are implicit against `step` with sparse
/// exceptions, and one anomaly bit per sample. `anomaly` must be the same length
/// as `samples`.
#[must_use]
pub fn encode_compact(samples: &[Sample], anomaly: &[bool], step: i64) -> Vec<u8> {
    encode_compact_scaled(samples, anomaly, step, None)
}

/// Encode a block, optionally quantizing values to fixed-point at `scale`.
///
/// When `scale` is `Some(s)`, a fixed-point candidate is measured: each value is
/// stored as `round(value × s)` delta-of-delta encoded — **lossless to 1/s
/// precision** and typically ~7 % denser than float32-XOR for centi-precision
/// gauges. The adaptive selector keeps the **smallest** of {fixed-point,
/// float32-XOR, lossless int-DoD (integral only)} per block and tags it, so a
/// series that packs better as float32 is never made worse by a fixed-point
/// policy. `scale` must be positive; the chosen scale is stored in the block so
/// decode needs no external policy.
#[must_use]
pub fn encode_compact_scaled(
    samples: &[Sample],
    anomaly: &[bool],
    step: i64,
    scale: Option<i64>,
) -> Vec<u8> {
    let mut out = Vec::new();
    out.extend_from_slice(&(samples.len() as u32).to_le_bytes());
    if samples.is_empty() {
        return out;
    }
    let first_ts = samples[0].ts;
    out.extend_from_slice(&first_ts.to_le_bytes());
    out.extend_from_slice(&step.to_le_bytes());

    let (codec, value_bytes) = select_value_codec(samples, scale);
    out.push(codec);

    // --- timestamp exceptions (points off the fixed cadence) ---
    let mut exc: Vec<(usize, i64)> = Vec::new();
    for (i, s) in samples.iter().enumerate() {
        let expected = first_ts.wrapping_add((i as i64).wrapping_mul(step));
        if s.ts != expected {
            exc.push((i, s.ts.wrapping_sub(expected)));
        }
    }
    put_uvarint(&mut out, exc.len() as u64);
    let mut prev_idx = 0usize;
    for (idx, delta) in exc {
        put_uvarint(&mut out, (idx - prev_idx) as u64);
        put_ivarint(&mut out, delta);
        prev_idx = idx;
    }

    // --- values ---
    put_uvarint(&mut out, value_bytes.len() as u64);
    out.extend_from_slice(&value_bytes);

    // --- anomaly bits, run-length encoded (runs alternate from `false`) ---
    put_anomaly_rle(&mut out, anomaly, samples.len());

    out
}

/// Sample count from a block's header, without decoding the body.
#[must_use]
pub fn block_count(bytes: &[u8]) -> u32 {
    if bytes.len() < 4 {
        return 0;
    }
    u32::from_le_bytes(bytes[0..4].try_into().unwrap())
}

/// Decode a block into its samples (timestamps exact, values f64-widened from
/// their stored form) and the per-sample anomaly bits.
pub fn decode_compact(bytes: &[u8]) -> Result<(Vec<Sample>, Vec<bool>)> {
    let err = || TsdbError::CorruptBlock("compact");
    if bytes.len() < 4 {
        return Err(err());
    }
    let count = u32::from_le_bytes(bytes[0..4].try_into().unwrap()) as usize;
    if count == 0 {
        return Ok((Vec::new(), Vec::new()));
    }
    if bytes.len() < 21 {
        return Err(err());
    }
    let first_ts = i64::from_le_bytes(bytes[4..12].try_into().unwrap());
    let step = i64::from_le_bytes(bytes[12..20].try_into().unwrap());
    let codec = bytes[20];
    let mut pos = 21usize;

    // timestamp exceptions
    let n_exc = get_uvarint(bytes, &mut pos).ok_or_else(err)? as usize;
    let mut exceptions: Vec<(usize, i64)> = Vec::with_capacity(n_exc);
    let mut prev_idx = 0usize;
    for _ in 0..n_exc {
        let idx = prev_idx + get_uvarint(bytes, &mut pos).ok_or_else(err)? as usize;
        let delta = get_ivarint(bytes, &mut pos).ok_or_else(err)?;
        exceptions.push((idx, delta));
        prev_idx = idx;
    }

    // values
    let vlen = get_uvarint(bytes, &mut pos).ok_or_else(err)? as usize;
    let vbytes = bytes.get(pos..pos + vlen).ok_or_else(err)?;
    pos += vlen;
    let values = match codec {
        CODEC_XOR32 => decode_xor32(vbytes, count)?,
        CODEC_FIXED_DOD => decode_fixed_dod(vbytes, count)?,
        CODEC_INT_DOD => decode_int_dod(vbytes, count)?,
        _ => return Err(TsdbError::CorruptBlock("unknown value codec")),
    };

    // anomaly bits
    let anomaly = get_anomaly_rle(bytes, &mut pos, count).ok_or_else(err)?;

    let mut out = Vec::with_capacity(count);
    for (i, value) in values.into_iter().enumerate() {
        let ts = first_ts.wrapping_add((i as i64).wrapping_mul(step));
        out.push(Sample::new(ts, value));
    }
    for (idx, delta) in exceptions {
        if let Some(s) = out.get_mut(idx) {
            s.ts = s.ts.wrapping_add(delta);
        }
    }
    Ok((out, anomaly))
}

/// Pick the densest value codec for a block. Preserves the original behaviour
/// when `scale` is `None` (float32-XOR, or lossless int-DoD for an integral
/// series when it is no larger), and adds fixed-point at `scale` as a candidate
/// that wins only when strictly smaller.
fn select_value_codec(samples: &[Sample], scale: Option<i64>) -> (u8, Vec<u8>) {
    let integral = samples
        .iter()
        .all(|s| s.value.fract() == 0.0 && s.value.abs() < INT_SAFE);
    let xor = encode_xor32(samples);
    let (mut codec, mut best) = if integral {
        let intdod = encode_int_dod(samples);
        if intdod.len() <= xor.len() {
            (CODEC_INT_DOD, intdod)
        } else {
            (CODEC_XOR32, xor)
        }
    } else {
        (CODEC_XOR32, xor)
    };
    if let Some(scale) = scale.filter(|s| *s > 0) {
        let fixed = encode_fixed_dod(samples, scale);
        if fixed.len() < best.len() {
            codec = CODEC_FIXED_DOD;
            best = fixed;
        }
    }
    (codec, best)
}

// --- Fixed-point delta-of-delta (per-metric quantized gauges) ---------------

/// Encode values as `round(value × scale)` integers, delta-of-delta coded. The
/// scale is stored inline so decode is self-describing. Lossless to 1/scale.
fn encode_fixed_dod(samples: &[Sample], scale: i64) -> Vec<u8> {
    let mut out = Vec::with_capacity(8 + samples.len());
    out.extend_from_slice(&scale.to_le_bytes());
    let mut w = BitWriter::new();
    let sf = scale as f64;
    let first = (samples[0].value * sf).round() as i64;
    w.put_bits(first as u64, 64);
    let mut prev_val = first;
    let mut prev_delta = 0i64;
    for s in &samples[1..] {
        let val = (s.value * sf).round() as i64;
        let delta = val.wrapping_sub(prev_val);
        let dod = delta.wrapping_sub(prev_delta);
        encode_dod(&mut w, dod);
        prev_val = val;
        prev_delta = delta;
    }
    out.extend_from_slice(&w.finish());
    out
}

fn decode_fixed_dod(bytes: &[u8], count: usize) -> Result<Vec<f64>> {
    let err = || TsdbError::CorruptBlock("fixed-dod");
    if bytes.len() < 8 {
        return Err(err());
    }
    let scale = i64::from_le_bytes(bytes[0..8].try_into().unwrap());
    if scale == 0 {
        return Err(err());
    }
    let sf = scale as f64;
    let mut r = BitReader::new(&bytes[8..]);
    let first = r.get_bits(64).ok_or_else(err)? as i64;
    let mut prev_val = first;
    let mut prev_delta = 0i64;
    let mut out = Vec::with_capacity(count);
    out.push(first as f64 / sf);
    for _ in 1..count {
        let dod = decode_dod(&mut r)?;
        let delta = prev_delta.wrapping_add(dod);
        prev_val = prev_val.wrapping_add(delta);
        prev_delta = delta;
        out.push(prev_val as f64 / sf);
    }
    Ok(out)
}

// --- XOR32 Gorilla over float32 values -------------------------------------

fn encode_xor32(samples: &[Sample]) -> Vec<u8> {
    let mut w = BitWriter::new();
    let first = (samples[0].value as f32).to_bits();
    w.put_bits(u64::from(first), 32);
    let mut prev = first;
    let mut prev_lead = u32::MAX;
    let mut prev_trail = 0u32;
    for s in &samples[1..] {
        let bits = (s.value as f32).to_bits();
        let xor = bits ^ prev;
        if xor == 0 {
            w.put_bit(0);
        } else {
            w.put_bit(1);
            let lead = xor.leading_zeros().min(31);
            let trail = xor.trailing_zeros();
            if prev_lead != u32::MAX && lead >= prev_lead && trail >= prev_trail {
                w.put_bit(0);
                let sig = 32 - prev_lead - prev_trail;
                w.put_bits(u64::from(xor >> prev_trail), sig);
            } else {
                w.put_bit(1);
                let sig = 32 - lead - trail;
                w.put_bits(u64::from(lead), 5);
                w.put_bits(u64::from(sig - 1), 5);
                w.put_bits(u64::from(xor >> trail), sig);
                prev_lead = lead;
                prev_trail = trail;
            }
        }
        prev = bits;
    }
    w.finish()
}

fn decode_xor32(bytes: &[u8], count: usize) -> Result<Vec<f64>> {
    let err = || TsdbError::CorruptBlock("xor32");
    let mut r = BitReader::new(bytes);
    let first = r.get_bits(32).ok_or_else(err)? as u32;
    let mut prev = first;
    let mut prev_lead = u32::MAX;
    let mut prev_trail = 0u32;
    let mut out = Vec::with_capacity(count);
    out.push(f64::from(f32::from_bits(first)));
    for _ in 1..count {
        let bits = match r.get_bit().ok_or_else(err)? {
            0 => prev,
            _ => {
                let (lead, trail) = match r.get_bit().ok_or_else(err)? {
                    0 => {
                        if prev_lead == u32::MAX {
                            return Err(err());
                        }
                        (prev_lead, prev_trail)
                    }
                    _ => {
                        let lead = r.get_bits(5).ok_or_else(err)? as u32;
                        let sig = r.get_bits(5).ok_or_else(err)? as u32 + 1;
                        prev_lead = lead;
                        prev_trail = 32 - lead - sig;
                        (lead, prev_trail)
                    }
                };
                let sig = 32 - lead - trail;
                let meaningful = r.get_bits(sig).ok_or_else(err)? as u32;
                prev ^ (meaningful << trail)
            }
        };
        prev = bits;
        out.push(f64::from(f32::from_bits(bits)));
    }
    Ok(out)
}

// --- Lossless integer delta-of-delta (counters) ----------------------------

fn encode_int_dod(samples: &[Sample]) -> Vec<u8> {
    let mut w = BitWriter::new();
    let first = samples[0].value as i64;
    w.put_bits(first as u64, 64);
    let mut prev_val = first;
    let mut prev_delta = 0i64;
    for s in &samples[1..] {
        let val = s.value as i64;
        let delta = val.wrapping_sub(prev_val);
        let dod = delta.wrapping_sub(prev_delta);
        encode_dod(&mut w, dod);
        prev_val = val;
        prev_delta = delta;
    }
    w.finish()
}

fn decode_int_dod(bytes: &[u8], count: usize) -> Result<Vec<f64>> {
    let err = || TsdbError::CorruptBlock("int-dod");
    let mut r = BitReader::new(bytes);
    let first = r.get_bits(64).ok_or_else(err)? as i64;
    let mut prev_val = first;
    let mut prev_delta = 0i64;
    let mut out = Vec::with_capacity(count);
    out.push(first as f64);
    for _ in 1..count {
        let dod = decode_dod(&mut r)?;
        let delta = prev_delta.wrapping_add(dod);
        prev_val = prev_val.wrapping_add(delta);
        prev_delta = delta;
        out.push(prev_val as f64);
    }
    Ok(out)
}

// --- Anomaly bit RLE --------------------------------------------------------

fn put_anomaly_rle(out: &mut Vec<u8>, anomaly: &[bool], count: usize) {
    // Runs alternate starting from `false`; a leading `true` yields a 0-length
    // first run. Total run lengths always sum to `count`.
    let mut runs: Vec<u64> = Vec::new();
    let mut current = false;
    let mut len = 0u64;
    for i in 0..count {
        let bit = anomaly.get(i).copied().unwrap_or(false);
        if bit == current {
            len += 1;
        } else {
            runs.push(len);
            current = bit;
            len = 1;
        }
    }
    runs.push(len);
    put_uvarint(out, runs.len() as u64);
    for r in runs {
        put_uvarint(out, r);
    }
}

fn get_anomaly_rle(bytes: &[u8], pos: &mut usize, count: usize) -> Option<Vec<bool>> {
    let n_runs = get_uvarint(bytes, pos)? as usize;
    let mut out = Vec::with_capacity(count);
    let mut current = false;
    for _ in 0..n_runs {
        let len = get_uvarint(bytes, pos)? as usize;
        out.extend(std::iter::repeat_n(current, len));
        current = !current;
    }
    out.truncate(count);
    while out.len() < count {
        out.push(false);
    }
    Some(out)
}

// --- Varints ----------------------------------------------------------------

fn put_uvarint(out: &mut Vec<u8>, mut v: u64) {
    loop {
        let b = (v & 0x7f) as u8;
        v >>= 7;
        if v != 0 {
            out.push(b | 0x80);
        } else {
            out.push(b);
            break;
        }
    }
}

fn get_uvarint(buf: &[u8], pos: &mut usize) -> Option<u64> {
    let mut shift = 0u32;
    let mut res = 0u64;
    loop {
        let b = *buf.get(*pos)?;
        *pos += 1;
        res |= u64::from(b & 0x7f) << shift;
        if b & 0x80 == 0 {
            return Some(res);
        }
        shift += 7;
        if shift >= 64 {
            return None;
        }
    }
}

fn put_ivarint(out: &mut Vec<u8>, v: i64) {
    put_uvarint(out, ((v << 1) ^ (v >> 63)) as u64);
}

fn get_ivarint(buf: &[u8], pos: &mut usize) -> Option<i64> {
    let u = get_uvarint(buf, pos)?;
    Some(((u >> 1) as i64) ^ -((u & 1) as i64))
}

#[cfg(test)]
mod tests {
    use super::{
        block_count, decode_compact, encode_compact, encode_compact_scaled, select_value_codec,
        CODEC_INT_DOD,
    };
    use crate::sample::Sample;

    #[test]
    fn block_count_requires_a_complete_header() {
        assert_eq!(block_count(&[1, 0, 0]), 0);
        assert_eq!(block_count(&[1, 0, 0, 0]), 1);
    }

    #[test]
    fn integral_counter_selects_the_lossless_integer_codec() {
        let counter: Vec<Sample> = (0..240)
            .map(|i| Sample::new(100 + i, (i * 7 + 3) as f64))
            .collect();
        let (codec, _) = select_value_codec(&counter, None);

        assert_eq!(codec, CODEC_INT_DOD);
    }

    #[test]
    fn fixed_point_is_lossless_to_scale() {
        // Centi-precision gauge: fixed-point at ×100 recovers each value exactly
        // to 0.01, which float32 cannot guarantee bit-for-bit.
        let g: Vec<Sample> = (0..400)
            .map(|i| Sample::new(1_000 + i, 37.50 + (i % 13) as f64 * 0.01))
            .collect();
        let bytes = encode_compact_scaled(&g, &vec![false; g.len()], 1, Some(100));
        let (d, _) = decode_compact(&bytes).unwrap();
        for (x, o) in d.iter().zip(&g) {
            let recovered = (x.value * 100.0).round() as i64;
            let expected = (o.value * 100.0).round() as i64;
            assert_eq!(recovered, expected, "fixed-point lost centi precision");
        }
    }

    #[test]
    fn fixed_point_is_selected_and_denser_for_centi_gauges() {
        // A slowly-moving centi gauge should pack smaller as fixed-point int-DoD
        // than as float32-XOR — the measured density lever.
        let g: Vec<Sample> = (0..1_000)
            .map(|i| Sample::new(1_000 + i, 40.0 + ((i / 5) % 20) as f64 * 0.01))
            .collect();
        let anom = vec![false; g.len()];
        let float32 = encode_compact_scaled(&g, &anom, 1, None);
        let fixed = encode_compact_scaled(&g, &anom, 1, Some(100));
        assert!(
            fixed.len() < float32.len(),
            "fixed-point ({}) not denser than float32 ({})",
            fixed.len(),
            float32.len()
        );
    }

    #[test]
    fn scale_never_regresses_below_adaptive() {
        // A series that packs better as float32 must not be made larger by
        // offering a fixed-point scale — the selector keeps the smallest.
        let noisy: Vec<Sample> = (0..500)
            .map(|i| Sample::new(1_000 + i, (i as f64 * 1.2345).sin() * 50.0))
            .collect();
        let anom = vec![false; noisy.len()];
        let adaptive = encode_compact_scaled(&noisy, &anom, 1, None);
        let with_scale = encode_compact_scaled(&noisy, &anom, 1, Some(1000));
        assert!(with_scale.len() <= adaptive.len());
    }

    #[test]
    fn empty_block_round_trips() {
        let (s, a) = decode_compact(&encode_compact(&[], &[], 1)).unwrap();
        assert!(s.is_empty() && a.is_empty());
    }

    #[test]
    fn regular_gauge_is_dense_and_within_f32() {
        let g: Vec<Sample> = (0..240)
            .map(|i| Sample::new(100 + i, 40.0 + (i % 4) as f64 * 0.25))
            .collect();
        let bytes = encode_compact(&g, &vec![false; g.len()], 1);
        // No timestamps stored per point ⇒ well under 2 B/sample.
        assert!((bytes.len() as f64 / g.len() as f64) < 2.0);
        let (d, _) = decode_compact(&bytes).unwrap();
        for (x, o) in d.iter().zip(&g) {
            assert_eq!(x.ts, o.ts);
            assert!((x.value - o.value).abs() < 1e-4);
        }
    }

    #[test]
    fn integral_counter_is_lossless() {
        let c: Vec<Sample> = (0..240)
            .map(|i| Sample::new(100 + i, (i * 7 + 3) as f64))
            .collect();
        let (d, _) = decode_compact(&encode_compact(&c, &vec![false; c.len()], 1)).unwrap();
        for (x, o) in d.iter().zip(&c) {
            assert_eq!(x.value.to_bits(), o.value.to_bits());
        }
    }

    #[test]
    fn anomaly_edge_cases_round_trip() {
        let s: Vec<Sample> = (0..10).map(|i| Sample::new(i, i as f64)).collect();
        for anomaly in [
            vec![false; 10],
            vec![true; 10],
            vec![
                true, false, false, true, true, false, false, false, false, true,
            ],
        ] {
            let (_d, a) = decode_compact(&encode_compact(&s, &anomaly, 1)).unwrap();
            assert_eq!(a, anomaly);
        }
    }

    #[test]
    fn single_sample_round_trips() {
        let s = vec![Sample::new(42, 3.5)];
        let (d, a) = decode_compact(&encode_compact(&s, &[false], 1)).unwrap();
        assert_eq!(d.len(), 1);
        assert_eq!(d[0].ts, 42);
        assert_eq!(a, vec![false]);
    }

    #[test]
    fn truncated_bytes_error_not_panic() {
        let s: Vec<Sample> = (0..50).map(|i| Sample::new(i, i as f64)).collect();
        let bytes = encode_compact(&s, &[false; 50], 1);
        // Chop the tail: decode must return Err, never panic.
        assert!(decode_compact(&bytes[..bytes.len() / 2]).is_err());
    }
}
