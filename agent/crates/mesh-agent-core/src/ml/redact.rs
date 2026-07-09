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
        if lower.contains("://") && token.contains('@') {
            redacted.push("[REDACTED_URL]");
            continue;
        }

        redacted.push(token);
    }

    redacted.join(" ")
}

/// Lowercase secret key names whose associated value must be stripped, whether
/// they appear as a `key=value` / `key: value` assignment or a bare
/// `--key value` flag. Shared by the command-line and raw-log redactors.
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

/// Redact common secret-bearing fragments from a raw log line. This is a
/// defense-in-depth backstop applied at the edge before any raw line leaves the
/// device; the server applies an equivalent guard (`redactSecrets`) so neither
/// layer is trusted alone. The line is whitespace-tokenized, so single-token
/// secrets (JWTs, connection strings, `key=value`) and two-token shapes
/// (`Bearer <tok>`, `password: <value>`) are both handled. Over-redaction is
/// preferred to leaking, so ambiguous auth-scheme markers redact the next token.
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

        // `Bearer <tok>` / `Basic <tok>` — the credential is the next token. A
        // trailing match also catches a glued prefix such as `auth="Bearer`.
        if lower.ends_with("bearer") || lower.ends_with("basic") {
            out.push(token.to_string());
            redact_next = true;
            continue;
        }
        // A bare secret key or `key:` marker whose value is the next token.
        if is_bare_secret_key(&lower) {
            out.push(token.to_string());
            redact_next = true;
            continue;
        }
        // Connection string / URL carrying `user:pass@host` credentials.
        if lower.contains("://") && token.contains('@') {
            out.push("[REDACTED_URL]".to_string());
            continue;
        }
        // Single-token `key=value` / `key:value` assignment — keep the key.
        if is_secret_assignment(&lower) {
            out.push(redact_kv(token));
            continue;
        }
        // Self-identifying secrets independent of any surrounding key.
        if is_jwt(token) || contains_aws_access_key(token) || is_gcp_api_key(token) {
            out.push("[REDACTED]".to_string());
            continue;
        }

        out.push(token.to_string());
    }

    out.join(" ")
}

/// A bare secret key (`password`, `--token`, `api_key:`) that carries no inline
/// value — its value is the following whitespace-separated token.
fn is_bare_secret_key(lower: &str) -> bool {
    let key = lower.trim_start_matches('-').trim_end_matches(':');
    !key.contains('=') && !key.contains(':') && SECRET_KEYS.contains(&key)
}

/// Strips the value from a single `key=value` / `key:value` token, preserving
/// the key and separator so the line stays readable.
fn redact_kv(token: &str) -> String {
    match token.find(['=', ':']) {
        Some(idx) => format!("{}[REDACTED]", &token[..=idx]),
        None => "[REDACTED]".to_string(),
    }
}

/// A JSON Web Token — three non-empty base64url segments starting `eyJ` (the
/// base64 of `{"`). Surrounding quotes/punctuation are trimmed first.
fn is_jwt(token: &str) -> bool {
    let t = token
        .trim_matches(|c: char| !(c.is_ascii_alphanumeric() || c == '.' || c == '_' || c == '-'));
    t.starts_with("eyJ") && t.matches('.').count() == 2 && t.split('.').all(|s| !s.is_empty())
}

/// A Google API key: the `AIza` prefix followed by ~35 base64url characters.
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
