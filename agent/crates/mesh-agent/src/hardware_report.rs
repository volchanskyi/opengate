//! The hardware inventory the agent reports when the server asks for it.

/// Collects hardware inventory from the local system.
pub(crate) fn collect_hardware_info() -> mesh_protocol::ControlMessage {
    use sysinfo::{Disks, System};

    let sys = System::new_all();

    let cpu_model = sys
        .cpus()
        .first()
        .map(|c| c.brand().to_string())
        .unwrap_or_default();
    let cpu_cores = sys.cpus().len() as u32;
    let ram_total_mb = sys.total_memory() / (1024 * 1024);

    let disks = Disks::new_with_refreshed_list();
    let (disk_total, disk_free) = disks.iter().fold((0u64, 0u64), |(t, f), d| {
        (t + d.total_space(), f + d.available_space())
    });

    let network_interfaces = collect_network_interfaces();
    let amt = mesh_agent_core::amt_detect::detect();

    mesh_protocol::ControlMessage::HardwareReport {
        cpu_model,
        cpu_cores,
        ram_total_mb,
        disk_total_mb: disk_total / (1024 * 1024),
        disk_free_mb: disk_free / (1024 * 1024),
        network_interfaces,
        system_uuid: mesh_agent_core::hardware::system_uuid(),
        amt_available: amt.available,
        amt_version: amt.version,
    }
}

/// Collects network interface information using libc getifaddrs.
fn collect_network_interfaces() -> Vec<mesh_protocol::NetworkInterface> {
    use std::collections::HashMap;
    use std::ffi::CStr;
    use std::net::{Ipv4Addr, Ipv6Addr};

    let mut interfaces: HashMap<String, mesh_protocol::NetworkInterface> = HashMap::new();

    unsafe {
        let mut ifaddrs: *mut libc::ifaddrs = std::ptr::null_mut();
        if libc::getifaddrs(&mut ifaddrs) != 0 {
            return Vec::new();
        }

        let mut current = ifaddrs;
        while !current.is_null() {
            let ifa = &*current;
            let name = CStr::from_ptr(ifa.ifa_name).to_string_lossy().to_string();

            let entry =
                interfaces
                    .entry(name.clone())
                    .or_insert_with(|| mesh_protocol::NetworkInterface {
                        name: name.clone(),
                        mac: String::new(),
                        ipv4: Vec::new(),
                        ipv6: Vec::new(),
                    });

            if !ifa.ifa_addr.is_null() {
                let family = (*ifa.ifa_addr).sa_family as i32;
                if family == libc::AF_INET {
                    let addr = &*(ifa.ifa_addr as *const libc::sockaddr_in);
                    let ip = Ipv4Addr::from(u32::from_be(addr.sin_addr.s_addr));
                    entry.ipv4.push(ip.to_string());
                } else if family == libc::AF_INET6 {
                    let addr = &*(ifa.ifa_addr as *const libc::sockaddr_in6);
                    let ip = Ipv6Addr::from(addr.sin6_addr.s6_addr);
                    entry.ipv6.push(ip.to_string());
                } else if family == libc::AF_PACKET {
                    let addr = &*(ifa.ifa_addr as *const libc::sockaddr_ll);
                    if addr.sll_halen == 6 {
                        entry.mac = format!(
                            "{:02x}:{:02x}:{:02x}:{:02x}:{:02x}:{:02x}",
                            addr.sll_addr[0],
                            addr.sll_addr[1],
                            addr.sll_addr[2],
                            addr.sll_addr[3],
                            addr.sll_addr[4],
                            addr.sll_addr[5],
                        );
                    }
                }
            }

            current = ifa.ifa_next;
        }

        libc::freeifaddrs(ifaddrs);
    }

    interfaces.into_values().collect()
}
