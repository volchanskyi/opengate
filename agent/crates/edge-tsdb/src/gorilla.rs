//! Gorilla-style compression: delta-of-delta timestamps + XOR float values.
//!
//! This is the *shared* compression layer of the bake-off — substrate A
//! (append-only) and substrate B (redb) both persist blocks produced here, so
//! bytes/sample differences between them are storage overhead, not codec
//! differences. Regular 1 Hz data collapses to ~1 bit for the timestamp (DoD 0)
//! and ~1 bit for an unchanged value, which is what puts the append-only
//! substrate in the sub-1 B/sample class.

use crate::bitio::{BitReader, BitWriter};
use crate::error::{Result, TsdbError};
use crate::sample::Sample;

/// Encode a block of samples into a self-delimiting compressed payload.
///
/// The first 32 bits are the sample count so the decoder is framing-independent.
#[must_use]
pub fn encode_block(samples: &[Sample]) -> Vec<u8> {
    let mut w = BitWriter::new();
    w.put_bits(samples.len() as u64, 32);
    let Some(first) = samples.first() else {
        return w.finish();
    };

    w.put_bits(first.ts as u64, 64);
    w.put_bits(first.value.to_bits(), 64);

    let mut prev_ts = first.ts;
    let mut prev_delta: i64 = 0;
    let mut prev_bits = first.value.to_bits();
    // Sentinel: no previous meaningful window yet.
    let mut prev_lead = u32::MAX;
    let mut prev_trail = 0u32;

    for s in &samples[1..] {
        // --- timestamp: delta-of-delta ---
        let delta = s.ts.wrapping_sub(prev_ts);
        let dod = delta.wrapping_sub(prev_delta);
        encode_dod(&mut w, dod);
        prev_ts = s.ts;
        prev_delta = delta;

        // --- value: XOR with previous ---
        let bits = s.value.to_bits();
        let xor = bits ^ prev_bits;
        if xor == 0 {
            w.put_bit(0);
        } else {
            w.put_bit(1);
            let lead = xor.leading_zeros().min(31);
            let trail = xor.trailing_zeros();
            if prev_lead != u32::MAX && lead >= prev_lead && trail >= prev_trail {
                // Reuse the previous window.
                w.put_bit(0);
                let sig = 64 - prev_lead - prev_trail;
                w.put_bits(xor >> prev_trail, sig);
            } else {
                w.put_bit(1);
                let sig = 64 - lead - trail;
                w.put_bits(u64::from(lead), 5);
                w.put_bits(u64::from(sig - 1), 6);
                w.put_bits(xor >> trail, sig);
                prev_lead = lead;
                prev_trail = trail;
            }
        }
        prev_bits = bits;
    }

    w.finish()
}

/// Number of samples a block encodes, read from its 32-bit count header without
/// decoding the body. Used by recovery/stats to count fast.
#[must_use]
pub fn block_count(payload: &[u8]) -> u32 {
    let mut r = BitReader::new(payload);
    r.get_bits(32).unwrap_or(0) as u32
}

/// Decode a block previously produced by [`encode_block`].
pub fn decode_block(payload: &[u8]) -> Result<Vec<Sample>> {
    let mut r = BitReader::new(payload);
    let count = r
        .get_bits(32)
        .ok_or(TsdbError::CorruptBlock("missing count"))? as usize;
    if count == 0 {
        return Ok(Vec::new());
    }

    let mut out = Vec::with_capacity(count);
    let first_ts = r.get_bits(64).ok_or(TsdbError::CorruptBlock("first ts"))? as i64;
    let first_bits = r.get_bits(64).ok_or(TsdbError::CorruptBlock("first val"))?;
    out.push(Sample::new(first_ts, f64::from_bits(first_bits)));

    let mut prev_ts = first_ts;
    let mut prev_delta: i64 = 0;
    let mut prev_bits = first_bits;
    let mut prev_lead = u32::MAX;
    let mut prev_trail = 0u32;

    for _ in 1..count {
        let dod = decode_dod(&mut r)?;
        let delta = prev_delta.wrapping_add(dod);
        let ts = prev_ts.wrapping_add(delta);
        prev_ts = ts;
        prev_delta = delta;

        let bits = match r.get_bit().ok_or(TsdbError::CorruptBlock("value ctrl"))? {
            0 => prev_bits,
            _ => {
                let (lead, trail) =
                    match r.get_bit().ok_or(TsdbError::CorruptBlock("window ctrl"))? {
                        0 => {
                            if prev_lead == u32::MAX {
                                return Err(TsdbError::CorruptBlock("window reuse before set"));
                            }
                            (prev_lead, prev_trail)
                        }
                        _ => {
                            let lead = r.get_bits(5).ok_or(TsdbError::CorruptBlock("lead"))? as u32;
                            let sig =
                                r.get_bits(6).ok_or(TsdbError::CorruptBlock("sig"))? as u32 + 1;
                            prev_lead = lead;
                            prev_trail = 64 - lead - sig;
                            (lead, prev_trail)
                        }
                    };
                let sig = 64 - lead - trail;
                let meaningful = r.get_bits(sig).ok_or(TsdbError::CorruptBlock("mantissa"))?;
                prev_bits ^ (meaningful << trail)
            }
        };
        prev_bits = bits;
        out.push(Sample::new(prev_ts, f64::from_bits(bits)));
    }

    Ok(out)
}

/// Write a delta-of-delta with a variable-width prefix code. The final 64-bit
/// escape keeps arbitrary NTP-step timestamp jumps lossless.
fn encode_dod(w: &mut BitWriter, dod: i64) {
    if dod == 0 {
        w.put_bit(0);
    } else if fits(dod, 7) {
        w.put_bits(0b10, 2);
        w.put_bits(dod as u64, 7);
    } else if fits(dod, 9) {
        w.put_bits(0b110, 3);
        w.put_bits(dod as u64, 9);
    } else if fits(dod, 12) {
        w.put_bits(0b1110, 4);
        w.put_bits(dod as u64, 12);
    } else if fits(dod, 32) {
        w.put_bits(0b11110, 5);
        w.put_bits(dod as u64, 32);
    } else {
        w.put_bits(0b11111, 5);
        w.put_bits(dod as u64, 64);
    }
}

fn decode_dod(r: &mut BitReader) -> Result<i64> {
    let err = || TsdbError::CorruptBlock("dod");
    if r.get_bit().ok_or_else(err)? == 0 {
        return Ok(0);
    }
    if r.get_bit().ok_or_else(err)? == 0 {
        return Ok(sign_extend(r.get_bits(7).ok_or_else(err)?, 7));
    }
    if r.get_bit().ok_or_else(err)? == 0 {
        return Ok(sign_extend(r.get_bits(9).ok_or_else(err)?, 9));
    }
    if r.get_bit().ok_or_else(err)? == 0 {
        return Ok(sign_extend(r.get_bits(12).ok_or_else(err)?, 12));
    }
    if r.get_bit().ok_or_else(err)? == 0 {
        return Ok(sign_extend(r.get_bits(32).ok_or_else(err)?, 32));
    }
    Ok(r.get_bits(64).ok_or_else(err)? as i64)
}

/// Does `v` fit in an `n`-bit two's-complement field?
fn fits(v: i64, n: u32) -> bool {
    let min = -(1i64 << (n - 1));
    let max = (1i64 << (n - 1)) - 1;
    v >= min && v <= max
}

/// Sign-extend the low `n` bits of `raw`.
fn sign_extend(raw: u64, n: u32) -> i64 {
    let shift = 64 - n;
    ((raw << shift) as i64) >> shift
}

#[cfg(test)]
mod tests {
    use super::{block_count, decode_block, encode_block};
    use crate::sample::Sample;
    use proptest::prelude::*;

    #[test]
    fn empty_block_round_trips() {
        let bytes = encode_block(&[]);
        assert_eq!(block_count(&bytes), 0);
        assert_eq!(decode_block(&bytes).unwrap(), Vec::<Sample>::new());
    }

    #[test]
    fn regular_1hz_is_dense() {
        // 120 samples of a slowly-changing value at a fixed 1 s cadence.
        let samples: Vec<Sample> = (0..120)
            .map(|i| Sample::new(1_000_000 + i, 42.0 + (i % 3) as f64))
            .collect();
        let bytes = encode_block(&samples);
        assert_eq!(decode_block(&bytes).unwrap(), samples);
        // Header (12 B) dominates; steady state is well under 1 B/sample.
        let per_sample = (bytes.len() - 12) as f64 / samples.len() as f64;
        assert!(per_sample < 1.0, "steady-state {per_sample:.3} B/sample");
    }

    #[test]
    fn backward_and_forward_time_steps_round_trip() {
        // NTP corrections: timestamps step back then jump far forward.
        let samples = vec![
            Sample::new(1000, 1.0),
            Sample::new(1001, 1.5),
            Sample::new(995, 2.0), // step back 6 s
            Sample::new(996, 2.0),
            Sample::new(500_000, 9.0), // large forward jump
            Sample::new(500_001, 9.0),
        ];
        let bytes = encode_block(&samples);
        assert_eq!(decode_block(&bytes).unwrap(), samples);
    }

    proptest! {
        #[test]
        fn arbitrary_series_round_trip(
            raw in prop::collection::vec((any::<i32>(), any::<f64>()), 1..300)
        ) {
            let samples: Vec<Sample> = raw
                .into_iter()
                .map(|(t, v)| Sample::new(i64::from(t), v))
                .collect();
            let bytes = encode_block(&samples);
            let decoded = decode_block(&bytes).unwrap();
            prop_assert_eq!(decoded.len(), samples.len());
            for (a, b) in decoded.iter().zip(&samples) {
                prop_assert_eq!(a.ts, b.ts);
                // NaN payloads compare by bit pattern (XOR codec is bit-exact).
                prop_assert_eq!(a.value.to_bits(), b.value.to_bits());
            }
        }
    }
}
