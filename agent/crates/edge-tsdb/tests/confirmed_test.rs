//! The spans the server confirmed: minute-aligned, coalesced, cut back on request, and kept by
//! the store across a restart.

use edge_tsdb::confirmed::ConfirmedRanges;
use edge_tsdb::store::LocalTsdb;
use edge_tsdb::{Durability, TsdbConfig};

const MIN: i64 = 60;
const T: i64 = 1_700_000_040;

#[test]
fn adjacent_and_overlapping_spans_coalesce() {
    let mut ranges = ConfirmedRanges::default();
    ranges.add(T, T + MIN);
    ranges.add(T + 2 * MIN, T + 3 * MIN);
    ranges.add(T + MIN, T + 2 * MIN);
    ranges.add(T + 5 * MIN, T + 7 * MIN);
    ranges.add(T + 6 * MIN, T + 9 * MIN);
    assert_eq!(
        ranges.spans(),
        &[(T, T + 3 * MIN), (T + 5 * MIN, T + 9 * MIN)]
    );
}

#[test]
fn a_span_is_shrunk_to_whole_minutes() {
    let mut ranges = ConfirmedRanges::default();
    ranges.add(T + 10, T + 2 * MIN + 30);
    assert_eq!(
        ranges.spans(),
        &[(T + MIN, T + 2 * MIN)],
        "only minutes wholly inside the span count as confirmed"
    );
    ranges.add(T + 3 * MIN + 1, T + 3 * MIN + 59);
    assert_eq!(
        ranges.spans().len(),
        1,
        "a span inside one minute confirms nothing"
    );
}

#[test]
fn forgetting_after_a_time_cuts_back_to_the_minute_holding_it() {
    let mut ranges = ConfirmedRanges::default();
    ranges.add(T, T + 10 * MIN);
    ranges.add(T + 20 * MIN, T + 30 * MIN);
    ranges.forget_after(T + 4 * MIN + 30);
    assert_eq!(
        ranges.spans(),
        &[(T, T + 4 * MIN)],
        "the minute holding T and everything after is sent again"
    );
}

#[test]
fn covers_and_span_end_read_the_spans() {
    let mut ranges = ConfirmedRanges::default();
    ranges.add(T, T + 3 * MIN);
    assert!(ranges.covers(T + MIN, T + 2 * MIN));
    assert!(
        !ranges.covers(T + 2 * MIN, T + 4 * MIN),
        "a bucket reaching past the span is not covered"
    );
    assert_eq!(ranges.span_end_at(T + 90), Some(T + 3 * MIN));
    assert_eq!(
        ranges.span_end_at(T + 3 * MIN),
        None,
        "the end itself is unconfirmed"
    );
    assert_eq!(ranges.span_end_at(T - 1), None);
}

#[test]
fn the_store_keeps_confirmations_across_a_restart_and_forgets_them_on_purge() {
    let dir = tempfile::tempdir().unwrap();
    let mut ranges = ConfirmedRanges::default();
    ranges.add(T, T + 5 * MIN);
    {
        let mut db = LocalTsdb::open(dir.path(), TsdbConfig::default()).unwrap();
        assert!(
            db.confirmed().unwrap().spans().is_empty(),
            "nothing confirmed before the first answer"
        );
        db.save_confirmed(&ranges, Durability::Full).unwrap();
    }
    let mut db = LocalTsdb::open(dir.path(), TsdbConfig::default()).unwrap();
    assert_eq!(db.confirmed().unwrap(), ranges);
    db.purge().unwrap();
    assert!(db.confirmed().unwrap().spans().is_empty());
}

#[test]
fn a_cursor_removed_from_the_store_reads_as_never_set() {
    let dir = tempfile::tempdir().unwrap();
    let mut db = LocalTsdb::open(dir.path(), TsdbConfig::default()).unwrap();
    db.set_cursor(7, T, Durability::Full).unwrap();
    db.remove_cursor(7, Durability::Full).unwrap();
    db.remove_cursor(8, Durability::Full).unwrap();
    assert_eq!(db.cursor(7).unwrap(), None);
}
