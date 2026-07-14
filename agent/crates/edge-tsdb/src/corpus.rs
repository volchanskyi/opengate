//! Deterministic fixture corpus of realistic host telemetry.
//!
//! One seed reproduces the exact same multi-series stream on every machine, so
//! the bake-off measures substrates against identical input in CI. Series model
//! the WS-2 metric families: bounded percentages that drift and occasionally
//! spike, and monotonically-rising counters. Samples are emitted interleaved by
//! second (all series at t, then t+1), the order a real 1 Hz sampler produces.

use crate::sample::{Sample, SeriesId};
#[cfg(feature = "bakeoff")]
use crate::{error::Result, substrate::Substrate};

/// Corpus generation parameters.
#[derive(Debug, Clone, Copy)]
pub struct CorpusConfig {
    /// PRNG seed — fixes the entire stream.
    pub seed: u64,
    /// Number of independent series.
    pub series: u32,
    /// Duration in seconds; one sample per series per second.
    pub duration_secs: i64,
    /// Timestamp of the first sample.
    pub start_ts: i64,
}

impl Default for CorpusConfig {
    fn default() -> Self {
        Self {
            seed: 0x5EED_1234_ABCD_0001,
            series: 16,
            duration_secs: 3_600,
            start_ts: 1_700_000_000,
        }
    }
}

/// A generated, replayable corpus with its expected per-series read-back.
pub struct Corpus {
    // Read by the bake-off `assert_readback` range bounds; the production tests
    // drive the store from `series()` directly.
    #[cfg_attr(not(feature = "bakeoff"), allow(dead_code))]
    config: CorpusConfig,
    events: Vec<(SeriesId, Sample)>,
    expected: Vec<Vec<Sample>>,
}

/// SplitMix64 — a tiny, well-distributed, dependency-free PRNG.
struct SplitMix64(u64);

impl SplitMix64 {
    fn next_u64(&mut self) -> u64 {
        self.0 = self.0.wrapping_add(0x9E37_79B9_7F4A_7C15);
        let mut z = self.0;
        z = (z ^ (z >> 30)).wrapping_mul(0xBF58_476D_1CE4_E5B9);
        z = (z ^ (z >> 27)).wrapping_mul(0x94D0_49BB_1331_11EB);
        z ^ (z >> 31)
    }

    /// Uniform in `[0, 1)`.
    fn unit(&mut self) -> f64 {
        (self.next_u64() >> 11) as f64 / (1u64 << 53) as f64
    }
}

impl Corpus {
    /// Generate a corpus from `config`.
    #[must_use]
    pub fn generate(config: CorpusConfig) -> Self {
        let mut rng = SplitMix64(config.seed);
        let series = config.series as usize;

        // Per-series state. Real host telemetry at 1 Hz is *sticky*: idle CPU,
        // flat disk %, and steady memory hold the same value for many samples,
        // which is exactly what the XOR codec collapses to one bit. `volatility`
        // is the per-sample probability the gauge actually moves; counters rise
        // monotonically (byte/packet counters), which XOR compresses poorly — a
        // finding the spike reports rather than hides.
        let mut value = vec![0.0f64; series];
        let mut is_counter = vec![false; series];
        let mut drift = vec![0.0f64; series];
        let mut volatility = vec![0.0f64; series];
        for i in 0..series {
            is_counter[i] = i % 4 == 3; // one in four series is a rising counter
            value[i] = if is_counter[i] {
                0.0
            } else {
                rng.unit() * 80.0
            };
            drift[i] = 0.2 + rng.unit() * 2.0;
            // A spread of stability: some series barely move, some churn.
            volatility[i] = 0.05 + rng.unit() * 0.5;
        }

        let mut events = Vec::with_capacity(series * config.duration_secs as usize);
        let mut expected = vec![Vec::with_capacity(config.duration_secs as usize); series];

        for step in 0..config.duration_secs {
            let ts = config.start_ts + step;
            for s in 0..series {
                if is_counter[s] {
                    value[s] += (rng.unit() * 1_000.0).round(); // bytes/sec-ish
                } else if rng.unit() < volatility[s] {
                    // Move: small random walk within [0, 100], rare spikes.
                    let delta = (rng.unit() - 0.5) * 2.0 * drift[s];
                    value[s] = round_centi((value[s] + delta).clamp(0.0, 100.0));
                    if rng.unit() < 0.01 {
                        value[s] = round_centi((value[s] + rng.unit() * 40.0).min(100.0));
                    }
                }
                // else: hold the previous value (idle plateau) — XOR == 0.
                let sample = Sample::new(ts, value[s]);
                events.push((s as SeriesId, sample));
                expected[s].push(sample);
            }
        }

        Self {
            config,
            events,
            expected,
        }
    }

    /// Total samples across all series.
    #[must_use]
    pub fn sample_count(&self) -> usize {
        self.events.len()
    }

    /// The per-series expected sample streams (index = `SeriesId`). Used by the
    /// codec bake-off to encode identical input through each codec.
    #[must_use]
    pub fn series(&self) -> &[Vec<Sample>] {
        &self.expected
    }

    /// Replay every event into `store` (caller commits).
    #[cfg(feature = "bakeoff")]
    pub fn replay_into<S: Substrate>(&self, store: &mut S) -> Result<()> {
        for (series, sample) in &self.events {
            store.append(*series, *sample)?;
        }
        Ok(())
    }

    /// Replay the first `n` events; returns the number actually appended.
    #[cfg(feature = "bakeoff")]
    pub fn replay_prefix_into<S: Substrate>(&self, store: &mut S, n: usize) -> Result<usize> {
        let end = n.min(self.events.len());
        for (series, sample) in &self.events[..end] {
            store.append(*series, *sample)?;
        }
        Ok(end)
    }

    /// Replay events from index `n` to the end.
    #[cfg(feature = "bakeoff")]
    pub fn replay_suffix_into<S: Substrate>(&self, store: &mut S, n: usize) -> Result<()> {
        let start = n.min(self.events.len());
        for (series, sample) in &self.events[start..] {
            store.append(*series, *sample)?;
        }
        Ok(())
    }

    /// Assert every series reads back exactly what was written, in order.
    #[cfg(feature = "bakeoff")]
    pub fn assert_readback<S: Substrate>(&self, store: &S) {
        let lo = self.config.start_ts;
        let hi = self.config.start_ts + self.config.duration_secs;
        for s in 0..self.config.series {
            let got = store.range(s, lo, hi).expect("range query failed");
            let want = &self.expected[s as usize];
            assert_eq!(
                got.len(),
                want.len(),
                "series {s} length mismatch: got {} want {}",
                got.len(),
                want.len()
            );
            assert_eq!(&got, want, "series {s} content mismatch");
        }
    }

    /// A short non-monotonic series (NTP steps back and far forward) for the
    /// clock-jump gate. Timestamps are deliberately out of order.
    #[must_use]
    pub fn jumpy_series() -> Vec<Sample> {
        let mut out = Vec::new();
        let mut ts = 2_000_000i64;
        for i in 0..300 {
            out.push(Sample::new(ts, (i % 11) as f64));
            ts += match i % 5 {
                0 => 1,
                1 => 1,
                2 => -3, // NTP correction backward
                3 => 1,
                _ => 100_000, // large forward jump
            };
        }
        out
    }
}

/// Round to two decimals so values are realistic (percentages) yet compress via
/// XOR window reuse.
fn round_centi(v: f64) -> f64 {
    (v * 100.0).round() / 100.0
}

#[cfg(all(test, feature = "bakeoff"))]
mod tests {
    use super::{round_centi, Corpus, CorpusConfig};
    use crate::append_only::AppendOnlyStore;
    use crate::substrate::{Durability, Substrate};

    fn fingerprint_events(events: &[(u32, crate::sample::Sample)]) -> u64 {
        events
            .iter()
            .fold(0xcbf2_9ce4_8422_2325, |hash, (series, sample)| {
                series
                    .to_le_bytes()
                    .into_iter()
                    .chain(sample.ts.to_le_bytes())
                    .chain(sample.value.to_bits().to_le_bytes())
                    .fold(hash, |hash, byte| {
                        (hash ^ u64::from(byte)).wrapping_mul(0x100_0000_01b3)
                    })
            })
    }

    fn fingerprint_samples(samples: &[crate::sample::Sample]) -> u64 {
        samples.iter().fold(0xcbf2_9ce4_8422_2325, |hash, sample| {
            sample
                .ts
                .to_le_bytes()
                .into_iter()
                .chain(sample.value.to_bits().to_le_bytes())
                .fold(hash, |hash, byte| {
                    (hash ^ u64::from(byte)).wrapping_mul(0x100_0000_01b3)
                })
        })
    }

    #[test]
    fn generation_is_deterministic_for_a_seed() {
        let cfg = CorpusConfig {
            seed: 42,
            series: 4,
            duration_secs: 100,
            ..CorpusConfig::default()
        };
        let a = Corpus::generate(cfg);
        let b = Corpus::generate(cfg);
        assert_eq!(a.events, b.events);
        assert_eq!(a.sample_count(), 400);
    }

    #[test]
    fn generated_fixture_matches_the_pinned_stream() {
        let corpus = Corpus::generate(CorpusConfig {
            seed: 0x0123_4567_89ab_cdef,
            series: 8,
            duration_secs: 512,
            start_ts: 1_234_567,
        });

        assert_eq!(corpus.events.len(), 4_096);
        assert_eq!(corpus.expected.len(), 8);
        assert!(corpus.expected.iter().all(|series| series.len() == 512));
        assert_eq!(
            fingerprint_events(&corpus.events),
            7_928_434_019_202_859_884
        );
    }

    #[test]
    fn rounding_is_stable_at_cent_boundaries() {
        let cases = [
            (0.004, 0.0),
            (0.005, 0.01),
            (12.344, 12.34),
            (12.345, 12.35),
            (99.999, 100.0),
        ];
        for (input, expected) in cases {
            assert_eq!(round_centi(input), expected, "input {input}");
        }
    }

    #[test]
    fn round_trips_through_a_substrate() {
        let corpus = Corpus::generate(CorpusConfig {
            seed: 7,
            series: 3,
            duration_secs: 300,
            ..CorpusConfig::default()
        });
        let dir = tempfile::tempdir().unwrap();
        let mut store = AppendOnlyStore::open(dir.path()).unwrap();
        corpus.replay_into(&mut store).unwrap();
        store.commit(Durability::Full).unwrap();
        corpus.assert_readback(&store);
    }

    #[test]
    fn prefix_replay_reports_the_bounded_event_count() {
        let corpus = Corpus::generate(CorpusConfig {
            seed: 9,
            series: 2,
            duration_secs: 5,
            ..CorpusConfig::default()
        });
        let dir = tempfile::tempdir().unwrap();
        let mut store = AppendOnlyStore::open(dir.path()).unwrap();

        assert_eq!(corpus.replay_prefix_into(&mut store, 3).unwrap(), 3);
        assert_eq!(corpus.replay_prefix_into(&mut store, 100).unwrap(), 10);
    }

    #[test]
    fn readback_assertion_rejects_missing_samples() {
        let corpus = Corpus::generate(CorpusConfig {
            seed: 11,
            series: 1,
            duration_secs: 2,
            ..CorpusConfig::default()
        });
        let dir = tempfile::tempdir().unwrap();
        let store = AppendOnlyStore::open(dir.path()).unwrap();

        let result = std::panic::catch_unwind(|| corpus.assert_readback(&store));
        assert!(result.is_err());
    }

    #[test]
    fn jumpy_series_is_non_monotonic() {
        let j = Corpus::jumpy_series();
        assert_eq!(j.len(), 300);
        assert_eq!(fingerprint_samples(&j), 10_004_485_525_278_665_930);
        assert!(
            j.windows(2).any(|w| w[1].ts < w[0].ts),
            "expected a backward step"
        );
    }
}
