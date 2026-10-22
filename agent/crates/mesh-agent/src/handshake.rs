//! The application handshake on the control stream: a full AgentHello/ServerHello exchange, or a
//! fast-path SkipAuth carrying the cached master-key fingerprint.

use anyhow::{Context, Result};
use sha2::{Digest, Sha384};
use tokio::io::AsyncWriteExt;
use tracing::info;

/// Sends AgentHello first, since the stream opener must write before the peer can accept it.
/// Returns the server CA cert hash from ServerHello, which drives the 0x14 fast path.
pub(crate) async fn perform_full_handshake(
    send: &mut quinn::SendStream,
    recv: &mut quinn::RecvStream,
    cert_der: &[u8],
) -> Result<[u8; 48]> {
    let agent_cert_hash: [u8; 48] = Sha384::digest(cert_der).into();

    let mut nonce = [0u8; 32];
    getrandom::fill(&mut nonce).context("generate nonce")?;

    let agent_hello = mesh_protocol::HandshakeMessage::AgentHello {
        nonce,
        agent_cert_hash,
    };
    send.write_all(&agent_hello.encode_binary())
        .await
        .context("write AgentHello")?;
    send.flush().await.context("flush AgentHello")?;

    // ServerHello is 81 bytes: 1 type + 32 nonce + 48 cert_hash.
    let mut hello_buf = [0u8; 81];
    recv.read_exact(&mut hello_buf)
        .await
        .context("read ServerHello")?;

    let server_hello =
        mesh_protocol::HandshakeMessage::decode_binary(&hello_buf).context("decode ServerHello")?;

    match server_hello {
        mesh_protocol::HandshakeMessage::ServerHello { cert_hash, .. } => {
            info!("handshake complete");
            Ok(cert_hash)
        }
        other => anyhow::bail!("expected ServerHello, got {:?}", other),
    }
}

/// Sends SkipAuth with the cached CA cert hash; the server answers a stale hash by closing with
/// code 2, which sends the next attempt through a full handshake.
pub(crate) async fn perform_fast_handshake(
    send: &mut quinn::SendStream,
    cached_ca_hash: &[u8; 48],
) -> Result<()> {
    let skip_auth = mesh_protocol::HandshakeMessage::SkipAuth {
        cached_cert_hash: *cached_ca_hash,
    };
    send.write_all(&skip_auth.encode_binary())
        .await
        .context("write SkipAuth")?;
    send.flush().await.context("flush SkipAuth")?;
    info!("sent fast-path SkipAuth");
    Ok(())
}
