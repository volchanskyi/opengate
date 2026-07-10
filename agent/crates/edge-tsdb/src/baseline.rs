//! Substrate C — the no-persist control.
//!
//! A bounded in-RAM ring per series. It exists to quantify exactly what the
//! offline promise costs and buys: it has zero write-amplification and the
//! lowest possible latency, but loses all history on restart, so it can never
//! back true min/max or deep backfill. Every reopen is empty by construction.

use std::collections::BTreeMap;
use std::collections::VecDeque;
use std::path::Path;

use crate::error::Result;
use crate::sample::{Sample, SeriesId};
use crate::substrate::{Durability, Substrate};

/// Per-series retained-sample bound for the RAM control.
const RING_CAP: usize = 8_192;

/// Substrate C.
#[derive(Default)]
pub struct BaselineStore {
    series: BTreeMap<SeriesId, VecDeque<Sample>>,
}

impl Substrate for BaselineStore {
    fn open(_path: &Path) -> Result<Self> {
        // No persistence: a fresh instance is always empty.
        Ok(Self::default())
    }

    fn append(&mut self, series: SeriesId, sample: Sample) -> Result<()> {
        let ring = self.series.entry(series).or_default();
        ring.push_back(sample);
        if ring.len() > RING_CAP {
            ring.pop_front();
        }
        Ok(())
    }

    fn commit(&mut self, _durability: Durability) -> Result<()> {
        Ok(())
    }

    fn range(&self, series: SeriesId, start: i64, end: i64) -> Result<Vec<Sample>> {
        let mut out: Vec<Sample> = self
            .series
            .get(&series)
            .into_iter()
            .flatten()
            .filter(|s| s.ts >= start && s.ts < end)
            .copied()
            .collect();
        out.sort_by_key(|s| s.ts);
        Ok(out)
    }

    fn size_on_disk(&self) -> Result<u64> {
        Ok(0)
    }

    fn total_samples(&self) -> Result<usize> {
        Ok(self.series.values().map(VecDeque::len).sum())
    }
}

#[cfg(test)]
mod tests {
    use super::{BaselineStore, RING_CAP};
    use crate::sample::Sample;
    use crate::substrate::Substrate;
    use std::path::Path;

    #[test]
    fn retains_recent_and_bounds_memory() {
        let mut s = BaselineStore::open(Path::new(".")).unwrap();
        for i in 0..(RING_CAP as i64 + 100) {
            s.append(9, Sample::new(i, i as f64)).unwrap();
        }
        assert_eq!(s.total_samples().unwrap(), RING_CAP);
        assert_eq!(s.size_on_disk().unwrap(), 0);
        // Oldest 100 evicted.
        assert!(s.range(9, 0, 100).unwrap().is_empty());
    }
}
