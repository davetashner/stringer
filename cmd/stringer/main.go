package main

import (
	"fmt"
	"os"
)

// Version is set via -ldflags at build time.
var Version = "dev"

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
