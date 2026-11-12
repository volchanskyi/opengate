//! Answers a `RequestDeviceLogs`: the agent's own files or a host source, redacted on the device.

use std::path::PathBuf;

use mesh_protocol::ControlMessage;
use tracing::info;

use crate::requests::{self, Hold};
use crate::{host_logs, logs};

/// Collects the logs a `RequestDeviceLogs` asks for and returns the reply to send; `hold` keeps
/// log reads busy until this read has ended.
pub(crate) async fn answer(
    log_dir: PathBuf,
    request: ControlMessage,
    hold: Hold,
) -> ControlMessage {
    let ControlMessage::RequestDeviceLogs {
        log_level,
        time_from,
        time_to,
        search,
        log_offset,
        log_limit,
        source,
        unit,
    } = request
    else {
        return ControlMessage::DeviceLogsError {
            error: "not a log request".to_string(),
        };
    };
    info!(%source, "device logs requested by server");
    let filter = logs::LogFilter {
        level: non_empty(log_level),
        time_from: non_empty(time_from),
        time_to: non_empty(time_to),
        search: non_empty(search),
        offset: log_offset,
        limit: log_limit,
    };
    // "self" or "" reads the agent's own files; any other source is a host log source, refused by
    // name where the host has no reader.
    let outcome = if source.is_empty() || source == "self" {
        let collector = logs::LogCollector::new(log_dir);
        match requests::blocking(hold, move || collector.collect(&filter)).await {
            Ok(read) => read
                .map(|result| (result, Vec::new()))
                .map_err(anyhow::Error::from),
            Err(_) => Err(anyhow::anyhow!("reading the agent's own logs failed")),
        }
    } else {
        let _busy = hold;
        host_logs::collect_system_logs(&source, &filter, &unit)
            .await
            .map_err(anyhow::Error::from)
    };
    match outcome {
        Ok((mut result, available_units)) => {
            // Edge-side redaction is the first of two independent guards on secret-dense lines.
            host_logs::redact_entries(&mut result.entries);
            let trimmed = fit_log_answer(&mut result.entries, LOG_ANSWER_BYTES);
            ControlMessage::DeviceLogsResponse {
                log_entries: result.entries,
                total_count: result.total_count,
                has_more: result.has_more || trimmed,
                available_units,
            }
        }
        Err(e) => ControlMessage::DeviceLogsError {
            error: e.to_string(),
        },
    }
}

fn non_empty(value: String) -> Option<String> {
    (!value.is_empty()).then_some(value)
}

/// The encoded bytes a log answer's lines may take: the server reads no message past 1 MiB, and
/// 64 KiB stays for the answer's other fields.
const LOG_ANSWER_BYTES: usize = (1 << 20) - (64 << 10);

/// Keeps the lines that fit `budget` encoded bytes and reports whether any were left for the next
/// page; a single line too long for the budget is cut to fit, so every page holds a line.
fn fit_log_answer(entries: &mut Vec<mesh_protocol::LogEntry>, budget: usize) -> bool {
    let mut used: usize = 0;
    for (i, entry) in entries.iter_mut().enumerate() {
        let size = rmp_serde::to_vec_named(entry).map_or(usize::MAX, |encoded| encoded.len());
        if used.saturating_add(size) <= budget {
            used += size;
            continue;
        }
        if i == 0 {
            let room = budget.saturating_sub(size - entry.message.len());
            let mut cut = room.min(entry.message.len());
            while !entry.message.is_char_boundary(cut) {
                cut -= 1;
            }
            entry.message.truncate(cut);
            entries.truncate(1);
        } else {
            entries.truncate(i);
        }
        return true;
    }
    false
}

#[cfg(test)]
mod tests {
    use super::*;
    use mesh_protocol::LogEntry;

    fn line(message: String) -> LogEntry {
        LogEntry {
            timestamp: "2026-11-11T04:30:00Z".to_string(),
            level: "ERROR".to_string(),
            target: "tomcat".to_string(),
            message,
        }
    }

    fn encoded(entries: &[LogEntry]) -> usize {
        entries
            .iter()
            .map(|e| rmp_serde::to_vec_named(e).unwrap().len())
            .sum()
    }

    #[test]
    fn an_answer_that_fits_is_sent_whole() {
        let mut entries: Vec<_> = (0..10).map(|i| line(format!("line {i}"))).collect();
        assert!(!fit_log_answer(&mut entries, LOG_ANSWER_BYTES));
        assert_eq!(entries.len(), 10);
    }

    #[test]
    fn long_stack_traces_are_paged_inside_the_servers_message_ceiling() {
        let trace = "at org.apache.catalina.core.StandardWrapperValve.invoke(".repeat(150);
        let mut entries: Vec<_> = (0..1000).map(|_| line(trace.clone())).collect();

        assert!(
            fit_log_answer(&mut entries, LOG_ANSWER_BYTES),
            "the rest is on the next page"
        );
        assert!(entries.len() < 1000);
        assert!(!entries.is_empty());
        assert!(encoded(&entries) <= LOG_ANSWER_BYTES);
    }

    #[test]
    fn a_single_line_longer_than_the_budget_is_cut_to_fit() {
        let mut entries = vec![line("é".repeat(1 << 20)), line("next".to_string())];
        assert!(fit_log_answer(&mut entries, 4096));
        assert_eq!(entries.len(), 1, "the page still makes progress");
        assert!(encoded(&entries) <= 4096);
        assert!(
            entries[0].message.chars().all(|c| c == 'é'),
            "the cut falls on a character boundary"
        );
    }
}
