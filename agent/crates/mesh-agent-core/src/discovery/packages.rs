//! Installed-package discovery through read-only `dpkg-query` and `rpm -qa` queries on Linux.

use mesh_protocol::DiscoveredPackage;

/// Parses `name\tversion` lines, skipping blank lines and rows missing either half.
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

pub(crate) fn parse_dpkg(stdout: &str) -> Vec<DiscoveredPackage> {
    parse_name_tab_version(stdout)
}

pub(crate) fn parse_rpm(stdout: &str) -> Vec<DiscoveredPackage> {
    parse_name_tab_version(stdout)
}

/// Lists installed packages; empty where no recognized package manager is present.
pub fn collect_packages() -> Vec<DiscoveredPackage> {
    #[cfg(target_os = "linux")]
    {
        collect_packages_linux()
    }
    #[cfg(not(target_os = "linux"))]
    {
        Vec::new()
    }
}

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

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parse_name_tab_version_reads_pairs() {
        let out = "openssl\t3.0.13-0ubuntu3\nlibc6\t2.39-0ubuntu8\n\nbroken-no-version\n";
        let packages = parse_dpkg(out);
        assert_eq!(packages.len(), 2);
        assert_eq!(packages[0].name, "openssl");
        assert_eq!(packages[0].version, "3.0.13-0ubuntu3");
        assert_eq!(packages[1].name, "libc6");
    }

    #[test]
    fn parse_name_tab_version_drops_a_row_missing_either_half() {
        assert!(
            parse_dpkg("openssl\t\n").is_empty(),
            "a name with no version is not an installed package"
        );
        assert!(
            parse_dpkg("\t3.0.13\n").is_empty(),
            "a version with no name is not an installed package"
        );
        assert!(parse_dpkg("\t\n").is_empty(), "neither half present");
        assert_eq!(
            parse_dpkg("openssl\t\nlibc6\t2.39\n").len(),
            1,
            "the complete row beside a broken one still survives"
        );
    }

    #[test]
    fn parse_rpm_reads_pairs() {
        let out = "bash\t5.2.15\ncoreutils\t9.1\n";
        let packages = parse_rpm(out);
        assert_eq!(packages.len(), 2);
        assert_eq!(packages[1].name, "coreutils");
        assert_eq!(packages[1].version, "9.1");
    }

    #[test]
    fn collect_packages_reads_the_platform_or_reports_nothing() {
        let packages = collect_packages();
        #[cfg(not(target_os = "linux"))]
        assert!(packages.is_empty(), "no package manager to read");
        for package in &packages {
            assert!(!package.name.is_empty(), "every package carries a name");
            assert!(
                !package.version.is_empty(),
                "package {} carries no version",
                package.name
            );
        }
    }
}
