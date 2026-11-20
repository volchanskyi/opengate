package agentapi

import (
	"bytes"
	"compress/flate"
	"errors"
	"io"
	"runtime"
	"sync"
)

// evidenceInflater reads one blob at a time through a decompressor and scratch buffer it keeps.
type evidenceInflater struct {
	src      bytes.Reader
	reader   io.Reader
	resetter flate.Resetter
	scratch  [4096]byte
}

func newEvidenceInflater() *evidenceInflater {
	in := &evidenceInflater{}
	reader := flate.NewReader(&in.src)
	in.reader = reader
	// The reader flate.NewReader returns implements flate.Resetter by its documentation.
	in.resetter = reader.(flate.Resetter)
	return in
}

// inflatedSize reads the blob to its end, stopping once it passes limit bytes.
func (in *evidenceInflater) inflatedSize(blob []byte, limit int64) (int64, error) {
	in.src.Reset(blob)
	defer in.src.Reset(nil)
	if err := in.resetter.Reset(&in.src, nil); err != nil {
		return 0, err
	}
	var total int64
	for total <= limit {
		n, err := in.reader.Read(in.scratch[:])
		total += int64(n)
		if errors.Is(err, io.EOF) {
			return total, nil
		}
		if err != nil {
			return total, err
		}
	}
	return total, nil
}

// inflaterPool keeps up to one idle inflater per processor and hands out the latest returned,
// since a decompressor is about 40 KB to allocate and clear.
type inflaterPool struct {
	mu   sync.Mutex
	idle []*evidenceInflater
}

func (p *inflaterPool) get() *evidenceInflater {
	p.mu.Lock()
	n := len(p.idle)
	if n == 0 {
		p.mu.Unlock()
		return newEvidenceInflater()
	}
	in := p.idle[n-1]
	p.idle = p.idle[:n-1]
	p.mu.Unlock()
	return in
}

func (p *inflaterPool) put(in *evidenceInflater) {
	p.mu.Lock()
	if len(p.idle) < cap(p.idle) {
		p.idle = append(p.idle, in)
	}
	p.mu.Unlock()
}

var evidenceInflaters = inflaterPool{idle: make([]*evidenceInflater, 0, runtime.GOMAXPROCS(0))}

// checkEvidenceReadable proves the blob is what its codec says it is, and that
// it does not expand past anything the fixed composition could produce.
func checkEvidenceReadable(blob []byte) error {
	in := evidenceInflaters.get()
	defer evidenceInflaters.put(in)
	inflated, err := in.inflatedSize(blob, maxEvidenceInflatedBytes)
	if err != nil {
		return err
	}
	if inflated > maxEvidenceInflatedBytes {
		return errEvidenceTooLargeInflated
	}
	return nil
}
