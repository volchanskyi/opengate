package wsman

import (
	"fmt"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelConnReadWrite(t *testing.T) {
	pr, pw := io.Pipe()
	cc := &ChannelConn{
		pr:      pr,
		pw:      pw,
		writeFn: func(data []byte) error { return nil },
	}

	// Feed data (simulates message loop calling OnData).
	go func() {
		cc.Feed([]byte("hello"))
		cc.Feed([]byte(" world"))
	}()

	buf := make([]byte, 11)
	n, err := io.ReadFull(cc, buf)
	require.NoError(t, err)
	assert.Equal(t, 11, n)
	assert.Equal(t, "hello world", string(buf))

	require.NoError(t, cc.Close())
}

func TestChannelConnFeedAfterClose(t *testing.T) {
	cc := NewChannelConn(func([]byte) error { return nil })
	cc.Close()

	// Feed on a closed pipe should not panic — it silently returns.
	assert.NotPanics(t, func() {
		cc.Feed([]byte("data after close"))
	})
}

func TestChannelConnWriteErrorPropagation(t *testing.T) {
	pr, pw := io.Pipe()
	defer pr.Close()
	defer pw.Close()

	wantErr := fmt.Errorf("simulated write error")
	cc := &ChannelConn{
		pr:      pr,
		pw:      pw,
		writeFn: func([]byte) error { return wantErr },
	}

	n, err := cc.Write([]byte("data"))
	assert.Equal(t, 0, n)
	assert.ErrorIs(t, err, wantErr)
}

func TestChannelConnWriteCallsWriteFn(t *testing.T) {
	pr, pw := io.Pipe()
	defer pr.Close()
	defer pw.Close()

	var written []byte
	cc := &ChannelConn{
		pr: pr,
		pw: pw,
		writeFn: func(data []byte) error {
			written = append(written, data...)
			return nil
		},
	}

	_, err := cc.Write([]byte("test data"))
	require.NoError(t, err)
	assert.Equal(t, "test data", string(written))
}
