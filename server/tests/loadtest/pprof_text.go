package main

import (
	"fmt"
	"strconv"
	"strings"
)

// Reading a profile the target symbolised on the way out.
//
// Both text profiles share one shape: a line carrying a sample's weight and its
// addresses, then one tab-indented line per frame, carrying the function and the
// file and line it is written at. Only the weight differs — goroutines parked on
// the stack for one, bytes still held for the other.
//
// It reads that shape and nothing else, the way the two exposition readers
// beside it do, rather than pulling in a parser for the whole pprof format. What
// a change to the format would break is then visible in one place instead of
// behind a library.
//
// A page that is not the profile it was asked for is refused rather than read as
// a profile carrying no stacks. Nothing separates "the server holds nothing" from
// "the server answered 404" once an empty slice is all that is left, and the
// first of those is the healthiest reading a leak detector can produce.

// Frame is one symbolised entry of a stack, as the target printed it.
type Frame struct {
	Function string
	Location string
}

// String is the frame as a reader wants it: what ran, and where it is written.
func (f Frame) String() string {
	if f.Location == "" {
		return f.Function
	}
	return f.Function + " " + f.Location
}

// Product reports whether this frame is code this repository ships or imports,
// as opposed to the standard library.
//
// It is the rule Go itself uses to tell a module path from a standard-library
// one: the first element of an import path carries a domain, and no
// standard-library path ever does. `main` is the other half — a program's own
// entry package has no path at all.
//
// The distinction earns its place because every parked goroutine's own top
// frame is the runtime parking it. A site taken from the top of the stack would
// report every leak in the product at the same line of proc.go.
func (f Frame) Product() bool {
	head := f.Function
	if slash := strings.Index(head, "/"); slash >= 0 {
		head = head[:slash]
	} else if dot := strings.Index(head, "."); dot >= 0 {
		head = head[:dot]
	}
	return head == "main" || strings.Contains(head, ".")
}

// StackSample is one stack and what it weighed in the profile it came from:
// goroutines parked on it, or bytes still held by what it allocated.
type StackSample struct {
	Frames []Frame
	Weight float64
}

// Site is the line somebody opens. It is the first frame belonging to code this
// repository ships or imports; a stack that is standard library the whole way
// down names its own top rather than naming nothing.
func (s StackSample) Site() string {
	for _, frame := range s.Frames {
		if frame.Product() {
			return frame.String()
		}
	}
	if len(s.Frames) > 0 {
		return s.Frames[0].String()
	}
	return ""
}

// key identifies a stack across readings. Two readings of the same process
// print the same addresses, but the comparison is made on what the addresses
// were symbolised to, so a row survives a reader who only has the text.
func (s StackSample) key() string {
	parts := make([]string, 0, len(s.Frames))
	for _, frame := range s.Frames {
		parts = append(parts, frame.String())
	}
	return strings.Join(parts, " | ")
}

// ParseGoroutineStacks reads a goroutine profile taken with debug=1. Its weight
// is goroutines parked on the stack.
func ParseGoroutineStacks(page string) ([]StackSample, error) {
	if !strings.HasPrefix(strings.TrimSpace(page), "goroutine profile:") {
		return nil, fmt.Errorf("this is not a goroutine profile: it begins %q", firstLine(page))
	}
	return parseStacks(page, goroutineWeight), nil
}

// ParseHeapStacks reads a heap profile taken with debug=1. Its weight is the
// bytes still held — not the bytes ever allocated, which rises on every healthy
// server that has ever run.
func ParseHeapStacks(page string) ([]StackSample, error) {
	if !strings.HasPrefix(strings.TrimSpace(page), "heap profile:") {
		return nil, fmt.Errorf("this is not a heap profile: it begins %q", firstLine(page))
	}
	return parseStacks(page, heapInUseBytes), nil
}

// firstLine is what a page that is not the profile it was asked for gets to say
// for itself in the error.
func firstLine(page string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(page), "\n")
	const most = 60
	if len(line) > most {
		return line[:most]
	}
	return line
}

// parseStacks reads the sample blocks both text profiles share: a line carrying
// the sample's weight and its addresses, followed by one tab-indented line per
// symbolised frame.
//
// It reads only what this harness depends on rather than the whole format, the
// way the two exposition readers beside it do, so what a change to the format
// would break is visible in one place.
func parseStacks(page string, weigh func(head string) (float64, bool)) []StackSample {
	var (
		samples []StackSample
		current *StackSample
	)
	flush := func() {
		// A sample with no frames names no site, so it can neither be reported
		// nor compared. pprof always prints frames for these profiles; the
		// guard is here so a truncated page produces fewer rows rather than a
		// row nobody can read.
		if current != nil && len(current.Frames) > 0 {
			samples = append(samples, *current)
		}
		current = nil
	}

	for _, line := range strings.Split(page, "\n") {
		if frame, ok := parseFrame(line); ok {
			if current != nil {
				current.Frames = append(current.Frames, frame)
			}
			continue
		}
		flush()
		head, _, found := strings.Cut(line, " @ ")
		if !found || head == "" || !isDigit(head[0]) {
			continue
		}
		if weight, ok := weigh(head); ok {
			current = &StackSample{Weight: weight}
		}
	}
	flush()
	return samples
}

// parseFrame reads one symbolised frame. A frame line is tab-indented after its
// hash, which is what tells it apart from the memory-statistics comments the
// heap profile ends with.
func parseFrame(line string) (Frame, bool) {
	if !strings.HasPrefix(line, "#\t") {
		return Frame{}, false
	}
	fields := strings.Split(line, "\t")
	if len(fields) < 3 {
		return Frame{}, false
	}
	function := fields[2]
	if plus := strings.LastIndex(function, "+0x"); plus > 0 {
		function = function[:plus]
	}
	frame := Frame{Function: function}
	if len(fields) >= 4 {
		frame.Location = fields[3]
	}
	return frame, true
}

func isDigit(b byte) bool { return b >= '0' && b <= '9' }

// goroutineWeight reads the count off a goroutine sample's head, which is the
// count alone.
func goroutineWeight(head string) (float64, bool) {
	value, err := strconv.ParseFloat(strings.TrimSpace(head), 64)
	if err != nil {
		return 0, false
	}
	return value, true
}

// heapInUseBytes reads the bytes still held off a heap sample's head, which is
// `<objects>: <bytes> [<objects ever>: <bytes ever>]`.
func heapInUseBytes(head string) (float64, bool) {
	if open := strings.Index(head, "["); open >= 0 {
		head = head[:open]
	}
	_, bytes, found := strings.Cut(head, ":")
	if !found {
		return 0, false
	}
	value, err := strconv.ParseFloat(strings.TrimSpace(bytes), 64)
	if err != nil {
		return 0, false
	}
	return value, true
}
