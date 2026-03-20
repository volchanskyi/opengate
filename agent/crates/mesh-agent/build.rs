fn main() {
    // Allow CI to override the agent version via OPENGATE_VERSION env var.
    // Falls back to CARGO_PKG_VERSION if not set.
    println!("cargo:rerun-if-env-changed=OPENGATE_VERSION");
    if let Ok(ver) = std::env::var("OPENGATE_VERSION") {
        println!("cargo:rustc-env=AGENT_VERSION={ver}");
    } else {
        println!(
            "cargo:rustc-env=AGENT_VERSION={}",
            std::env::var("CARGO_PKG_VERSION").unwrap()
        );
    }
}
