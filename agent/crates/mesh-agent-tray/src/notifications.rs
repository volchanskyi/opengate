//! Desktop notifications and clipboard operations for build info display.

use mesh_agent_ipc::TrayResponse;
use tracing::{debug, warn};

/// Show agent build info as a desktop notification and copy details to clipboard.
pub fn show_build_info(info: &TrayResponse) {
    let TrayResponse::Info {
        version,
        device_id,
        hostname,
        os,
        arch,
        server_addr,
        connected,
        uptime_secs,
        log_path,
    } = info
    else {
        return;
    };

    let status = if *connected {
        "Connected"
    } else {
        "Disconnected"
    };
    let uptime = format_uptime(*uptime_secs);

    // Full detail block for clipboard.
    let details = format!(
        "Version:   {version}\n\
         Device ID: {device_id}\n\
         Hostname:  {hostname}\n\
         OS:        {os}\n\
         Arch:      {arch}\n\
         Server:    {server_addr}\n\
         Status:    {status}\n\
         Uptime:    {uptime}\n\
         Log File:  {log_path}"
    );

    // Copy to clipboard.
    match arboard::Clipboard::new() {
        Ok(mut clipboard) => {
            if let Err(e) = clipboard.set_text(&details) {
                warn!(error = %e, "failed to copy build info to clipboard");
            } else {
                debug!("build info copied to clipboard");
            }
        }
        Err(e) => {
            warn!(error = %e, "clipboard not available");
        }
    }

    // Show desktop notification.
    let summary = format!("OpenGate Agent v{version}");
    let body = format!("{status} · {hostname}\nFull details copied to clipboard");

    if let Err(e) = notify_rust::Notification::new()
        .summary(&summary)
        .body(&body)
        .appname("OpenGate Agent")
        .timeout(notify_rust::Timeout::Milliseconds(5000))
        .show()
    {
        warn!(error = %e, "failed to show desktop notification");
    }
}

/// Show a simple notification with a title and message.
pub fn notify(title: &str, message: &str) {
    if let Err(e) = notify_rust::Notification::new()
        .summary(title)
        .body(message)
        .appname("OpenGate Agent")
        .timeout(notify_rust::Timeout::Milliseconds(3000))
        .show()
    {
        warn!(error = %e, "failed to show notification");
    }
}

/// Format seconds into human-readable "Xd Xh Xm" string.
fn format_uptime(secs: u64) -> String {
    let days = secs / 86400;
    let hours = (secs % 86400) / 3600;
    let mins = (secs % 3600) / 60;

    if days > 0 {
        format!("{days}d {hours}h {mins}m")
    } else if hours > 0 {
        format!("{hours}h {mins}m")
    } else {
        format!("{mins}m")
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_format_uptime_minutes() {
        assert_eq!(format_uptime(0), "0m");
        assert_eq!(format_uptime(59), "0m");
        assert_eq!(format_uptime(60), "1m");
        assert_eq!(format_uptime(300), "5m");
    }

    #[test]
    fn test_format_uptime_hours() {
        assert_eq!(format_uptime(3600), "1h 0m");
        assert_eq!(format_uptime(7200 + 300), "2h 5m");
    }

    #[test]
    fn test_format_uptime_days() {
        assert_eq!(format_uptime(86400), "1d 0h 0m");
        assert_eq!(format_uptime(86400 * 3 + 3600 * 14 + 60 * 22), "3d 14h 22m");
    }
}
