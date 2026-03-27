fn main() {
    // Version from env (CI) → git tag → Cargo.toml fallback.
    let version = std::env::var("OPENGATE_VERSION").unwrap_or_else(|_| {
        std::process::Command::new("git")
            .args(["describe", "--tags", "--abbrev=0"])
            .output()
            .ok()
            .and_then(|o| {
                if o.status.success() {
                    String::from_utf8(o.stdout)
                        .ok()
                        .map(|s| s.trim().trim_start_matches('v').to_string())
                } else {
                    None
                }
            })
            .unwrap_or_else(|| env!("CARGO_PKG_VERSION").to_string())
    });
    println!("cargo:rustc-env=TRAY_VERSION={version}");
}
