package main

import (
	"fmt"
	"os"

	"github.com/Hikyo-Org/hikyo/scripts/ci/benchmark"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: pr-benchmark-control prepare|request|verify|report|automatic")
		os.Exit(2)
	}
	if err := benchmark.Control(os.Args[1]); err != nil {
		fmt.Fprintln(os.Stderr, "PR benchmark:", err)
		os.Exit(1)
	}
}
