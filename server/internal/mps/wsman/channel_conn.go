package wsman

import (
	"io"
)

// ChannelConn adapts an MPS APF channel into an io.ReadWriteCloser.
// The message loop calls Feed() to push received data; the WSMAN client reads it.
// Writes are sent as channel data via the writeFn callback.
type ChannelConn struct {
	pr      *io.PipeReader
	pw      *io.PipeWriter
	writeFn func(data []byte) error
}

// NewChannelConn creates a ChannelConn with the given write function.
func NewChannelConn(writeFn func([]byte) error) *ChannelConn {
	pr, pw := io.Pipe()
	return &ChannelConn{pr: pr, pw: pw, writeFn: writeFn}
}

// Read reads data that was pushed via Feed.
func (cc *ChannelConn) Read(p []byte) (int, error) {
	return cc.pr.Read(p)
}

// Write sends data as APF channel data.
func (cc *ChannelConn) Write(p []byte) (int, error) {
	if err := cc.writeFn(p); err != nil {
		return 0, err
	}
	return len(p), nil
}

// Feed pushes received channel data into the read pipe.
// Called by the MPS message loop via Channel.OnData.
func (cc *ChannelConn) Feed(data []byte) {
	cc.pw.Write(data) //nolint:errcheck
}

// Close closes both ends of the pipe.
func (cc *ChannelConn) Close() error {
	cc.pw.Close()
	return cc.pr.Close()
}
