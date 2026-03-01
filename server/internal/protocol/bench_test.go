package protocol

import (
	"bytes"
	"testing"
)

func BenchmarkCodec_WriteFrame(b *testing.B) {
	c := &Codec{}
	payload := make([]byte, 1024)
	for i := range payload {
		payload[i] = byte(i % 256)
	}
	var buf bytes.Buffer

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		buf.Reset()
		_ = c.WriteFrame(&buf, FrameControl, payload)
	}
}

func BenchmarkCodec_ReadFrame(b *testing.B) {
	c := &Codec{}
	payload := make([]byte, 1024)
	for i := range payload {
		payload[i] = byte(i % 256)
	}
	var buf bytes.Buffer
	_ = c.WriteFrame(&buf, FrameControl, payload)
	data := buf.Bytes()

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		r := bytes.NewReader(data)
		_, _, _ = c.ReadFrame(r)
	}
}

func BenchmarkCodec_EncodeControl(b *testing.B) {
	c := &Codec{}
	msg := &ControlMessage{
		Type:         MsgAgentRegister,
		Capabilities: []AgentCapability{CapRemoteDesktop, CapTerminal, CapFileManager},
		Hostname:     "test-host",
		OS:           "linux",
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _ = c.EncodeControl(msg)
	}
}

func BenchmarkCodec_DecodeControl(b *testing.B) {
	c := &Codec{}
	msg := &ControlMessage{
		Type:         MsgAgentRegister,
		Capabilities: []AgentCapability{CapRemoteDesktop, CapTerminal, CapFileManager},
		Hostname:     "test-host",
		OS:           "linux",
	}
	data, _ := c.EncodeControl(msg)

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _ = c.DecodeControl(data)
	}
}

func BenchmarkEncodeServerHello(b *testing.B) {
	var nonce [32]byte
	var certHash [48]byte
	for i := range nonce {
		nonce[i] = byte(i)
	}
	for i := range certHash {
		certHash[i] = byte(i + 32)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_ = EncodeServerHello(nonce, certHash)
	}
}

func BenchmarkDecodeServerHello(b *testing.B) {
	var nonce [32]byte
	var certHash [48]byte
	for i := range nonce {
		nonce[i] = byte(i)
	}
	for i := range certHash {
		certHash[i] = byte(i + 32)
	}
	data := EncodeServerHello(nonce, certHash)

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _, _ = DecodeServerHello(data)
	}
}
