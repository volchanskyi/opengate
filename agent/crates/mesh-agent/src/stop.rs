//! One stop request shared by the background tasks, waking each from whichever sleep it is in.

use std::sync::{Arc, Condvar, Mutex, PoisonError};
use std::time::Duration;

use tokio::sync::watch;

/// A stop request that blocking threads and async tasks both wait on.
#[derive(Clone)]
pub(crate) struct Stop(Arc<Inner>);

struct Inner {
    requested: Mutex<bool>,
    wake: Condvar,
    notify: watch::Sender<bool>,
}

impl Stop {
    pub(crate) fn new() -> Self {
        Self(Arc::new(Inner {
            requested: Mutex::new(false),
            wake: Condvar::new(),
            notify: watch::Sender::new(false),
        }))
    }

    /// Asks every task to stop and wakes the ones that are sleeping.
    pub(crate) fn request(&self) {
        *self
            .0
            .requested
            .lock()
            .unwrap_or_else(PoisonError::into_inner) = true;
        self.0.wake.notify_all();
        self.0.notify.send_replace(true);
    }

    pub(crate) fn requested(&self) -> bool {
        *self
            .0
            .requested
            .lock()
            .unwrap_or_else(PoisonError::into_inner)
    }

    /// Blocks the calling thread for `duration`, returning `true` as soon as a stop is requested.
    pub(crate) fn sleep_blocking(&self, duration: Duration) -> bool {
        let guard = self
            .0
            .requested
            .lock()
            .unwrap_or_else(PoisonError::into_inner);
        let (guard, _) = self
            .0
            .wake
            .wait_timeout_while(guard, duration, |requested| !*requested)
            .unwrap_or_else(PoisonError::into_inner);
        *guard
    }

    /// Resolves once a stop is requested.
    pub(crate) async fn requested_async(&self) {
        let mut rx = self.0.notify.subscribe();
        // The sender lives in `self`, so the wait ends only by the flag turning true.
        rx.wait_for(|requested| *requested).await.ok();
    }

    /// Sleeps for `duration`, returning `true` as soon as a stop is requested.
    pub(crate) async fn sleep(&self, duration: Duration) -> bool {
        tokio::select! {
            biased;
            () = self.requested_async() => true,
            () = tokio::time::sleep(duration) => self.requested(),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::Stop;
    use std::time::{Duration, Instant};

    const LONG: Duration = Duration::from_secs(60);

    #[test]
    fn a_blocking_sleep_runs_its_full_length_without_a_request() {
        let stop = Stop::new();
        let started = Instant::now();
        assert!(!stop.sleep_blocking(Duration::from_millis(30)));
        assert!(started.elapsed() >= Duration::from_millis(30));
    }

    #[test]
    fn a_request_wakes_a_blocking_sleep() {
        let stop = Stop::new();
        let sleeper = {
            let stop = stop.clone();
            std::thread::spawn(move || {
                let started = Instant::now();
                (stop.sleep_blocking(LONG), started.elapsed())
            })
        };
        std::thread::sleep(Duration::from_millis(50));
        stop.request();
        let (stopped, slept) = sleeper.join().unwrap();
        assert!(stopped);
        assert!(slept < Duration::from_secs(5), "slept {slept:?}");
    }

    #[test]
    fn a_blocking_sleep_after_a_request_returns_at_once() {
        let stop = Stop::new();
        stop.request();
        let started = Instant::now();
        assert!(stop.sleep_blocking(LONG));
        assert!(started.elapsed() < Duration::from_secs(1));
        assert!(stop.requested());
    }

    #[tokio::test]
    async fn a_request_wakes_an_async_sleep() {
        let stop = Stop::new();
        let sleeper = {
            let stop = stop.clone();
            tokio::spawn(async move { stop.sleep(LONG).await })
        };
        tokio::time::sleep(Duration::from_millis(50)).await;
        stop.request();
        let stopped = tokio::time::timeout(Duration::from_secs(5), sleeper)
            .await
            .expect("the sleep woke")
            .unwrap();
        assert!(stopped);
    }

    #[tokio::test]
    async fn an_async_sleep_without_a_request_reports_no_stop() {
        let stop = Stop::new();
        assert!(!stop.sleep(Duration::from_millis(10)).await);
        assert!(!stop.requested());
    }

    #[tokio::test]
    async fn an_async_sleep_after_a_request_returns_at_once() {
        let stop = Stop::new();
        stop.request();
        let woke = tokio::time::timeout(Duration::from_secs(1), stop.sleep(LONG)).await;
        assert_eq!(woke, Ok(true));
    }
}
