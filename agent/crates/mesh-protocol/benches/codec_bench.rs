use criterion::{black_box, criterion_group, criterion_main, Criterion};
use mesh_protocol::{AgentCapability, ControlMessage, Frame, HandshakeMessage};

fn bench_frame_encode_control(c: &mut Criterion) {
    let msg = ControlMessage::AgentRegister {
        capabilities: vec![
            AgentCapability::RemoteDesktop,
            AgentCapability::Terminal,
            AgentCapability::FileManager,
        ],
        hostname: "bench-host".to_string(),
        os: "linux".to_string(),
    };
    let frame = Frame::Control(msg);

    c.bench_function("frame_encode_control", |b| {
        b.iter(|| black_box(black_box(&frame).encode().unwrap()))
    });
}

fn bench_frame_decode_control(c: &mut Criterion) {
    let msg = ControlMessage::AgentRegister {
        capabilities: vec![
            AgentCapability::RemoteDesktop,
            AgentCapability::Terminal,
            AgentCapability::FileManager,
        ],
        hostname: "bench-host".to_string(),
        os: "linux".to_string(),
    };
    let frame = Frame::Control(msg);
    let encoded = frame.encode().unwrap();

    c.bench_function("frame_decode_control", |b| {
        b.iter(|| black_box(Frame::decode(black_box(&encoded)).unwrap()))
    });
}

fn bench_frame_encode_ping(c: &mut Criterion) {
    let frame = Frame::Ping;

    c.bench_function("frame_encode_ping", |b| {
        b.iter(|| black_box(black_box(&frame).encode().unwrap()))
    });
}

fn bench_encode_server_hello(c: &mut Criterion) {
    let nonce = [0xABu8; 32];
    let cert_hash = [0xCDu8; 48];
    let msg = HandshakeMessage::ServerHello { nonce, cert_hash };

    c.bench_function("encode_server_hello", |b| {
        b.iter(|| black_box(black_box(&msg).encode_binary()))
    });
}

fn bench_decode_server_hello(c: &mut Criterion) {
    let nonce = [0xABu8; 32];
    let cert_hash = [0xCDu8; 48];
    let msg = HandshakeMessage::ServerHello { nonce, cert_hash };
    let encoded = msg.encode_binary();

    c.bench_function("decode_server_hello", |b| {
        b.iter(|| black_box(HandshakeMessage::decode_binary(black_box(&encoded)).unwrap()))
    });
}

criterion_group!(
    benches,
    bench_frame_encode_control,
    bench_frame_decode_control,
    bench_frame_encode_ping,
    bench_encode_server_hello,
    bench_decode_server_hello,
);
criterion_main!(benches);
