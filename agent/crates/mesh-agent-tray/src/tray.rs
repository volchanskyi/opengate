//! System tray icon management.
//!
//! Handles icon state changes (connected/disconnected/updating) and
//! tooltip updates.

use tray_icon::Icon;
use tray_icon::TrayIcon;

/// Tray icon states that map to different visual indicators.
#[derive(Debug, Clone, Copy, PartialEq)]
#[allow(dead_code)]
pub enum IconState {
    /// Agent is connected to server.
    Connected,
    /// Agent is reconnecting to server.
    Reconnecting,
    /// Agent is disconnected.
    Disconnected,
    /// Agent is performing an update.
    Updating,
    /// Tray cannot reach the agent service.
    Unavailable,
}

impl IconState {
    /// RGBA color for the icon state (used to generate colored circles).
    fn color(&self) -> [u8; 4] {
        match self {
            Self::Connected => [0x4C, 0xAF, 0x50, 0xFF],    // Green
            Self::Reconnecting => [0xFF, 0xC1, 0x07, 0xFF], // Yellow
            Self::Disconnected => [0x9E, 0x9E, 0x9E, 0xFF], // Gray
            Self::Updating => [0x21, 0x96, 0xF3, 0xFF],     // Blue
            Self::Unavailable => [0xF4, 0x43, 0x36, 0xFF],  // Red
        }
    }

    fn tooltip_suffix(&self) -> &'static str {
        match self {
            Self::Connected => "Connected",
            Self::Reconnecting => "Reconnecting...",
            Self::Disconnected => "Disconnected",
            Self::Updating => "Updating...",
            Self::Unavailable => "Service unavailable",
        }
    }
}

/// Generate a simple colored circle icon as RGBA pixel data.
/// This serves as placeholder until proper designed icons are added.
fn generate_icon(state: IconState, size: u32) -> Icon {
    let color = state.color();
    let mut rgba = Vec::with_capacity((size * size * 4) as usize);
    let center = size as f32 / 2.0;
    let radius = center - 1.0;

    for y in 0..size {
        for x in 0..size {
            let dx = x as f32 - center;
            let dy = y as f32 - center;
            let dist = (dx * dx + dy * dy).sqrt();

            if dist <= radius {
                // Inside the circle
                rgba.extend_from_slice(&color);
            } else if dist <= radius + 1.0 {
                // Anti-aliased edge
                let alpha = ((radius + 1.0 - dist) * color[3] as f32) as u8;
                rgba.extend_from_slice(&[color[0], color[1], color[2], alpha]);
            } else {
                // Transparent
                rgba.extend_from_slice(&[0, 0, 0, 0]);
            }
        }
    }

    Icon::from_rgba(rgba, size, size).expect("valid icon dimensions")
}

/// Create a tray icon with the given initial state.
pub fn create_tray_icon(menu: &muda::Menu, state: IconState, version: &str) -> tray_icon::TrayIcon {
    let icon = generate_icon(state, 32);
    let tooltip = format!("OpenGate Agent v{version} — {}", state.tooltip_suffix());

    tray_icon::TrayIconBuilder::new()
        .with_menu(Box::new(menu.clone()))
        .with_tooltip(&tooltip)
        .with_icon(icon)
        .build()
        .expect("failed to build tray icon")
}

/// Update the tray icon state and tooltip.
pub fn update_icon(tray: &TrayIcon, state: IconState, version: &str) {
    let icon = generate_icon(state, 32);
    let tooltip = format!("OpenGate Agent v{version} — {}", state.tooltip_suffix());

    let _ = tray.set_icon(Some(icon));
    let _ = tray.set_tooltip(Some(&tooltip));
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_generate_icon_connected() {
        let icon = generate_icon(IconState::Connected, 16);
        // Just verify it doesn't panic
        let _ = icon;
    }

    #[test]
    fn test_generate_icon_all_states() {
        for state in [
            IconState::Connected,
            IconState::Reconnecting,
            IconState::Disconnected,
            IconState::Updating,
            IconState::Unavailable,
        ] {
            let _ = generate_icon(state, 32);
        }
    }

    #[test]
    fn test_icon_state_tooltips() {
        assert_eq!(IconState::Connected.tooltip_suffix(), "Connected");
        assert_eq!(
            IconState::Unavailable.tooltip_suffix(),
            "Service unavailable"
        );
    }
}
