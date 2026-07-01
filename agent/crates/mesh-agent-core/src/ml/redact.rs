use sha2::{Digest, Sha256};

/// Return a stable SHA-256 hex digest for a command line.
pub fn cmdline_hash(cmdline: &str) -> String {
    let mut hasher = Sha256::new();
    hasher.update(cmdline.as_bytes());
    hex::encode(hasher.finalize())
}

/// Redact common secret-bearing command-line fragments.
pub fn redact_cmdline(cmdline: &str) -> String {
    let mut redacted = Vec::new();
    let mut redact_next = false;

    for token in cmdline.split_whitespace() {
        if redact_next {
            redacted.push("[REDACTED]");
            redact_next = false;
            continue;
        }

        let lower = token.to_ascii_lowercase();
        if lower == "bearer" {
            redacted.push(token);
            redact_next = true;
            continue;
        }
        if is_secret_assignment(&lower) {
            redacted.push(redact_assignment(token));
            continue;
        }
        if contains_aws_access_key(token) {
            redacted.push("[REDACTED]");
            continue;
        }
        if lower.contains("://") && token.contains('@') {
            redacted.push("[REDACTED_URL]");
            continue;
        }

        redacted.push(token);
    }

    redacted.join(" ")
}

fn is_secret_assignment(lower: &str) -> bool {
    const NEEDLES: [&str; 7] = [
        "password=",
        "passwd=",
        "token=",
        "api_key=",
        "api-key=",
        "apikey=",
        "secret=",
    ];
    NEEDLES.iter().any(|needle| lower.contains(needle))
}

fn redact_assignment(token: &str) -> &str {
    if token.contains('=') {
        "[REDACTED]"
    } else {
        token
    }
}

fn is_aws_access_key(token: &str) -> bool {
    token.len() >= 20
        && (token.starts_with("AKIA") || token.starts_with("ASIA"))
        && token
            .chars()
            .all(|c| c.is_ascii_uppercase() || c.is_ascii_digit())
}

fn contains_aws_access_key(token: &str) -> bool {
    if is_aws_access_key(token) {
        return true;
    }
    token
        .split_once('=')
        .is_some_and(|(_, value)| is_aws_access_key(value))
}
