// Command cpubench runs a fixed minute of hashing and compression on one processor and prints how
// many rounds it finished, so two machines' processors compare on the same work.
package main

import (
	"flag"
	"fmt"
	"os"
	"runtime"
	"time"
)

func main() {
	window := flag.Duration("window", time.Minute, "how long the fixed work runs")
	flag.Parse()
	runtime.GOMAXPROCS(1)
	if err := write(os.Stdout, run(*window)); err != nil {
		fmt.Fprintf(os.Stderr, "cpubench: %v\n", err)
		os.Exit(1)
	}
}
