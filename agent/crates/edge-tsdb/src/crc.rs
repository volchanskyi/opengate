//! Inline CRC-32 (IEEE 802.3, reflected polynomial) that detects torn or flipped chunks.

const POLY: u32 = 0xEDB8_8320;

/// The reflected CRC-32 lookup table, built once at first use.
struct Table([u32; 256]);

impl Table {
    const fn build() -> Self {
        let mut table = [0u32; 256];
        let mut i = 0;
        while i < 256 {
            let mut crc = i as u32;
            let mut j = 0;
            while j < 8 {
                if crc & 1 == 1 {
                    crc = (crc >> 1) ^ POLY;
                } else {
                    crc >>= 1;
                }
                j += 1;
            }
            table[i] = crc;
            i += 1;
        }
        Self(table)
    }
}

static TABLE: Table = Table::build();

/// CRC-32/IEEE of `data`.
#[must_use]
pub fn crc32(data: &[u8]) -> u32 {
    let mut crc = 0xFFFF_FFFFu32;
    for &b in data {
        let idx = ((crc ^ u32::from(b)) & 0xFF) as usize;
        crc = (crc >> 8) ^ TABLE.0[idx];
    }
    crc ^ 0xFFFF_FFFF
}

#[cfg(test)]
mod tests {
    use super::crc32;

    #[test]
    fn known_vectors() {
        assert_eq!(crc32(b""), 0x0000_0000);
        assert_eq!(crc32(b"123456789"), 0xCBF4_3926);
        assert_eq!(
            crc32(b"The quick brown fox jumps over the lazy dog"),
            0x414F_A339
        );
    }

    #[test]
    fn single_bit_flip_changes_crc() {
        let a = crc32(b"edge-sentinel");
        let b = crc32(b"edge-sentinem");
        assert_ne!(a, b);
    }
}
