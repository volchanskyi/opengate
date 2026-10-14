//! The direct link's per-frame work on both ends: the host cutting a frame into pieces it keeps
//! until confirmed, and the technician reassembling, delivering and confirming it.

use criterion::{criterion_group, criterion_main, BatchSize, Criterion};
use mesh_protocol::direct::{Config, Handoff, Instruction};
use mesh_protocol::{FileFrame, Frame, TerminalFrame};
use std::hint::black_box;

/// The bytes of a file piece, as the host reads a file for a download.
const FILE_PIECE: usize = 256 * 1024;

/// A host and a technician hand-off joined over an open link, both switch notices exchanged.
struct LinkedPair {
    host: Handoff,
    technician: Handoff,
    now: u64,
}

impl LinkedPair {
    fn new() -> Self {
        let mut host = Handoff::new(Config::default());
        let mut technician = Handoff::new(Config::default());
        host.begin_attempt(0);
        technician.begin_attempt(0);
        for notice in relay_frames(host.link_open(0)) {
            technician.from_relay(notice, 0);
        }
        for notice in relay_frames(technician.link_open(0)) {
            host.from_relay(notice, 0);
        }
        Self {
            host,
            technician,
            now: 0,
        }
    }

    /// Carries one session frame from the host to the technician, and any confirmation back;
    /// returns the bytes delivered.
    fn carry(&mut self, frame: Vec<u8>) -> usize {
        self.now += 1;
        let mut delivered = 0;
        for instruction in self.host.send(frame, self.now) {
            let Instruction::Link(piece) = instruction else {
                continue;
            };
            for reply in self.technician.from_link(&piece, self.now) {
                match reply {
                    Instruction::Deliver(frame) => delivered += frame.len(),
                    Instruction::Link(confirmation) => {
                        self.host.from_link(&confirmation, self.now);
                    }
                    _ => {}
                }
            }
        }
        delivered
    }
}

fn relay_frames(instructions: Vec<Instruction>) -> impl Iterator<Item = Vec<u8>> {
    instructions
        .into_iter()
        .filter_map(|instruction| match instruction {
            Instruction::Relay(frame) => Some(frame),
            _ => None,
        })
}

fn bench_direct_link(c: &mut Criterion) {
    let echo = Frame::Terminal(TerminalFrame {
        data: b"ls -la\r\n".to_vec(),
    })
    .encode()
    .expect("a terminal frame encodes");
    let piece = Frame::FileTransfer(FileFrame {
        offset: 0,
        total_size: 64 * FILE_PIECE as u64,
        data: vec![0x5a; FILE_PIECE],
    })
    .encode()
    .expect("a file piece encodes");

    for (name, frame) in [
        ("direct_link_terminal_echo", echo),
        ("direct_link_file_piece", piece),
    ] {
        let mut pair = LinkedPair::new();
        c.bench_function(name, |b| {
            b.iter_batched(
                || frame.clone(),
                |frame| black_box(pair.carry(frame)),
                BatchSize::SmallInput,
            )
        });
    }
}

criterion_group!(benches, bench_direct_link);
criterion_main!(benches);
