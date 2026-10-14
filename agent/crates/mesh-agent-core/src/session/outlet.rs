//! A writer's queue the session core never waits on: it pushes freely, and takes a new session
//! frame from its lanes only while the queue is empty.

use std::sync::atomic::{AtomicUsize, Ordering};
use std::sync::Arc;

use tokio::sync::{mpsc, Notify};

/// The core's end of one writer's queue.
#[derive(Debug, Clone)]
pub(crate) struct Outlet {
    tx: mpsc::UnboundedSender<Vec<u8>>,
    queued: Arc<AtomicUsize>,
    drained: Arc<Notify>,
}

/// The writer's end: it takes what the core pushed and reports each item written.
#[derive(Debug)]
pub(crate) struct OutletReader {
    rx: mpsc::UnboundedReceiver<Vec<u8>>,
    queued: Arc<AtomicUsize>,
    drained: Arc<Notify>,
}

impl Outlet {
    /// Returns a queue and the reader its writer drains.
    pub(crate) fn new() -> (Self, OutletReader) {
        let (tx, rx) = mpsc::unbounded_channel();
        let queued = Arc::new(AtomicUsize::new(0));
        let drained = Arc::new(Notify::new());
        (
            Self {
                tx,
                queued: queued.clone(),
                drained: drained.clone(),
            },
            OutletReader {
                rx,
                queued,
                drained,
            },
        )
    }

    /// Queues `bytes` without waiting; false once the writer is gone.
    pub(crate) fn push(&self, bytes: Vec<u8>) -> bool {
        self.queued.fetch_add(1, Ordering::SeqCst);
        if self.tx.send(bytes).is_err() {
            self.queued.fetch_sub(1, Ordering::SeqCst);
            return false;
        }
        true
    }

    /// Whether everything pushed has been written.
    pub(crate) fn is_empty(&self) -> bool {
        self.queued() == 0
    }

    /// How many pushed items wait to be written.
    pub(crate) fn queued(&self) -> usize {
        self.queued.load(Ordering::SeqCst)
    }

    /// Resolves once everything pushed has been written.
    pub(crate) async fn empty(&self) {
        self.at_most(0).await;
    }

    /// Resolves once no more than `n` pushed items wait.
    pub(crate) async fn at_most(&self, n: usize) {
        loop {
            let drained = self.drained.notified();
            if self.queued() <= n {
                return;
            }
            drained.await;
        }
    }
}

impl OutletReader {
    /// The next item to write, or `None` once the core has gone.
    pub(crate) async fn next(&mut self) -> Option<Vec<u8>> {
        self.rx.recv().await
    }

    /// Reports one item written.
    pub(crate) fn written(&self) {
        self.queued.fetch_sub(1, Ordering::SeqCst);
        self.drained.notify_waiters();
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::time::Duration;

    #[tokio::test]
    async fn the_outlet_is_empty_only_once_every_item_is_written() {
        let (outlet, mut reader) = Outlet::new();
        assert!(outlet.is_empty());
        assert!(outlet.push(vec![1]));
        assert!(outlet.push(vec![2]));
        assert!(!outlet.is_empty());

        assert_eq!(reader.next().await, Some(vec![1]));
        reader.written();
        assert!(!outlet.is_empty(), "one item still waits");
        assert_eq!(reader.next().await, Some(vec![2]));
        reader.written();
        assert!(outlet.is_empty());
    }

    #[tokio::test]
    async fn waiting_for_empty_wakes_when_the_writer_drains() {
        let (outlet, mut reader) = Outlet::new();
        outlet.push(vec![9]);
        let waiter = tokio::spawn({
            let outlet = outlet.clone();
            async move { outlet.empty().await }
        });
        tokio::time::sleep(Duration::from_millis(20)).await;
        assert!(!waiter.is_finished());
        reader.next().await.unwrap();
        reader.written();
        tokio::time::timeout(Duration::from_secs(1), waiter)
            .await
            .expect("the drain wakes the waiter")
            .unwrap();
    }

    #[tokio::test]
    async fn waiting_for_a_backlog_wakes_once_it_falls_to_the_bound() {
        let (outlet, mut reader) = Outlet::new();
        for n in 0..3 {
            outlet.push(vec![n]);
        }
        assert_eq!(outlet.queued(), 3);
        let waiter = tokio::spawn({
            let outlet = outlet.clone();
            async move { outlet.at_most(1).await }
        });
        reader.next().await.unwrap();
        reader.written();
        tokio::time::sleep(Duration::from_millis(20)).await;
        assert!(!waiter.is_finished(), "two still wait");
        reader.next().await.unwrap();
        reader.written();
        tokio::time::timeout(Duration::from_secs(1), waiter)
            .await
            .unwrap()
            .unwrap();
    }

    #[tokio::test]
    async fn a_push_after_the_writer_left_reports_it() {
        let (outlet, reader) = Outlet::new();
        drop(reader);
        assert!(!outlet.push(vec![1]));
        assert!(outlet.is_empty());
    }
}
