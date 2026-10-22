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

// framedPayload is benchPayload as one control frame on the wire.
func framedPayload(b *testing.B, c *Codec) []byte {
	var buf bytes.Buffer
	if err := c.WriteFrame(&buf, FrameControl, benchPayload()); err != nil {
		b.Fatal(err)
	}
	return buf.Bytes()
}

// encodedRegister is the registration message as the codec encodes it.
func encodedRegister(b *testing.B, c *Codec) []byte {
	data, err := c.EncodeControl(benchRegisterMessage())
	if err != nil {
		b.Fatal(err)
	}
	return data
}

func BenchmarkCodec_WriteFrame(b *testing.B) {
	c := &Codec{}
	payload := benchPayload()
	var buf bytes.Buffer

	b.ReportAllocs()
	for b.Loop() {
		buf.Reset()
		if err := c.WriteFrame(&buf, FrameControl, payload); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCodec_ReadFrame(b *testing.B) {
	c := &Codec{}
	data := framedPayload(b, c)

	b.ReportAllocs()
	for b.Loop() {
		if _, _, err := c.ReadFrame(bytes.NewReader(data)); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCodec_EncodeControl(b *testing.B) {
	c := &Codec{}
	msg := benchRegisterMessage()

	b.ReportAllocs()
	for b.Loop() {
		if _, err := c.EncodeControl(msg); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCodec_DecodeControl(b *testing.B) {
	c := &Codec{}
	data := encodedRegister(b, c)

	b.ReportAllocs()
	for b.Loop() {
		if _, err := c.DecodeControl(data); err != nil {
			b.Fatal(err)
		}
	}
}
