//! The spans of time the server has confirmed it stores, which catch-up skips.

/// Seconds in the minute grid every confirmed span is aligned to.
const MINUTE: i64 = 60;

/// Coalesced, non-overlapping `[start, end)` spans the server confirmed, ascending, each a whole
/// number of minutes.
#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct ConfirmedRanges {
    spans: Vec<(i64, i64)>,
}

impl ConfirmedRanges {
    /// Builds the ranges from stored spans in one sorted pass, coalescing any that touch.
    #[must_use]
    pub fn from_spans(spans: impl IntoIterator<Item = (i64, i64)>) -> Self {
        let mut whole: Vec<(i64, i64)> = spans
            .into_iter()
            .filter_map(|(start, end)| whole_minutes(start, end))
            .collect();
        whole.sort_unstable();
        let mut merged: Vec<(i64, i64)> = Vec::with_capacity(whole.len());
        for (start, end) in whole {
            match merged.last_mut() {
                Some(last) if start <= last.1 => last.1 = last.1.max(end),
                _ => merged.push((start, end)),
            }
        }
        Self { spans: merged }
    }

    /// The spans, ascending.
    #[must_use]
    pub fn spans(&self) -> &[(i64, i64)] {
        &self.spans
    }

    /// Confirms the whole minutes inside `[start, end)`, merging with the spans it touches.
    pub fn add(&mut self, start: i64, end: i64) {
        let Some((mut lo, mut hi)) = whole_minutes(start, end) else {
            return;
        };
        let mut kept = Vec::with_capacity(self.spans.len() + 1);
        for &(s, e) in &self.spans {
            if e < lo || s > hi {
                kept.push((s, e));
            } else {
                lo = lo.min(s);
                hi = hi.max(e);
            }
        }
        kept.push((lo, hi));
        kept.sort_unstable();
        self.spans = kept;
    }

    /// Forgets every confirmation from the minute holding `t` onwards.
    pub fn forget_after(&mut self, t: i64) {
        let cut = t.div_euclid(MINUTE) * MINUTE;
        self.spans.retain_mut(|span| {
            span.1 = span.1.min(cut);
            span.0 < span.1
        });
    }

    /// Forgets every span that ended at or before `t`; a span reaching past it is kept whole.
    pub fn forget_before(&mut self, t: i64) {
        self.spans.retain(|&(_, end)| end > t);
    }

    /// Whether `[start, end)` lies inside one confirmed span.
    #[must_use]
    pub fn covers(&self, start: i64, end: i64) -> bool {
        self.spans.iter().any(|&(s, e)| s <= start && end <= e)
    }

    /// The end of the confirmed span holding `ts`, where catch-up resumes, or `None` when `ts` is
    /// unconfirmed.
    #[must_use]
    pub fn span_end_at(&self, ts: i64) -> Option<i64> {
        self.spans
            .iter()
            .find(|&&(s, e)| s <= ts && ts < e)
            .map(|&(_, e)| e)
    }
}

/// The whole minutes inside `[start, end)`, or `None` when it holds none.
fn whole_minutes(start: i64, end: i64) -> Option<(i64, i64)> {
    let (start, end) = (ceil_minute(start), end.div_euclid(MINUTE) * MINUTE);
    (start < end).then_some((start, end))
}

/// The first minute boundary at or after `ts`.
fn ceil_minute(ts: i64) -> i64 {
    let floor = ts.div_euclid(MINUTE) * MINUTE;
    if floor == ts {
        ts
    } else {
        floor + MINUTE
    }
}
