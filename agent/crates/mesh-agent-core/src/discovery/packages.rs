//! Installed-package discovery (WS-16).
//!
//! Enumerates installed OS packages through read-only package-manager queries:
//! `dpkg-query` (Debian/Ubuntu) or `rpm -qa` (RHEL/SUSE) on Linux, and the
//! uninstall registry via PowerShell on Windows. Nothing is installed, removed,
//! or upgraded. The output is bounded by [`super::MAX_PACKAGES`]; a host with no
//! recognized package manager contributes nothing.

use mesh_protocol::DiscoveredPackage;

/// Parses tab-separated `name\tversion` lines (the format emitted by both
/// `dpkg-query -W -f='${Package}\t${Version}\n'` and
/// `rpm -qa --qf '%{NAME}\t%{VERSION}\n'`). Blank lines and lines missing a
/// version are skipped.
fn parse_name_tab_version(stdout: &str) -> Vec<DiscoveredPackage> {
    let mut out = Vec::new();
    for line in stdout.lines() {
        let Some((name, version)) = line.split_once('\t') else {
            continue;
        };
        let name = name.trim();
        let version = version.trim();
        if name.is_empty() || version.is_empty() {
            continue;
        }
        out.push(DiscoveredPackage {
            name: name.to_string(),
            version: version.to_string(),
        });
    }
    out
}

/// Parses `dpkg-query -W -f='${Package}\t${Version}\n'` output.
pub(crate) fn parse_dpkg(stdout: &str) -> Vec<DiscoveredPackage> {
    parse_name_tab_version(stdout)
}

/// Parses `rpm -qa --qf '%{NAME}\t%{VERSION}\n'` output.
pub(crate) fn parse_rpm(stdout: &str) -> Vec<DiscoveredPackage> {
    parse_name_tab_version(stdout)
}

/// Parses the Windows uninstall-registry JSON (`Get-ItemProperty … |
/// Select DisplayName,DisplayVersion | ConvertTo-Json`). Records without a
/// display name are skipped; a missing version becomes empty. A top-level array
/// or a bare object are both handled.
#[cfg(any(target_os = "windows", test))]
pub(crate) fn parse_windows_packages(json: &str) -> Vec<DiscoveredPackage> {
    let value: serde_json::Value = match serde_json::from_str(json) {
        Ok(value) => value,
        Err(_) => return Vec::new(),
    };
    let mut out = Vec::new();
    match value {
        serde_json::Value::Array(records) => {
            for record in &records {
                if let Some(pkg) = parse_windows_package(record) {
                    out.push(pkg);
                }
            }
        }
        other => {
            if let Some(pkg) = parse_windows_package(&other) {
                out.push(pkg);
            }
        }
    }
    out
}

/// Parses one Windows uninstall-registry record. A record without a
/// `DisplayName` is not an installed product and yields `None`.
#[cfg(any(target_os = "windows", test))]
fn parse_windows_package(value: &serde_json::Value) -> Option<DiscoveredPackage> {
    let name = value.get("DisplayName").and_then(|v| v.as_str())?.trim();
    if name.is_empty() {
        return None;
    }
    let version = value
        .get("DisplayVersion")
        .and_then(|v| v.as_str())
        .unwrap_or("")
        .trim()
        .to_string();
    Some(DiscoveredPackage {
        name: name.to_string(),
        version,
    })
}

/// Reads installed packages, bounded and normalized. Empty on any platform
/// where no recognized package manager is present.
pub fn collect_packages() -> Vec<DiscoveredPackage> {
    #[cfg(target_os = "linux")]
    {
        collect_packages_linux()
    }
    #[cfg(target_os = "windows")]
    {
        collect_packages_windows()
    }
    #[cfg(not(any(target_os = "linux", target_os = "windows")))]
    {
        Vec::new()
    }
}

/// Tries `dpkg-query` first, then `rpm`. Empty when neither is present.
#[cfg(target_os = "linux")]
fn collect_packages_linux() -> Vec<DiscoveredPackage> {
    let dpkg = std::process::Command::new("dpkg-query")
        .args(["-W", "-f=${Package}\t${Version}\n"])
        .output();
    if let Ok(output) = dpkg {
        if output.status.success() {
            let packages = parse_dpkg(&String::from_utf8_lossy(&output.stdout));
            if !packages.is_empty() {
                return packages;
            }
        }
    }
    let rpm = std::process::Command::new("rpm")
        .args(["-qa", "--qf", "%{NAME}\t%{VERSION}\n"])
        .output();
    match rpm {
        Ok(output) if output.status.success() => {
            parse_rpm(&String::from_utf8_lossy(&output.stdout))
        }
        _ => Vec::new(),
    }
}

/// Reads the 64- and 32-bit uninstall registry hives via PowerShell. Empty on
/// any failure path.
#[cfg(target_os = "windows")]
fn collect_packages_windows() -> Vec<DiscoveredPackage> {
    let script = concat!(
        "Get-ItemProperty ",
        "HKLM:\\Software\\Microsoft\\Windows\\CurrentVersion\\Uninstall\\*, ",
        "HKLM:\\Software\\WOW6432Node\\Microsoft\\Windows\\CurrentVersion\\Uninstall\\* ",
        "| Where-Object { $_.DisplayName } ",
        "| Select-Object DisplayName,DisplayVersion | ConvertTo-Json -Compress"
    );
    let output = std::process::Command::new("powershell")
        .args(["-NoProfile", "-NonInteractive", "-Command", script])
        .output();
    match output {
        Ok(output) if output.status.success() => {
            parse_windows_packages(&String::from_utf8_lossy(&output.stdout))
        }
        _ => Vec::new(),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    /// dpkg/rpm tab-separated lines parse to name + version; blank and
    /// version-less lines are skipped.
    #[test]
    fn parse_name_tab_version_reads_pairs() {
        let out = "openssl\t3.0.13-0ubuntu3\nlibc6\t2.39-0ubuntu8\n\nbroken-no-version\n";
        let packages = parse_dpkg(out);
        assert_eq!(packages.len(), 2);
        assert_eq!(packages[0].name, "openssl");
        assert_eq!(packages[0].version, "3.0.13-0ubuntu3");
        assert_eq!(packages[1].name, "libc6");
    }

    /// rpm output uses the same tab format and parses identically.
    #[test]
    fn parse_rpm_reads_pairs() {
        let out = "bash\t5.2.15\ncoreutils\t9.1\n";
        let packages = parse_rpm(out);
        assert_eq!(packages.len(), 2);
        assert_eq!(packages[1].name, "coreutils");
        assert_eq!(packages[1].version, "9.1");
    }

    /// The Windows uninstall-registry JSON array parses each product; a missing
    /// version is empty and a record without a display name is dropped.
    #[test]
    fn parse_windows_packages_reads_array() {
        let json = r#"[
            {"DisplayName":"7-Zip 23.01","DisplayVersion":"23.01"},
            {"DisplayName":"Some Driver"},
            {"DisplayVersion":"1.0"}
        ]"#;
        let packages = parse_windows_packages(json);
        assert_eq!(packages.len(), 2, "the version-less record still counts");
        assert_eq!(packages[0].name, "7-Zip 23.01");
        assert_eq!(packages[0].version, "23.01");
        assert_eq!(packages[1].name, "Some Driver");
        assert!(packages[1].version.is_empty());
    }

    /// A bare object (single product) is handled, and bad JSON yields nothing.
    #[test]
    fn parse_windows_packages_object_and_bad_json() {
        let one = parse_windows_packages(r#"{"DisplayName":"Notepad++","DisplayVersion":"8.6"}"#);
        assert_eq!(one.len(), 1);
        assert_eq!(one[0].name, "Notepad++");
        assert!(parse_windows_packages("}{").is_empty());
    }
}
