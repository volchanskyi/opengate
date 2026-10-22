package protocol

import (
	"testing"
)

func benchHelloInputs() (nonce [32]byte, certHash [48]byte) {
	for i := range nonce {
		nonce[i] = byte(i)
	}
	for i := range certHash {
		certHash[i] = byte(i + 32)
	}
	return nonce, certHash
}

func BenchmarkEncodeServerHello(b *testing.B) {
	nonce, certHash := benchHelloInputs()

	b.ReportAllocs()
	for b.Loop() {
		EncodeServerHello(nonce, certHash)
	}
}

func BenchmarkDecodeServerHello(b *testing.B) {
	nonce, certHash := benchHelloInputs()
	data := EncodeServerHello(nonce, certHash)

	b.ReportAllocs()
	for b.Loop() {
		if _, _, err := DecodeServerHello(data); err != nil {
			b.Fatal(err)
		}
	}
}
