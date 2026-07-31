use mesh_agent_core::ml::{ensemble::EdgeMlEnsemble, window::AnomalyRateWindow};
use std::alloc::{GlobalAlloc, Layout, System};
use std::cell::Cell;

struct CountingAllocator;

thread_local! {
    /// Set only while this thread is inside the measured region.
    static COUNTING: Cell<bool> = const { Cell::new(false) };
    /// Allocations this thread made while `COUNTING` was set.
    static COUNT: Cell<usize> = const { Cell::new(0) };
}

// Counting is per-thread on purpose. A global allocator sees every thread in
// the process, and the claim under test is about one loop on one thread — so a
// process-wide counter also charges it for the test harness's own bookkeeping
// and for whatever the coverage runtime does on its own schedule. Those are
// timing-dependent: they show up when the machine is loaded enough for another
// thread to be scheduled mid-loop, which makes the assertion pass or fail on
// core count rather than on the code it is describing.
//
// Both thread-locals are `const`-initialized, so registering them cannot itself
// allocate — an allocating initializer would re-enter this allocator. They also
// hold `Copy` types with no drop glue, so no TLS destructor is registered and
// `with` cannot panic on a thread that is shutting down.
unsafe impl GlobalAlloc for CountingAllocator {
    unsafe fn alloc(&self, layout: Layout) -> *mut u8 {
        if COUNTING.with(Cell::get) {
            COUNT.with(|count| count.set(count.get() + 1));
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
        COUNT.with(|count| count.set(0));
        COUNTING.with(|counting| counting.set(true));
        Self
    }

    fn count(&self) -> usize {
        COUNT.with(Cell::get)
    }
}

impl Drop for AllocationCounter {
    fn drop(&mut self) {
        COUNTING.with(|counting| counting.set(false));
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

/// The counter has to be able to fail, or the test above proves nothing. One
/// allocation on the measured thread is observed; the same allocation made
/// before the measured region is not.
#[test]
fn counter_observes_allocations_on_the_measured_thread_only() {
    let before = vec![1u8; 32];
    std::hint::black_box(&before);

    let counter = AllocationCounter::start();
    let during = vec![1u8; 32];
    std::hint::black_box(&during);
    let observed = counter.count();
    drop(counter);

    assert_eq!(observed, 1, "an allocation inside the region is counted");

    // A child thread's own allocations are not charged here, which is what
    // keeps the harness's allocations out of the measurement above. Spawning
    // and joining does allocate on *this* thread, so the two runs are compared
    // against each other: the only difference between them is the work the
    // child does, and the counts have to come out equal.
    fn spawn_join_cost(child_allocations: usize) -> usize {
        let counter = AllocationCounter::start();
        std::thread::spawn(move || {
            for _ in 0..child_allocations {
                std::hint::black_box(vec![1u8; 32]);
            }
        })
        .join()
        .unwrap();
        counter.count()
    }

    assert_eq!(
        spawn_join_cost(0),
        spawn_join_cost(10_000),
        "another thread's allocations are not charged to this one"
    );
}
