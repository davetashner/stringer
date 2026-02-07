package main

import (
	"errors"
	"fmt"
	"os"
)

// Version is set via -ldflags at build time.
var Version = "dev"

func main() {
	if err := rootCmd.Execute(); err != nil {
		var ece *exitCodeError
		if errors.As(err, &ece) {
			if ece.msg != "" {
				fmt.Fprintln(os.Stderr, ece.msg)
			}
			os.Exit(ece.code)
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
