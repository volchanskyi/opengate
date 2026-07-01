use mesh_agent_core::ml::{ensemble::EdgeMlEnsemble, window::AnomalyRateWindow};
use std::alloc::{GlobalAlloc, Layout, System};
use std::sync::atomic::{AtomicBool, AtomicUsize, Ordering};

struct CountingAllocator;

static COUNT_ALLOCATIONS: AtomicBool = AtomicBool::new(false);
static ALLOCATION_COUNT: AtomicUsize = AtomicUsize::new(0);

unsafe impl GlobalAlloc for CountingAllocator {
    unsafe fn alloc(&self, layout: Layout) -> *mut u8 {
        if COUNT_ALLOCATIONS.load(Ordering::Relaxed) {
            ALLOCATION_COUNT.fetch_add(1, Ordering::Relaxed);
        }
        unsafe { System.alloc(layout) }
    }

    unsafe fn dealloc(&self, ptr: *mut u8, layout: Layout) {
        unsafe { System.dealloc(ptr, layout) }
    }
}

#[global_allocator]
static GLOBAL: CountingAllocator = CountingAllocator;

struct AllocationCounter;

impl AllocationCounter {
    fn start() -> Self {
        ALLOCATION_COUNT.store(0, Ordering::Relaxed);
        COUNT_ALLOCATIONS.store(true, Ordering::Relaxed);
        Self
    }

    fn count(&self) -> usize {
        ALLOCATION_COUNT.load(Ordering::Relaxed)
    }
}

impl Drop for AllocationCounter {
    fn drop(&mut self) {
        COUNT_ALLOCATIONS.store(false, Ordering::Relaxed);
    }
}

#[test]
fn detection_loop_is_allocation_free_after_model_load() {
    let samples = [
        [0.0, 0.0, 0.0],
        [0.1, 0.2, 0.1],
        [9.8, 10.0, 9.9],
        [10.1, 9.9, 10.2],
        [10.2, 10.1, 9.8],
    ];
    let ensemble = EdgeMlEnsemble::<3>::train_staggered(&samples, 6, 20).unwrap();
    let mut window = AnomalyRateWindow::new(64).unwrap();
    let probe = [50.0, 50.0, 50.0];

    let counter = AllocationCounter::start();
    for timestamp in 0..10_000 {
        let bits = u64::from(ensemble.is_anomaly(&probe));
        window.push(timestamp, bits);
        std::hint::black_box(window.rate(0));
    }

    assert_eq!(
        counter.count(),
        0,
        "detection vote + rolling anomaly window must not allocate after model load"
    );
}
