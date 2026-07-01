use std::collections::VecDeque;

use thiserror::Error;

/// Errors returned by anomaly window construction.
#[derive(Debug, Error, PartialEq, Eq)]
#[non_exhaustive]
pub enum WindowError {
    /// A rolling window needs at least one slot.
    #[error("capacity must be greater than zero")]
    EmptyCapacity,
}

/// Fixed-capacity rolling window of bit-packed anomaly flags.
#[derive(Debug, Clone)]
pub struct AnomalyRateWindow {
    capacity: usize,
    entries: VecDeque<(i64, u64)>,
}

impl AnomalyRateWindow {
    /// Create a rolling window with a hard entry cap.
    pub fn new(capacity: usize) -> Result<Self, WindowError> {
        if capacity == 0 {
            return Err(WindowError::EmptyCapacity);
        }
        Ok(Self {
            capacity,
            entries: VecDeque::with_capacity(capacity),
        })
    }

    /// Push one timestamped bitset, evicting the oldest entry at capacity.
    pub fn push(&mut self, timestamp: i64, bits: u64) {
        if self.entries.len() == self.capacity {
            self.entries.pop_front();
        }
        self.entries.push_back((timestamp, bits));
    }

    /// Return the fraction of entries where `bit_index` is set.
    pub fn rate(&self, bit_index: u8) -> f32 {
        if self.entries.is_empty() || bit_index >= u64::BITS as u8 {
            return 0.0;
        }
        let mask = 1u64 << bit_index;
        let hits = self
            .entries
            .iter()
            .filter(|(_, bits)| bits & mask != 0)
            .count();
        hits as f32 / self.entries.len() as f32
    }

    /// Return the number of retained entries.
    pub fn len(&self) -> usize {
        self.entries.len()
    }

    /// Return whether the window is empty.
    pub fn is_empty(&self) -> bool {
        self.entries.is_empty()
    }
}
