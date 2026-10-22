//! Enrollment with the install note's token: a request for a key held in memory, and one
//! crash-safe save of the issued identity that also trusts the note's master key and consumes it.

use anyhow::{anyhow, Context};
use serde::{Deserialize, Serialize};
use tracing::info;

use crate::client_tls;
use crate::lifecycle::{InstallNote, Paths, UPDATE_SIGNING_KEY_FILE};
use mesh_agent_core::identity_store::{self, Commit};
use mesh_agent_core::{AgentIdentity, PendingIdentity};

/// Request body for the enrollment endpoint.
#[derive(Serialize)]
struct EnrollRequestBody {
    csr_pem: String,
}

/// Response from the enrollment endpoint.
#[derive(Deserialize)]
struct EnrollResponse {
    cert_pem: Option<String>,
    device_id: Option<String>,
    update_signing_key: Option<String>,
}

/// Why an enrollment produced no identity.
#[derive(Debug)]
pub(crate) enum EnrollError {
    /// The server refused the token, quoting its reason.
    Refused(String),
    /// The server could not be reached or failed, so the attempt can be repeated.
    Unreachable(anyhow::Error),
    /// The reply could not become an identity; nothing was saved.
    Invalid(anyhow::Error),
}

impl std::fmt::Display for EnrollError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::Refused(why) => write!(f, "enrollment refused: {why}"),
            Self::Unreachable(e) => write!(f, "enrollment server unreachable: {e:#}"),
            Self::Invalid(e) => write!(f, "enrollment reply refused: {e:#}"),
        }
    }
}

/// Enrolls with `note` and saves the identity the server issued under the number it chose.
pub(crate) async fn enroll(
    note: &InstallNote,
    paths: &Paths,
) -> Result<AgentIdentity, EnrollError> {
    info!("enrolling with the install note's token");
    let pending = PendingIdentity::generate(None)
        .context("generate enrollment key")
        .map_err(EnrollError::Invalid)?;

    let url = format!(
        "{}/api/v1/enroll/{}",
        note.url.trim_end_matches('/'),
        note.token
    );
    // The reply is checked against the note's master key, so the transport needs none.
    let client = reqwest::Client::builder()
        .danger_accept_invalid_certs(true)
        .connect_timeout(std::time::Duration::from_secs(10))
        .timeout(std::time::Duration::from_secs(30))
        .build()
        .map_err(|e| EnrollError::Invalid(e.into()))?;
    let resp = client
        .post(&url)
        .json(&EnrollRequestBody {
            csr_pem: pending.csr_pem.clone(),
        })
        .send()
        .await
        .map_err(|e| EnrollError::Unreachable(e.into()))?;

    let status = resp.status();
    if !status.is_success() {
        let body = resp.text().await.unwrap_or_default();
        let why = format!("HTTP {status}: {body}");
        return Err(if status.is_client_error() {
            EnrollError::Refused(why)
        } else {
            EnrollError::Unreachable(anyhow!(why))
        });
    }
    let reply: EnrollResponse = resp
        .json()
        .await
        .map_err(|e| EnrollError::Invalid(anyhow!("parse enrollment response: {e}")))?;

    let identity = identity_from_reply(&reply, pending, note).map_err(EnrollError::Invalid)?;
    save(&identity, &reply, note, paths).map_err(EnrollError::Invalid)?;
    info!(device_id = %identity.device_id.0, "enrollment complete");
    Ok(identity)
}

/// Turns the reply into an identity, refusing one whose stated number differs from its
/// certificate's or whose certificate the note's master key did not issue.
fn identity_from_reply(
    reply: &EnrollResponse,
    pending: PendingIdentity,
    note: &InstallNote,
) -> anyhow::Result<AgentIdentity> {
    let cert_pem = reply
        .cert_pem
        .as_deref()
        .ok_or_else(|| anyhow!("the server returned no certificate"))?;
    let cert_der = pem::parse(cert_pem.as_bytes())
        .context("decode issued certificate")?
        .into_contents();
    anyhow::ensure!(
        client_tls::issued_by_bundle(&cert_der, &note.ca_pem),
        "the issued certificate does not come from the master key the installer saw"
    );
    let identity = AgentIdentity::from_issued(cert_der, pending.key_der)?;
    let stated = reply
        .device_id
        .as_deref()
        .ok_or_else(|| anyhow!("the server named no device number"))?;
    anyhow::ensure!(
        stated == identity.device_id.0.to_string(),
        "the reply names device {stated} but its certificate carries {}",
        identity.device_id.0
    );
    Ok(identity)
}

/// Saves the identity, the note's master key and the update key, and removes the note, in one
/// commit.
fn save(
    identity: &AgentIdentity,
    reply: &EnrollResponse,
    note: &InstallNote,
    paths: &Paths,
) -> anyhow::Result<()> {
    client_tls::parse_bundle(&note.ca_pem)?;
    let mut commit = Commit::new()
        .write(&paths.server_ca, note.ca_pem.clone())
        .remove(paths.note());
    if let Some(key_hex) = &reply.update_signing_key {
        commit = commit.write(
            paths.data_dir.join(UPDATE_SIGNING_KEY_FILE),
            key_hex.clone(),
        );
    }
    let commit = identity.staged(&paths.data_dir, commit);
    identity_store::commit(&paths.data_dir, commit).context("save the issued identity")
}
