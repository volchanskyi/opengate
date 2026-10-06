package api

import "testing"

func TestDimFilter(t *testing.T) {
	t.Parallel()

	if got := dimFilter(nil); got != nil {
		t.Fatalf("dimFilter(nil) = %v, want nil", got)
	}
	if got := dimFilter(&[]string{}); got != nil {
		t.Fatalf("dimFilter(empty) = %v, want nil", got)
	}

	if got := dimFilter(&[]string{"", ""}); got != nil {
		t.Fatalf("dimFilter(all-blank) = %v, want nil", got)
	}

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
