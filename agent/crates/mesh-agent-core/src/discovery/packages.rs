//! Installed-package discovery (WS-16).
//!
//! Enumerates installed OS packages through read-only package-manager queries:
//! `dpkg-query` (Debian/Ubuntu) or `rpm -qa` (RHEL/SUSE) on Linux. Nothing is
//! installed, removed, or upgraded. The output is bounded by
//! [`super::MAX_PACKAGES`]; a host with no recognized package manager
//! contributes nothing.

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

/// Reads installed packages, bounded and normalized. Empty on any platform
/// where no recognized package manager is present.
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

    /// A tab-separated line with either half missing is dropped — **either**
    /// half, not only both. `dpkg-query` emits a bare tab for a package whose
    /// version field is unset, and a nameless or versionless row would reach the
    /// inventory as a package that cannot be identified or matched against an
    /// advisory. The line still splits on its tab, so this is the only input
    /// that reaches the emptiness check at all.
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

    /// rpm output uses the same tab format and parses identically.
    #[test]
    fn parse_rpm_reads_pairs() {
        let out = "bash\t5.2.15\ncoreutils\t9.1\n";
        let packages = parse_rpm(out);
        assert_eq!(packages.len(), 2);
        assert_eq!(packages[1].name, "coreutils");
        assert_eq!(packages[1].version, "9.1");
    }

    /// The collector reads this platform's own package manager: on Linux every
    /// entry it reports carries a name and a version (`dpkg-query`, then `rpm`),
    /// and a host with neither installed contributes nothing. On a platform with
    /// no package manager to read there is nothing to report at all. The call is
    /// safe either way — never a panic, so no caller needs a platform branch.
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
