package main

import (
	"fmt"
	"strconv"
	"strings"
)

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

// Product reports whether this frame is code this repository ships or imports. A module path's
// first element carries a domain, which no standard-library path does; main has no path.
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

// Site is the first product frame of the stack, or its top frame when none is product code.
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

// key identifies a stack across readings by its symbolised frames.
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

// ParseHeapStacks reads a heap profile taken with debug=1. Its weight is the bytes still held.
func ParseHeapStacks(page string) ([]StackSample, error) {
	if !strings.HasPrefix(strings.TrimSpace(page), "heap profile:") {
		return nil, fmt.Errorf("this is not a heap profile: it begins %q", firstLine(page))
	}
	return parseStacks(page, heapInUseBytes), nil
}

func firstLine(page string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(page), "\n")
	const most = 60
	if len(line) > most {
		return line[:most]
	}
	return line
}

// parseStacks reads the sample blocks both text profiles share: a weight line, then one
// tab-indented line per symbolised frame.
func parseStacks(page string, weigh func(head string) (float64, bool)) []StackSample {
	var (
		samples []StackSample
		current *StackSample
	)
	flush := func() {
		// A sample with no frames names no site, so a truncated page yields fewer rows.
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

// parseFrame reads one symbolised frame. Its tab indent separates it from the memory-statistics
// comments the heap profile ends with.
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

func goroutineWeight(head string) (float64, bool) {
	value, err := strconv.ParseFloat(strings.TrimSpace(head), 64)
	if err != nil {
		return 0, false
	}
	return value, true
}

// heapInUseBytes reads the held bytes from a head shaped `<objects>: <bytes> [<ever>: <ever>]`.
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
