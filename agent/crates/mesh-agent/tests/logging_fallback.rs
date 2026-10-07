//! The real binary starts with an unusable log directory, losing only the file sink while stdout
//! keeps every line.

use std::fs;
use std::io::{BufRead, BufReader};
use std::path::Path;
use std::process::{Child, Command, Stdio};
use std::sync::mpsc;
use std::thread;
use std::time::{Duration, Instant};

use tempfile::TempDir;

const MESH_AGENT_BIN: &str = env!("CARGO_BIN_EXE_mesh-agent");

/// Logged right after the subscriber is installed; seeing it means the process passed the appender.
const STARTUP_LINE: &str = "mesh-agent starting";

const APPENDER_PANIC: &str = "initializing rolling file appender failed";

const STARTUP_WAIT: Duration = Duration::from_secs(60);

fn spawn_agent(log_dir: &Path, data_dir: &Path, stderr_path: &Path) -> Child {
    let stderr = fs::File::create(stderr_path).expect("create the stderr capture file");
    Command::new(MESH_AGENT_BIN)
        .arg("--server-addr")
        .arg("127.0.0.1:9090")
        .arg("--server-ca")
        .arg(data_dir.join("ca.pem"))
        .env("OPENGATE_LOG_DIR", log_dir)
        .env("OPENGATE_DATA_DIR", data_dir)
        .env("RUST_LOG", "info")
        .stdout(Stdio::piped())
        .stderr(Stdio::from(stderr))
        .spawn()
        .expect("run the mesh-agent binary")
}

fn started_then_stop(child: &mut Child) -> bool {
    let stdout = child.stdout.take().expect("the agent's stdout is piped");
    let (lines, received) = mpsc::channel();
    thread::spawn(move || {
        for line in BufReader::new(stdout).lines().map_while(Result::ok) {
            if lines.send(line).is_err() {
                return;
            }
        }
    });

    let deadline = Instant::now() + STARTUP_WAIT;
    let mut started = false;
    while let Some(remaining) = deadline.checked_duration_since(Instant::now()) {
        match received.recv_timeout(remaining) {
            Ok(line) if line.contains(STARTUP_LINE) => {
                started = true;
                break;
            }
            Ok(_) => continue,
            Err(_) => break,
        }
    }

    drop(child.kill());
    drop(child.wait());
    started
}

#[test]
fn a_log_directory_that_cannot_be_created_does_not_stop_the_agent() {
    let tmp = TempDir::new().expect("a temp dir");
    // No directory can be created under a regular file, for root or any other user.
    let blocker = tmp.path().join("not-a-directory");
    fs::write(&blocker, b"").expect("write the blocking file");
    let log_dir = blocker.join("logs");

    let data_dir = tmp.path().join("data");
    fs::create_dir_all(&data_dir).expect("create the data dir");
    let stderr_path = tmp.path().join("stderr.txt");

    let mut child = spawn_agent(&log_dir, &data_dir, &stderr_path);
    let started = started_then_stop(&mut child);
    let stderr = fs::read_to_string(&stderr_path).unwrap_or_default();

    assert!(
        !stderr.contains(APPENDER_PANIC),
        "the agent died on a log directory it could not create instead of \
         dropping the file sink. stderr: {stderr}"
    );
    assert!(
        started,
        "the agent never reached its startup line. stderr: {stderr}"
    );
}

#[test]
fn a_writable_log_directory_receives_the_rolling_file() {
    let tmp = TempDir::new().expect("a temp dir");
    let log_dir = tmp.path().join("logs");
    let data_dir = tmp.path().join("data");
    fs::create_dir_all(&data_dir).expect("create the data dir");
    let stderr_path = tmp.path().join("stderr.txt");

    let mut child = spawn_agent(&log_dir, &data_dir, &stderr_path);
    let started = started_then_stop(&mut child);
    let stderr = fs::read_to_string(&stderr_path).unwrap_or_default();
    assert!(
        started,
        "the agent never reached its startup line. stderr: {stderr}"
    );

    // `agent.log` is the prefix the device-log collector discovers files by.
    let written: Vec<String> = fs::read_dir(&log_dir)
        .expect("the agent creates the log directory it is given")
        .filter_map(Result::ok)
        .map(|entry| entry.file_name().to_string_lossy().into_owned())
        .filter(|name| name.starts_with("agent.log"))
        .collect();

    assert!(
        !written.is_empty(),
        "no agent.log file under {} — the agent did not honour the log \
         directory it was given",
        log_dir.display()
    );
}
