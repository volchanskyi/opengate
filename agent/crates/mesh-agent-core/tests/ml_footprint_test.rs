use mesh_agent_core::ml::{ensemble::EdgeMlEnsemble, window::AnomalyRateWindow};

#[cfg(target_os = "linux")]
#[test]
fn ensemble_and_window_rss_delta_stays_under_one_mib() {
    let before = current_rss_kib();
    let samples = [
        [0.0, 0.0, 0.0],
        [0.1, 0.2, 0.1],
        [9.8, 10.0, 9.9],
        [10.1, 9.9, 10.2],
        [10.2, 10.1, 9.8],
    ];
    let ensemble = EdgeMlEnsemble::<3>::train_staggered(&samples, 6, 20).unwrap();
    let window = AnomalyRateWindow::new(120).unwrap();
    let after = current_rss_kib();

    std::hint::black_box((&ensemble, &window));
    assert!(
        after.saturating_sub(before) <= 1024,
        "ensemble + anomaly ring RSS delta exceeded 1 MiB: before={before}KiB after={after}KiB"
    );
}

#[cfg(target_os = "linux")]
fn current_rss_kib() -> usize {
    let statm = std::fs::read_to_string("/proc/self/statm").expect("read /proc/self/statm");
    let resident_pages = statm
        .split_whitespace()
        .nth(1)
        .expect("resident page count")
        .parse::<usize>()
        .expect("parse resident page count");
    resident_pages * 4
}
