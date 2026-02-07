package main

import (
	"fmt"
	"os"
)

// Version is set via -ldflags at build time.
var Version = "dev"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "version" {
		fmt.Printf("stringer %s\n", Version)
		return
	}

	fmt.Fprintln(os.Stderr, "usage: stringer <command>")
	fmt.Fprintln(os.Stderr, "  version    Print version information")
	os.Exit(1)
}
