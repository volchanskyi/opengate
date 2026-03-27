//! Context menu construction and event dispatch.

use muda::{Menu, MenuEvent, MenuId, MenuItem, PredefinedMenuItem, Submenu};

/// IDs for menu items, used to identify which action was triggered.
pub struct MenuIds {
    pub restart: MenuId,
    pub check_update: MenuId,
    pub open_chat: MenuId,
    pub open_log_file: MenuId,
    pub tail_live_logs: MenuId,
    pub journalctl: MenuId,
    pub build_info: MenuId,
    pub quit: MenuId,
}

/// Build the tray context menu and return the menu + item IDs.
pub fn build_menu() -> (Menu, MenuIds) {
    let restart = MenuItem::new("Restart Agent", true, None);
    let check_update = MenuItem::new("Check for Updates", true, None);
    let open_chat = MenuItem::new("Open Chat", true, None);

    // Logs submenu
    let logs_submenu = Submenu::new("View Logs", true);
    let open_log_file = MenuItem::new("Open Log File", true, None);
    let tail_live_logs = MenuItem::new("Tail Live Logs", true, None);
    let journalctl = MenuItem::new("journalctl (systemd)", true, None);

    logs_submenu.append(&open_log_file).unwrap();
    logs_submenu.append(&tail_live_logs).unwrap();
    logs_submenu.append(&journalctl).unwrap();

    let build_info = MenuItem::new("Agent Info", true, None);
    let quit = MenuItem::new("Quit Tray", true, None);

    let menu = Menu::new();
    menu.append(&restart).unwrap();
    menu.append(&check_update).unwrap();
    menu.append(&PredefinedMenuItem::separator()).unwrap();
    menu.append(&open_chat).unwrap();
    menu.append(&logs_submenu).unwrap();
    menu.append(&build_info).unwrap();
    menu.append(&PredefinedMenuItem::separator()).unwrap();
    menu.append(&quit).unwrap();

    let ids = MenuIds {
        restart: restart.id().clone(),
        check_update: check_update.id().clone(),
        open_chat: open_chat.id().clone(),
        open_log_file: open_log_file.id().clone(),
        tail_live_logs: tail_live_logs.id().clone(),
        journalctl: journalctl.id().clone(),
        build_info: build_info.id().clone(),
        quit: quit.id().clone(),
    };

    (menu, ids)
}

/// User action from a menu click.
#[derive(Debug, Clone, PartialEq)]
pub enum MenuAction {
    Restart,
    CheckUpdate,
    OpenChat,
    OpenLogFile,
    TailLiveLogs,
    Journalctl,
    BuildInfo,
    Quit,
}

/// Map a menu event to a menu action.
pub fn resolve_action(event: &MenuEvent, ids: &MenuIds) -> Option<MenuAction> {
    let id = event.id();
    if *id == ids.restart {
        Some(MenuAction::Restart)
    } else if *id == ids.check_update {
        Some(MenuAction::CheckUpdate)
    } else if *id == ids.open_chat {
        Some(MenuAction::OpenChat)
    } else if *id == ids.open_log_file {
        Some(MenuAction::OpenLogFile)
    } else if *id == ids.tail_live_logs {
        Some(MenuAction::TailLiveLogs)
    } else if *id == ids.journalctl {
        Some(MenuAction::Journalctl)
    } else if *id == ids.build_info {
        Some(MenuAction::BuildInfo)
    } else if *id == ids.quit {
        Some(MenuAction::Quit)
    } else {
        None
    }
}
