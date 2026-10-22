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
            ControlMessage::DeviceLogsResponse {
                log_entries: result.entries,
                total_count: result.total_count,
                has_more: result.has_more,
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
