package api

import "testing"

// dimFilter builds a lookup set from the requested dimension names, dropping
// empties, and returns nil when nothing usable remains. These cases pin the
// `d != ""` and `len(want) == 0` guards (both flagged as uncovered by the
// mutation suite) so a flipped comparison changes the observable result.
func TestDimFilter(t *testing.T) {
	t.Parallel()

	if got := dimFilter(nil); got != nil {
		t.Fatalf("dimFilter(nil) = %v, want nil", got)
	}
	if got := dimFilter(&[]string{}); got != nil {
		t.Fatalf("dimFilter(empty) = %v, want nil", got)
	}

	// Only blank names → nothing usable → nil.
	if got := dimFilter(&[]string{"", ""}); got != nil {
		t.Fatalf("dimFilter(all-blank) = %v, want nil", got)
	}

	// Mixed: blanks dropped, real names kept.
	got := dimFilter(&[]string{"cpu.util", "", "mem.used"})
	if got == nil {
		t.Fatal("dimFilter(mixed) = nil, want a populated set")
	}
	if len(got) != 2 || !got["cpu.util"] || !got["mem.used"] {
		t.Fatalf("dimFilter(mixed) = %v, want {cpu.util, mem.used}", got)
	}
	if got[""] {
		t.Fatal("blank dimension must not be a member")
	}
}
