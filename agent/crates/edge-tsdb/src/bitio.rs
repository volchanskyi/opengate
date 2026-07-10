//! Minimal MSB-first bit writer/reader for the Gorilla codec.

/// Accumulates bits MSB-first into a byte vector.
#[derive(Default)]
pub struct BitWriter {
    buf: Vec<u8>,
    cur: u8,
    nbits: u8,
}

impl BitWriter {
    /// New empty writer.
    #[must_use]
    pub fn new() -> Self {
        Self::default()
    }

    /// Write a single bit (`value & 1`).
    pub fn put_bit(&mut self, value: u32) {
        self.cur = (self.cur << 1) | ((value & 1) as u8);
        self.nbits += 1;
        if self.nbits == 8 {
            self.buf.push(self.cur);
            self.cur = 0;
            self.nbits = 0;
        }
    }

    /// Write the low `count` bits of `value`, MSB-first. `count` must be 0..=64.
    pub fn put_bits(&mut self, value: u64, count: u32) {
        debug_assert!(count <= 64);
        for i in (0..count).rev() {
            self.put_bit(((value >> i) & 1) as u32);
        }
    }

    /// Flush any partial byte (zero-padded) and return the buffer.
    #[must_use]
    pub fn finish(mut self) -> Vec<u8> {
        if self.nbits > 0 {
            self.cur <<= 8 - self.nbits;
            self.buf.push(self.cur);
        }
        self.buf
    }
}

/// Reads bits MSB-first from a byte slice.
pub struct BitReader<'a> {
    buf: &'a [u8],
    byte: usize,
    bit: u8,
}

impl<'a> BitReader<'a> {
    /// New reader over `buf`.
    #[must_use]
    pub fn new(buf: &'a [u8]) -> Self {
        Self {
            buf,
            byte: 0,
            bit: 0,
        }
    }

    /// Read a single bit; returns `None` past end of stream.
    pub fn get_bit(&mut self) -> Option<u32> {
        if self.byte >= self.buf.len() {
            return None;
        }
        let b = self.buf[self.byte];
        let bit = (b >> (7 - self.bit)) & 1;
        self.bit += 1;
        if self.bit == 8 {
            self.bit = 0;
            self.byte += 1;
        }
        Some(u32::from(bit))
    }

    /// Read `count` bits (0..=64) into the low bits of a `u64`.
    pub fn get_bits(&mut self, count: u32) -> Option<u64> {
        let mut v = 0u64;
        for _ in 0..count {
            v = (v << 1) | u64::from(self.get_bit()?);
        }
        Some(v)
    }
}

#[cfg(test)]
mod tests {
    use super::{BitReader, BitWriter};

    #[test]
    fn round_trips_mixed_widths() {
        let mut w = BitWriter::new();
        w.put_bit(1);
        w.put_bits(0b101, 3);
        w.put_bits(0xDEAD_BEEF, 32);
        w.put_bits(u64::MAX, 64);
        w.put_bit(0);
        let bytes = w.finish();

        let mut r = BitReader::new(&bytes);
        assert_eq!(r.get_bit(), Some(1));
        assert_eq!(r.get_bits(3), Some(0b101));
        assert_eq!(r.get_bits(32), Some(0xDEAD_BEEF));
        assert_eq!(r.get_bits(64), Some(u64::MAX));
        assert_eq!(r.get_bit(), Some(0));
    }

    #[test]
    fn reads_past_end_return_none() {
        let bytes = BitWriter::new().finish();
        let mut r = BitReader::new(&bytes);
        assert_eq!(r.get_bit(), None);
        assert_eq!(r.get_bits(4), None);
    }
}
