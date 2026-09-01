package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/Hikyo-Org/hikyo/api"
)

func main() {
	basePath := flag.String("base", "", "path to the frozen OpenAPI document")
	revisedPath := flag.String("revised", "", "path to the revised OpenAPI document")
	flag.Parse()

	if *basePath == "" || *revisedPath == "" || flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "usage: api-freeze-guard --base <path> --revised <path>")
		os.Exit(2)
	}

	base, err := os.ReadFile(*basePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "API freeze guard: read base: %v\n", err)
		os.Exit(1)
	}
	revised, err := os.ReadFile(*revisedPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "API freeze guard: read revised: %v\n", err)
		os.Exit(1)
	}

	violations, err := api.CheckFreeze(base, revised)
	if err != nil {
		fmt.Fprintf(os.Stderr, "API freeze guard: %v\n", err)
		os.Exit(1)
	}
	if len(violations) == 0 {
		return
	}

	fmt.Fprintln(os.Stderr, "API freeze guard failed:")
	for _, violation := range violations {
		fmt.Fprintf(os.Stderr, "  %s\n", violation)
	}
	os.Exit(1)
}
