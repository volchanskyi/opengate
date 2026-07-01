use thiserror::Error;

/// Errors returned while training a k-means anomaly model.
#[derive(Debug, Error, PartialEq, Eq)]
#[non_exhaustive]
pub enum KMeansError {
    /// At least two samples are required to train k=2.
    #[error("at least two samples are required")]
    TooFewSamples,
    /// Samples must contain only finite values.
    #[error("all sample values must be finite")]
    NonFiniteSample,
}

/// A deterministic k=2 k-means model with an anomaly distance boundary.
#[derive(Debug, Clone, PartialEq)]
pub struct KMeansModel<const D: usize> {
    centers: [[f32; D]; 2],
    threshold: f32,
}

impl<const D: usize> KMeansModel<D> {
    /// Train a k=2 model using deterministic Lloyd iterations.
    pub fn train(samples: &[[f32; D]], iterations: usize) -> Result<Self, KMeansError> {
        if samples.len() < 2 {
            return Err(KMeansError::TooFewSamples);
        }
        if samples
            .iter()
            .flat_map(|sample| sample.iter())
            .any(|value| !value.is_finite())
        {
            return Err(KMeansError::NonFiniteSample);
        }

        let (first, second) = farthest_pair(samples);
        let mut centers = [samples[first], samples[second]];

        for _ in 0..iterations.max(1) {
            let mut sums = [[0.0f32; D]; 2];
            let mut counts = [0usize; 2];

            for sample in samples {
                let cluster = nearest_center(sample, &centers);
                for (dim, value) in sample.iter().enumerate() {
                    sums[cluster][dim] += *value;
                }
                counts[cluster] += 1;
            }

            for cluster in 0..2 {
                if counts[cluster] == 0 {
                    continue;
                }
                for dim in 0..D {
                    centers[cluster][dim] = sums[cluster][dim] / counts[cluster] as f32;
                }
            }
        }

        let mut distances: Vec<f32> = samples
            .iter()
            .map(|sample| nearest_distance(sample, &centers))
            .collect();
        distances.sort_by(f32::total_cmp);
        let boundary_index = ((distances.len() - 1) as f32 * 0.99).floor() as usize;

        Ok(Self {
            centers,
            threshold: distances[boundary_index],
        })
    }

    /// Return the two trained cluster centers.
    pub fn centers(&self) -> [[f32; D]; 2] {
        self.centers
    }

    /// Return the learned anomaly threshold.
    pub fn threshold(&self) -> f32 {
        self.threshold
    }

    /// Return true when a sample is farther than the learned boundary.
    pub fn is_anomaly(&self, sample: &[f32; D]) -> bool {
        nearest_distance(sample, &self.centers) > self.threshold
    }
}

fn farthest_pair<const D: usize>(samples: &[[f32; D]]) -> (usize, usize) {
    let mut best = (0usize, 1usize);
    let mut best_distance = squared_distance(&samples[0], &samples[1]);

    for left in 0..samples.len() {
        for right in (left + 1)..samples.len() {
            let distance = squared_distance(&samples[left], &samples[right]);
            if distance > best_distance {
                best = (left, right);
                best_distance = distance;
            }
        }
    }

    best
}

fn nearest_center<const D: usize>(sample: &[f32; D], centers: &[[f32; D]; 2]) -> usize {
    let left = squared_distance(sample, &centers[0]);
    let right = squared_distance(sample, &centers[1]);
    if left <= right {
        0
    } else {
        1
    }
}

fn nearest_distance<const D: usize>(sample: &[f32; D], centers: &[[f32; D]; 2]) -> f32 {
    squared_distance(sample, &centers[nearest_center(sample, centers)]).sqrt()
}

fn squared_distance<const D: usize>(a: &[f32; D], b: &[f32; D]) -> f32 {
    a.iter()
        .zip(b.iter())
        .map(|(left, right)| {
            let delta = left - right;
            delta * delta
        })
        .sum()
}
