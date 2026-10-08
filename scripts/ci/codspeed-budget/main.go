package main

import (
	"fmt"
	"os"

	"github.com/Hikyo-Org/hikyo/scripts/ci/benchmark"
)

func main() {
	if err := benchmark.CheckAllowance(); err != nil {
		fmt.Fprintln(os.Stderr, "CodSpeed budget:", err)
		os.Exit(1)
	}
}
