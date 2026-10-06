package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("check-duplication", flag.ContinueOnError)
	flags.SetOutput(stderr)
	dir := flags.String("report", ".scannerwork/scanner-report", "the scan report Sonar kept")
	ceiling := flags.Float64("ceiling", 3, "the largest share of repeated lines a production file may hold, in percent")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if _, err := os.Stat(*dir); errors.Is(err, os.ErrNotExist) {
		fmt.Fprintf(stderr, "check-duplication: no kept report at %s; run make sonar, which keeps it\n", *dir)
		return 2
	}
	loaded, err := readReport(*dir)
	if err != nil {
		fmt.Fprintf(stdout, "check-duplication: unreadable report at %s: %v\n", *dir, err)
		return 1
	}
	return evaluate(loaded, *ceiling, stdout)
}

func evaluate(loaded report, ceiling float64, out io.Writer) int {
	over, production := loaded.above(ceiling)
	if production == 0 {
		fmt.Fprintln(out, "check-duplication: read no production files")
		return 1
	}
	for _, item := range over {
		fmt.Fprintf(out, "%s: %.1f%% repeated (%d of %d lines), above %g%%\n", item.path, item.percent(), item.repeated, item.lines, ceiling)
	}
	fmt.Fprintf(out, "check-duplication: read %d production files, %d above %g%%\n", production, len(over), ceiling)
	if len(over) > 0 {
		return 1
	}
	return 0
}
