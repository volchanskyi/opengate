use sha2::{Digest, Sha256};

/// Returns a stable SHA-256 hex digest for a command line.
pub fn cmdline_hash(cmdline: &str) -> String {
    let mut hasher = Sha256::new();
    hasher.update(cmdline.as_bytes());
    hex::encode(hasher.finalize())
}

/// Redacts common secret-bearing command-line fragments.
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
        if is_secret_flag(&lower) {
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
        if carries_inline_credentials(token) {
            redacted.push("[REDACTED_URL]");
            continue;
        }

        redacted.push(token);
    }

    redacted.join(" ")
}

/// Lowercase secret key names whose value is stripped, as `key=value`, `key: value` or a flag.
const SECRET_KEYS: [&str; 11] = [
    "password",
    "passwd",
    "pwd",
    "token",
    "api_key",
    "api-key",
    "apikey",
    "secret",
    "access_key",
    "access-key",
    "client_secret",
];

/// Redacts secret-bearing tokens from a raw log line before it leaves the device; ambiguous
/// auth-scheme markers redact the next token, favouring over-redaction.
pub fn redact_log_line(line: &str) -> String {
    let mut out: Vec<String> = Vec::new();
    let mut redact_next = false;

    for token in line.split_whitespace() {
        if redact_next {
            out.push("[REDACTED]".to_string());
            redact_next = false;
            continue;
        }

        let lower = token.to_ascii_lowercase();

        // The credential follows `Bearer` or `Basic`, including a glued prefix such as `auth="Bearer`.
        if lower.ends_with("bearer") || lower.ends_with("basic") {
            out.push(token.to_string());
            redact_next = true;
            continue;
        }
        if is_bare_secret_key(&lower) {
            out.push(token.to_string());
            redact_next = true;
            continue;
        }
        if carries_inline_credentials(token) {
            out.push("[REDACTED_URL]".to_string());
            continue;
        }
        if is_secret_assignment(&lower) {
            out.push(redact_kv(token));
            continue;
        }
        if is_jwt(token) || contains_aws_access_key(token) || is_gcp_api_key(token) {
            out.push("[REDACTED]".to_string());
            continue;
        }

        out.push(token.to_string());
    }

    out.join(" ")
}

/// A bare secret key (`password`, `--token`, `api_key:`) whose value is the next token.
fn is_bare_secret_key(lower: &str) -> bool {
    let key = lower.trim_start_matches('-').trim_end_matches(':');
    !key.contains('=') && !key.contains(':') && SECRET_KEYS.contains(&key)
}

/// Strips the value from a `key=value` or `key:value` token, keeping the key and separator.
fn redact_kv(token: &str) -> String {
    match token.find(['=', ':']) {
        Some(idx) => format!("{}[REDACTED]", &token[..=idx]),
        None => "[REDACTED]".to_string(),
    }
}

/// A token with inline `user:pass@host` credentials, with or without a scheme (UNC, SSH, rsync);
/// a bare email address has no colon and passes.
fn carries_inline_credentials(token: &str) -> bool {
    let Some((credential, host)) = token.rsplit_once('@') else {
        return false;
    };
    if host.is_empty() {
        return false;
    }
    // The colon after a scheme is skipped.
    let credential = credential
        .rsplit_once("://")
        .map_or(credential, |(_, after)| after);
    credential
        .split_once(':')
        .is_some_and(|(user, secret)| !user.is_empty() && !secret.is_empty())
}

/// A JSON Web Token: three non-empty base64url segments starting `eyJ`, after trimming punctuation.
fn is_jwt(token: &str) -> bool {
    let t = token
        .trim_matches(|c: char| !(c.is_ascii_alphanumeric() || c == '.' || c == '_' || c == '-'));
    t.starts_with("eyJ") && t.matches('.').count() == 2 && t.split('.').all(|s| !s.is_empty())
}

/// A Google API key: the `AIza` prefix followed by base64url characters.
fn is_gcp_api_key(token: &str) -> bool {
    (35..=45).contains(&token.len())
        && token.starts_with("AIza")
        && token[4..]
            .chars()
            .all(|c| c.is_ascii_alphanumeric() || c == '_' || c == '-')
}

fn is_secret_assignment(lower: &str) -> bool {
    SECRET_KEYS
        .iter()
        .any(|key| lower.contains(&format!("{key}=")) || lower.contains(&format!("{key}:")))
}

fn is_secret_flag(lower: &str) -> bool {
    let flag = lower.trim_start_matches('-');
    SECRET_KEYS.contains(&flag)
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
