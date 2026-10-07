use thiserror::Error;

use super::kmeans::{KMeansError, KMeansModel};

/// Errors returned by the edge ML ensemble.
#[derive(Debug, Error)]
#[non_exhaustive]
pub enum EnsembleError {
    /// An ensemble cannot vote without at least one model.
    #[error("at least one model is required")]
    Empty,
    /// A child k-means model failed to train.
    #[error("train k-means model: {0}")]
    KMeans(#[from] KMeansError),
}

/// Consensus ensemble over multiple k=2 models for one metric vector.
#[derive(Debug, Clone)]
pub struct EdgeMlEnsemble<const D: usize> {
    models: Vec<KMeansModel<D>>,
}

impl<const D: usize> EdgeMlEnsemble<D> {
    /// Builds an ensemble from already trained models.
    pub fn from_models(models: Vec<KMeansModel<D>>) -> Result<Self, EnsembleError> {
        if models.is_empty() {
            return Err(EnsembleError::Empty);
        }
        Ok(Self { models })
    }

    /// Trains `model_count` deterministic models, each on the same bounded sample corpus.
    pub fn train_staggered(
        samples: &[[f32; D]],
        model_count: usize,
        iterations: usize,
    ) -> Result<Self, EnsembleError> {
        if model_count == 0 {
            return Err(EnsembleError::Empty);
        }

        let mut models = Vec::with_capacity(model_count);
        for _ in 0..model_count {
            models.push(KMeansModel::train(samples, iterations)?);
        }
        Ok(Self { models })
    }

    /// Reports an anomaly only when every model votes anomalous.
    pub fn is_anomaly(&self, sample: &[f32; D]) -> bool {
        self.models.iter().all(|model| model.is_anomaly(sample))
    }

    /// Returns the number of models in this ensemble.
    pub fn model_count(&self) -> usize {
        self.models.len()
    }
}
