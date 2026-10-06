package protocol

import (
	"bytes"
	"testing"
)

func benchPayload() []byte {
	payload := make([]byte, 1024)
	for i := range payload {
		payload[i] = byte(i % 256)
	}
	return payload
}

func benchRegisterMessage() *ControlMessage {
	return &ControlMessage{
		Type:         MsgAgentRegister,
		Capabilities: []AgentCapability{CapRemoteDesktop, CapTerminal, CapFileManager},
		Hostname:     "test-host",
		OS:           "linux",
		Arch:         "amd64",
		Version:      "0.1.0",
	}
}

func benchHelloInputs() (nonce [32]byte, certHash [48]byte) {
	for i := range nonce {
		nonce[i] = byte(i)
	}
	for i := range certHash {
		certHash[i] = byte(i + 32)
	}
	return nonce, certHash
}

func runBench(b *testing.B, op func()) {
	b.Helper()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		op()
	}
}

func BenchmarkCodec_WriteFrame(b *testing.B) {
	c := &Codec{}
	payload := benchPayload()
	var buf bytes.Buffer

	runBench(b, func() {
		buf.Reset()
		_ = c.WriteFrame(&buf, FrameControl, payload)
	})
}

func BenchmarkCodec_ReadFrame(b *testing.B) {
	c := &Codec{}
	var buf bytes.Buffer
	_ = c.WriteFrame(&buf, FrameControl, benchPayload())
	data := buf.Bytes()

	runBench(b, func() { _, _, _ = c.ReadFrame(bytes.NewReader(data)) })
}

func BenchmarkCodec_EncodeControl(b *testing.B) {
	c := &Codec{}
	msg := benchRegisterMessage()

	runBench(b, func() { _, _ = c.EncodeControl(msg) })
}

func BenchmarkCodec_DecodeControl(b *testing.B) {
	c := &Codec{}
	data, _ := c.EncodeControl(benchRegisterMessage())

	runBench(b, func() { _, _ = c.DecodeControl(data) })
}

func BenchmarkEncodeServerHello(b *testing.B) {
	nonce, certHash := benchHelloInputs()

	runBench(b, func() { _ = EncodeServerHello(nonce, certHash) })
}

func BenchmarkDecodeServerHello(b *testing.B) {
	nonce, certHash := benchHelloInputs()
	data := EncodeServerHello(nonce, certHash)

	runBench(b, func() { _, _, _ = DecodeServerHello(data) })
}
