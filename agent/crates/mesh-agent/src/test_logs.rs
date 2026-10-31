//! The log lines a test's code writes, captured from the current thread.

use std::io::Write;
use std::sync::{Arc, Mutex};

#[derive(Clone)]
struct Lines(Arc<Mutex<Vec<u8>>>);

impl Write for Lines {
    fn write(&mut self, buf: &[u8]) -> std::io::Result<usize> {
        self.0.lock().expect("log buffer").extend_from_slice(buf);
        Ok(buf.len())
    }

    fn flush(&mut self) -> std::io::Result<()> {
        Ok(())
    }
}

/// Every line `f` logs at the formatter's default level and above, without colour.
pub(crate) fn log_lines(f: impl FnOnce()) -> String {
    let lines = Lines(Arc::new(Mutex::new(Vec::new())));
    let writer = lines.clone();
    let subscriber = tracing_subscriber::fmt()
        .with_writer(move || writer.clone())
        .with_ansi(false)
        .finish();
    tracing::subscriber::with_default(subscriber, f);
    let captured = lines.0.lock().expect("log buffer").clone();
    String::from_utf8_lossy(&captured).into_owned()
}
