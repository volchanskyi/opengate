//! Installed-package discovery through read-only `dpkg-query` and `rpm -qa` queries on Linux.

use mesh_protocol::DiscoveredPackage;

use super::command::StopSignal;
#[cfg(target_os = "linux")]
use super::command::{stdout_within, COMMAND_LIMIT};

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
pub fn collect_packages(stop: &dyn StopSignal) -> Vec<DiscoveredPackage> {
    #[cfg(target_os = "linux")]
    {
        collect_packages_linux(stop)
    }
    #[cfg(not(target_os = "linux"))]
    {
        let _ = stop;
        Vec::new()
    }
}

#[cfg(target_os = "linux")]
fn collect_packages_linux(stop: &dyn StopSignal) -> Vec<DiscoveredPackage> {
    let mut dpkg = std::process::Command::new("dpkg-query");
    dpkg.args(["-W", "-f=${Package}\t${Version}\n"]);
    if let Some(stdout) = stdout_within(&mut dpkg, COMMAND_LIMIT, stop) {
        let packages = parse_dpkg(&stdout);
        if !packages.is_empty() {
            return packages;
        }
    }
    let mut rpm = std::process::Command::new("rpm");
    rpm.args(["-qa", "--qf", "%{NAME}\t%{VERSION}\n"]);
    stdout_within(&mut rpm, COMMAND_LIMIT, stop)
        .map(|stdout| parse_rpm(&stdout))
        .unwrap_or_default()
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::discovery::command::NeverStops;

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
        let packages = collect_packages(&NeverStops);
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
