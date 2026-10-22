//! A stand-in server, enrollment endpoint and service manager that drive the built agent binary.

#![allow(dead_code)] // each test binary compiles this module and uses a different part of it

use std::path::{Path, PathBuf};
use std::process::Stdio;
use std::sync::{Arc, Mutex};
use std::time::Duration;

use mesh_protocol::{ControlMessage, HandshakeMessage};
use sha2::{Digest, Sha384};
use tokio::io::{AsyncBufReadExt, BufReader};

/// How long a step of a scenario may take before the test fails.
pub const STEP: Duration = Duration::from_secs(30);

/// A master key and the server certificate it signed.
pub struct Pki {
    issuer: rcgen::Issuer<'static, rcgen::KeyPair>,
    pub ca_pem: String,
    pub ca_der: Vec<u8>,
    pub server_cert_der: Vec<u8>,
    pub server_key_der: Vec<u8>,
}

impl Pki {
    pub fn new(name: &str) -> Arc<Self> {
        let ca_key = rcgen::KeyPair::generate_for(&rcgen::PKCS_ECDSA_P256_SHA256).unwrap();
        let mut params = rcgen::CertificateParams::new(Vec::<String>::new()).unwrap();
        params.is_ca = rcgen::IsCa::Ca(rcgen::BasicConstraints::Unconstrained);
        params
            .distinguished_name
            .push(rcgen::DnType::CommonName, name.to_string());
        let ca_cert = params.clone().self_signed(&ca_key).unwrap();
        let issuer = rcgen::Issuer::new(params, ca_key);

        let server_key = rcgen::KeyPair::generate_for(&rcgen::PKCS_ECDSA_P256_SHA256).unwrap();
        let server_params =
            rcgen::CertificateParams::new(vec!["127.0.0.1".into(), "localhost".into()]).unwrap();
        let server_cert = server_params.signed_by(&server_key, &issuer).unwrap();

        Arc::new(Self {
            issuer,
            ca_pem: ca_cert.pem(),
            ca_der: ca_cert.der().to_vec(),
            server_cert_der: server_cert.der().to_vec(),
            server_key_der: server_key.serialize_der(),
        })
    }

    /// Signs a request with a device number the server chose, ignoring the subject it names.
    pub fn sign_csr(&self, csr_pem: &str, device_id: uuid::Uuid) -> String {
        use x509_parser::prelude::FromDer;
        let der = pem::parse(csr_pem).unwrap().into_contents();
        let (_, csr) =
            x509_parser::certification_request::X509CertificationRequest::from_der(&der).unwrap();
        let key = RequestedKey(
            csr.certification_request_info
                .subject_pki
                .subject_public_key
                .data
                .to_vec(),
        );
        let mut params = rcgen::CertificateParams::new(Vec::<String>::new()).unwrap();
        params
            .distinguished_name
            .push(rcgen::DnType::CommonName, device_id.to_string());
        params.signed_by(&key, &self.issuer).unwrap().pem()
    }

    /// An identity this master key issued a year ago, which expired six months later.
    pub fn expired_identity(&self, device_id: uuid::Uuid) -> (Vec<u8>, Vec<u8>) {
        let key = rcgen::KeyPair::generate_for(&rcgen::PKCS_ECDSA_P256_SHA256).unwrap();
        let mut params = rcgen::CertificateParams::new(Vec::<String>::new()).unwrap();
        params
            .distinguished_name
            .push(rcgen::DnType::CommonName, device_id.to_string());
        let now = std::time::SystemTime::now();
        params.not_before = (now - Duration::from_secs(365 * 86400)).into();
        params.not_after = (now - Duration::from_secs(180 * 86400)).into();
        let cert = params.signed_by(&key, &self.issuer).unwrap();
        (cert.der().to_vec(), key.serialize_der())
    }

    /// An identity issued by this master key: certificate DER and PKCS#8 key DER.
    pub fn identity(&self, device_id: uuid::Uuid) -> (Vec<u8>, Vec<u8>) {
        let key = rcgen::KeyPair::generate_for(&rcgen::PKCS_ECDSA_P256_SHA256).unwrap();
        let mut params = rcgen::CertificateParams::new(Vec::<String>::new()).unwrap();
        params
            .distinguished_name
            .push(rcgen::DnType::CommonName, device_id.to_string());
        let cert = params.signed_by(&key, &self.issuer).unwrap();
        (cert.der().to_vec(), key.serialize_der())
    }
}

/// The P-256 public key a signing request carries, as rcgen signs for it.
struct RequestedKey(Vec<u8>);

impl rcgen::PublicKeyData for RequestedKey {
    fn der_bytes(&self) -> &[u8] {
        &self.0
    }

    fn algorithm(&self) -> &'static rcgen::SignatureAlgorithm {
        &rcgen::PKCS_ECDSA_P256_SHA256
    }
}

/// The server side of the agent's QUIC connection, as the test scripts it.
pub struct FakeServer {
    endpoint: quinn::Endpoint,
    pub addr: std::net::SocketAddr,
    pub pki: Arc<Pki>,
}

impl FakeServer {
    pub fn start(pki: Arc<Pki>) -> Self {
        let mut roots = rustls::RootCertStore::empty();
        roots
            .add(rustls::pki_types::CertificateDer::from(pki.ca_der.clone()))
            .unwrap();
        let verifier = rustls::server::WebPkiClientVerifier::builder(Arc::new(roots))
            .build()
            .unwrap();
        Self::start_with(pki, verifier)
    }

    /// A server that lets any client certificate finish TLS, as the product's does before it
    /// classifies an expired one.
    pub fn start_admitting_any(pki: Arc<Pki>) -> Self {
        Self::start_with(pki, Arc::new(AdmitAny))
    }

    fn start_with(
        pki: Arc<Pki>,
        verifier: Arc<dyn rustls::server::danger::ClientCertVerifier>,
    ) -> Self {
        let mut tls = rustls::ServerConfig::builder()
            .with_client_cert_verifier(verifier)
            .with_single_cert(
                vec![rustls::pki_types::CertificateDer::from(
                    pki.server_cert_der.clone(),
                )],
                rustls::pki_types::PrivateKeyDer::Pkcs8(
                    rustls::pki_types::PrivatePkcs8KeyDer::from(pki.server_key_der.clone()),
                ),
            )
            .unwrap();
        tls.alpn_protocols = vec![b"opengate".to_vec()];
        let crypto = quinn::crypto::rustls::QuicServerConfig::try_from(tls).unwrap();
        let endpoint = quinn::Endpoint::server(
            quinn::ServerConfig::with_crypto(Arc::new(crypto)),
            "127.0.0.1:0".parse().unwrap(),
        )
        .unwrap();
        let addr = endpoint.local_addr().unwrap();
        Self {
            endpoint,
            addr,
            pki,
        }
    }

    /// Accepts the agent's next connection and completes the handshake it opens with.
    pub async fn accept(&self) -> ServerSide {
        tokio::time::timeout(STEP, self.accept_inner())
            .await
            .expect("the agent never connected")
    }

    /// Accepts the next connection attempt, which may fail its TLS handshake.
    pub async fn accept_attempt(&self) -> Result<quinn::Connection, quinn::ConnectionError> {
        let incoming = tokio::time::timeout(STEP, self.endpoint.accept())
            .await
            .expect("the agent never tried to connect")
            .expect("endpoint closed");
        incoming.await
    }

    async fn accept_inner(&self) -> ServerSide {
        let conn = self
            .endpoint
            .accept()
            .await
            .expect("endpoint closed")
            .await
            .expect("the TLS handshake failed");
        let (mut send, mut recv) = conn.accept_bi().await.expect("control stream");
        let mut kind = [0u8; 1];
        recv.read_exact(&mut kind).await.expect("handshake type");
        let fast_path = match kind[0] {
            0x14 => {
                let mut cached = [0u8; 48];
                recv.read_exact(&mut cached).await.expect("SkipAuth");
                true
            }
            _ => {
                let mut rest = [0u8; 80];
                recv.read_exact(&mut rest).await.expect("AgentHello");
                let hash: [u8; 48] = Sha384::digest(&self.pki.ca_der).into();
                let hello = HandshakeMessage::ServerHello {
                    nonce: [7u8; 32],
                    cert_hash: hash,
                };
                send.write_all(&hello.encode_binary())
                    .await
                    .expect("ServerHello");
                false
            }
        };
        ServerSide {
            conn,
            send,
            recv,
            fast_path,
            pongs: 0,
        }
    }
}

/// A client verifier that checks the handshake signature and accepts any certificate.
#[derive(Debug)]
struct AdmitAny;

impl rustls::server::danger::ClientCertVerifier for AdmitAny {
    fn root_hint_subjects(&self) -> &[rustls::DistinguishedName] {
        &[]
    }

    fn verify_client_cert(
        &self,
        _end_entity: &rustls::pki_types::CertificateDer<'_>,
        _intermediates: &[rustls::pki_types::CertificateDer<'_>],
        _now: rustls::pki_types::UnixTime,
    ) -> Result<rustls::server::danger::ClientCertVerified, rustls::Error> {
        Ok(rustls::server::danger::ClientCertVerified::assertion())
    }

    fn verify_tls12_signature(
        &self,
        message: &[u8],
        cert: &rustls::pki_types::CertificateDer<'_>,
        dss: &rustls::DigitallySignedStruct,
    ) -> Result<rustls::client::danger::HandshakeSignatureValid, rustls::Error> {
        let algorithms = rustls::crypto::ring::default_provider().signature_verification_algorithms;
        rustls::crypto::verify_tls12_signature(message, cert, dss, &algorithms)
    }

    fn verify_tls13_signature(
        &self,
        message: &[u8],
        cert: &rustls::pki_types::CertificateDer<'_>,
        dss: &rustls::DigitallySignedStruct,
    ) -> Result<rustls::client::danger::HandshakeSignatureValid, rustls::Error> {
        let algorithms = rustls::crypto::ring::default_provider().signature_verification_algorithms;
        rustls::crypto::verify_tls13_signature(message, cert, dss, &algorithms)
    }

    fn supported_verify_schemes(&self) -> Vec<rustls::SignatureScheme> {
        rustls::crypto::ring::default_provider()
            .signature_verification_algorithms
            .supported_schemes()
    }
}

/// One accepted connection: its control stream and how the agent opened it.
pub struct ServerSide {
    pub conn: quinn::Connection,
    send: quinn::SendStream,
    recv: quinn::RecvStream,
    pub fast_path: bool,
    /// Pongs the agent sent back, counted as the reader passes them.
    pub pongs: usize,
}

impl ServerSide {
    pub async fn read(&mut self) -> ControlMessage {
        tokio::time::timeout(STEP, self.read_frame())
            .await
            .expect("the agent sent nothing")
    }

    async fn read_frame(&mut self) -> ControlMessage {
        loop {
            let mut kind = [0u8; 1];
            self.recv.read_exact(&mut kind).await.expect("frame type");
            if kind[0] == mesh_protocol::codec::FRAME_PONG {
                self.pongs += 1;
                continue;
            }
            assert_eq!(
                kind[0],
                mesh_protocol::codec::FRAME_CONTROL,
                "a control frame"
            );
            let mut len = [0u8; 4];
            self.recv.read_exact(&mut len).await.expect("frame length");
            let mut payload = vec![0u8; u32::from_be_bytes(len) as usize];
            self.recv
                .read_exact(&mut payload)
                .await
                .expect("frame payload");
            return rmp_serde::from_slice(&payload).expect("decode control");
        }
    }

    /// Reads until the agent's registration arrives.
    pub async fn expect_register(&mut self) {
        loop {
            if let ControlMessage::AgentRegister { .. } = self.read().await {
                return;
            }
        }
    }

    pub async fn send(&mut self, msg: ControlMessage) {
        let bytes = mesh_protocol::Frame::Control(msg)
            .encode()
            .expect("encode control");
        self.send.write_all(&bytes).await.expect("send control");
    }

    /// Accepts the identity, which is the acknowledgement the agent waits for.
    pub async fn acknowledge(&mut self, ca_pem: &str) {
        self.send(ControlMessage::TrustBundle {
            ca_bundle: ca_pem.to_string(),
        })
        .await;
    }

    /// The certificate the agent presented.
    pub fn presented(&self) -> Vec<u8> {
        self.conn
            .peer_identity()
            .and_then(|any| {
                any.downcast::<Vec<rustls::pki_types::CertificateDer<'static>>>()
                    .ok()
            })
            .and_then(|chain| chain.first().map(|c| c.to_vec()))
            .expect("a client certificate")
    }

    /// Sends a ping frame, which the agent answers with a pong.
    pub async fn ping(&mut self) {
        let bytes = mesh_protocol::Frame::Ping.encode().expect("encode ping");
        self.send.write_all(&bytes).await.expect("send ping");
    }

    pub fn close(&self, code: u32) {
        self.conn.close(code.into(), b"closed by the test");
    }

    /// Waits for the agent to hang up.
    pub async fn closed(&self) -> quinn::ConnectionError {
        tokio::time::timeout(STEP, self.conn.closed())
            .await
            .expect("the agent never hung up")
    }
}

/// The enrollment endpoint: issues certificates with numbers it chooses, or refuses a token.
pub struct EnrollStub {
    server: mockito::ServerGuard,
    pub issued: Arc<Mutex<Vec<uuid::Uuid>>>,
}

impl EnrollStub {
    pub async fn start(pki: Arc<Pki>, token: &str) -> Self {
        let mut server = mockito::Server::new_async().await;
        let issued = Arc::new(Mutex::new(Vec::new()));
        let record = issued.clone();
        let signer = pki.clone();
        server
            .mock("POST", format!("/api/v1/enroll/{token}").as_str())
            .with_status(200)
            .with_header("content-type", "application/json")
            .with_body_from_request(move |req| {
                let body: serde_json::Value = serde_json::from_slice(req.body().unwrap()).unwrap();
                let id = uuid::Uuid::new_v4();
                let cert_pem = signer.sign_csr(body["csr_pem"].as_str().unwrap(), id);
                record.lock().unwrap().push(id);
                serde_json::json!({
                    "ca_pem": signer.ca_pem,
                    "cert_pem": cert_pem,
                    "device_id": id.to_string(),
                    "server_addr": "127.0.0.1:1",
                    "server_domain": "127.0.0.1",
                })
                .to_string()
                .into_bytes()
            })
            .create_async()
            .await;
        let mismatch = pki.clone();
        server
            .mock("POST", "/api/v1/enroll/mismatch-token")
            .with_status(200)
            .with_body_from_request(move |req| {
                let body: serde_json::Value = serde_json::from_slice(req.body().unwrap()).unwrap();
                let cert_pem =
                    mismatch.sign_csr(body["csr_pem"].as_str().unwrap(), uuid::Uuid::new_v4());
                serde_json::json!({
                    "ca_pem": mismatch.ca_pem,
                    "cert_pem": cert_pem,
                    "device_id": uuid::Uuid::new_v4().to_string(),
                    "server_addr": "127.0.0.1:1",
                    "server_domain": "127.0.0.1",
                })
                .to_string()
                .into_bytes()
            })
            .create_async()
            .await;
        server
            .mock("POST", "/api/v1/enroll/refused-token")
            .with_status(410)
            .with_body(r#"{"error":"enrollment token expired"}"#)
            .create_async()
            .await;
        Self { server, issued }
    }

    pub fn url(&self) -> String {
        self.server.url()
    }

    pub fn issued(&self) -> Vec<uuid::Uuid> {
        self.issued.lock().unwrap().clone()
    }
}

/// A machine's folders, its service manager stand-in, and a hard link to the agent binary.
pub struct MachineBox {
    _root: tempfile::TempDir,
    pub data: PathBuf,
    pub trust: PathBuf,
    pub ca_path: PathBuf,
    pub logs: PathBuf,
    pub bin: PathBuf,
    pub exe: PathBuf,
    pub unit: PathBuf,
    pub unit_state: PathBuf,
    pub trace: PathBuf,
}

/// The fake `systemctl`: records every call, answers where the unit file is and whether it is
/// enabled, and records a disable, which a `block-disable` file holds back as a kill would.
const FAKE_SYSTEMCTL: &str = r#"#!/usr/bin/env bash
printf '%s\n' "$*" >>"$FAKE_SYSTEMCTL_TRACE"
case "$1" in
  show)
    if [ -e "$FAKE_UNIT_PATH" ]; then printf '%s\n' "$FAKE_UNIT_PATH"; fi
    ;;
  is-enabled)
    cat "$FAKE_UNIT_STATE"
    ;;
  disable)
    if [ -e "$(dirname "$0")/block-disable" ]; then
      while [ -e "$(dirname "$0")/block-disable" ]; do sleep 0.1; done
      exit 0
    fi
    printf 'disabled\n' >"$FAKE_UNIT_STATE"
    ;;
esac
exit 0
"#;

impl MachineBox {
    pub fn new() -> Self {
        let root = tempfile::tempdir_in(env!("CARGO_TARGET_TMPDIR")).unwrap();
        let data = root.path().join("var/lib/opengate-agent");
        let trust = root.path().join("etc/opengate-agent");
        let bin = root.path().join("usr/local/bin");
        let systemd = root.path().join("etc/systemd/system");
        for dir in [&data, &trust, &bin, &systemd] {
            std::fs::create_dir_all(dir).unwrap();
        }
        let exe = bin.join("mesh-agent");
        std::fs::hard_link(env!("CARGO_BIN_EXE_mesh-agent"), &exe).unwrap();
        let fake = bin.join("systemctl");
        std::fs::write(&fake, FAKE_SYSTEMCTL).unwrap();
        set_executable(&fake);
        let unit = systemd.join("mesh-agent.service");
        std::fs::write(&unit, "[Service]\n").unwrap();
        let unit_state = root.path().join("unit-state");
        std::fs::write(&unit_state, "enabled\n").unwrap();
        Self {
            ca_path: trust.join("ca.pem"),
            logs: root.path().join("var/log/mesh-agent"),
            trace: root.path().join("systemctl.trace"),
            _root: root,
            data,
            trust,
            bin,
            exe,
            unit,
            unit_state,
        }
    }

    /// Puts a stand-in for a system tool on the agent's PATH.
    pub fn install_tool(&self, name: &str, script: &str) {
        let path = self.bin.join(name);
        std::fs::write(&path, script).unwrap();
        set_executable(&path);
    }

    pub fn note_path(&self) -> PathBuf {
        self.data.join("install-note.json")
    }

    pub fn write_note(&self, token: &str, url: &str, ca_pem: &str, fresh: bool) {
        let note =
            serde_json::json!({"token": token, "url": url, "ca_pem": ca_pem, "fresh": fresh});
        std::fs::write(self.note_path(), note.to_string()).unwrap();
    }

    /// Saves an identity and the master key that issued it, as a past enrollment would have.
    pub fn write_identity(&self, pki: &Pki, device_id: uuid::Uuid) {
        let (cert, key) = pki.identity(device_id);
        std::fs::write(self.data.join("device_id.txt"), device_id.to_string()).unwrap();
        std::fs::write(self.data.join("agent.crt"), cert).unwrap();
        std::fs::write(self.data.join("agent.key"), key).unwrap();
        std::fs::write(&self.ca_path, &pki.ca_pem).unwrap();
    }

    pub fn device_id(&self) -> Option<uuid::Uuid> {
        std::fs::read_to_string(self.data.join("device_id.txt"))
            .ok()
            .and_then(|s| s.trim().parse().ok())
    }

    pub fn has_identity(&self) -> bool {
        ["device_id.txt", "agent.crt", "agent.key"]
            .iter()
            .any(|f| self.data.join(f).exists())
    }

    pub fn trace(&self) -> String {
        std::fs::read_to_string(&self.trace).unwrap_or_default()
    }

    pub fn unit_enabled(&self) -> bool {
        std::fs::read_to_string(&self.unit_state)
            .unwrap_or_default()
            .trim()
            == "enabled"
    }

    /// Starts the agent and restarts it on exit code 42, as its unit does.
    pub fn spawn(&self, server: std::net::SocketAddr) -> AgentProcess {
        let path = format!("{}:{}", self.bin.display(), std::env::var("PATH").unwrap());
        let mut command = tokio::process::Command::new(&self.exe);
        command
            .args(["--server-addr", &server.to_string()])
            .arg("--server-ca")
            .arg(&self.ca_path)
            .arg("--data-dir")
            .arg(&self.data)
            .arg("--log-dir")
            .arg(&self.logs)
            .env("PATH", path)
            .env("RUST_LOG", "info")
            .env("FAKE_SYSTEMCTL_TRACE", &self.trace)
            .env("FAKE_UNIT_PATH", &self.unit)
            .env("FAKE_UNIT_STATE", &self.unit_state)
            .stdout(Stdio::piped())
            .stderr(Stdio::piped())
            .kill_on_drop(true);
        AgentProcess::supervise(command)
    }
}

#[cfg(unix)]
fn set_executable(path: &Path) {
    use std::os::unix::fs::PermissionsExt;
    std::fs::set_permissions(path, std::fs::Permissions::from_mode(0o755)).unwrap();
}

/// A running agent, restarted on exit code 42, and everything it has logged.
pub struct AgentProcess {
    log: Arc<Mutex<String>>,
    exit: Arc<Mutex<Option<Option<i32>>>>,
    pid: Arc<Mutex<Option<u32>>>,
    stop: Arc<std::sync::atomic::AtomicBool>,
    pub restarts: Arc<std::sync::atomic::AtomicU32>,
}

impl AgentProcess {
    fn supervise(mut command: tokio::process::Command) -> Self {
        let process = Self {
            log: Arc::default(),
            exit: Arc::default(),
            pid: Arc::default(),
            stop: Arc::default(),
            restarts: Arc::default(),
        };
        let (log, exit, pid, stop, restarts) = (
            process.log.clone(),
            process.exit.clone(),
            process.pid.clone(),
            process.stop.clone(),
            process.restarts.clone(),
        );
        tokio::spawn(async move {
            loop {
                let mut child = command.spawn().expect("spawn the agent");
                *pid.lock().unwrap() = child.id();
                capture(child.stdout.take(), log.clone());
                capture(child.stderr.take(), log.clone());
                let code = child.wait().await.ok().and_then(|s| s.code());
                if code == Some(42) && !stop.load(std::sync::atomic::Ordering::SeqCst) {
                    restarts.fetch_add(1, std::sync::atomic::Ordering::SeqCst);
                    continue;
                }
                *exit.lock().unwrap() = Some(code);
                return;
            }
        });
        process
    }

    pub fn log(&self) -> String {
        self.log.lock().unwrap().clone()
    }

    /// Waits for the agent to exit for good and returns its exit code.
    pub async fn exit_code(&mut self) -> Option<i32> {
        let deadline = tokio::time::Instant::now() + STEP;
        loop {
            if let Some(code) = *self.exit.lock().unwrap() {
                return code;
            }
            assert!(
                tokio::time::Instant::now() < deadline,
                "the agent did not exit; log:\n{}",
                self.log()
            );
            tokio::time::sleep(Duration::from_millis(50)).await;
        }
    }

    /// Waits until the log holds `needle`.
    pub async fn wait_for_log(&self, needle: &str) {
        let deadline = tokio::time::Instant::now() + STEP;
        while !self.log().contains(needle) {
            assert!(
                tokio::time::Instant::now() < deadline,
                "the agent never logged {needle:?}; log:\n{}",
                self.log()
            );
            tokio::time::sleep(Duration::from_millis(50)).await;
        }
    }

    /// Asks the agent to stop, as the service manager does, and stops restarting it.
    pub fn terminate(&self) {
        self.stop.store(true, std::sync::atomic::Ordering::SeqCst);
        if let Some(pid) = *self.pid.lock().unwrap() {
            // SAFETY: kill(2) on a pid this test spawned; a pid that already exited is refused.
            unsafe {
                libc::kill(pid as libc::pid_t, libc::SIGTERM);
            }
        }
    }

    /// Waits up to `limit` for the agent to exit for good, returning `None` while it still runs.
    pub async fn exited_within(&self, limit: Duration) -> Option<Option<i32>> {
        let deadline = tokio::time::Instant::now() + limit;
        loop {
            if let Some(code) = *self.exit.lock().unwrap() {
                return Some(code);
            }
            if tokio::time::Instant::now() >= deadline {
                return None;
            }
            tokio::time::sleep(Duration::from_millis(20)).await;
        }
    }

    /// Stops the agent as the service manager does, killing it when it outlasts a step; a clean
    /// exit is what writes the run's coverage profile.
    pub async fn stop(&mut self) {
        self.terminate();
        if self.exited_within(STEP).await.is_none() {
            self.kill().await;
        }
    }

    /// Kills the agent, as SIGKILL or a power cut would, and stops restarting it.
    pub async fn kill(&mut self) {
        self.stop.store(true, std::sync::atomic::Ordering::SeqCst);
        if let Some(pid) = *self.pid.lock().unwrap() {
            // SAFETY: kill(2) on a pid this test spawned; a pid that already exited is refused.
            unsafe {
                libc::kill(pid as libc::pid_t, libc::SIGKILL);
            }
        }
        let deadline = tokio::time::Instant::now() + STEP;
        while self.exit.lock().unwrap().is_none() && tokio::time::Instant::now() < deadline {
            tokio::time::sleep(Duration::from_millis(20)).await;
        }
    }
}

impl Drop for AgentProcess {
    fn drop(&mut self) {
        self.stop.store(true, std::sync::atomic::Ordering::SeqCst);
        if self.exit.lock().map(|e| e.is_none()).unwrap_or(false) {
            if let Some(pid) = *self.pid.lock().unwrap() {
                // SAFETY: kill(2) on a pid this test spawned and has not reaped.
                unsafe {
                    libc::kill(pid as libc::pid_t, libc::SIGKILL);
                }
            }
        }
    }
}

fn capture(
    pipe: Option<impl tokio::io::AsyncRead + Send + Unpin + 'static>,
    log: Arc<Mutex<String>>,
) {
    let Some(pipe) = pipe else {
        return;
    };
    tokio::spawn(async move {
        let mut lines = BufReader::new(pipe).lines();
        while let Ok(Some(line)) = lines.next_line().await {
            let mut log = log.lock().unwrap();
            log.push_str(&line);
            log.push('\n');
        }
    });
}
