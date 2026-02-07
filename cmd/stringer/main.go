package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/davetashner/stringer/internal/redact"
)

// Version is set via -ldflags at build time.
var Version = "dev"

func main() {
	if err := rootCmd.Execute(); err != nil {
		var ece *exitCodeError
		if errors.As(err, &ece) {
			if ece.msg != "" {
				fmt.Fprintln(os.Stderr, redact.String(ece.msg))
			}
			os.Exit(ece.code)
		}
		fmt.Fprintln(os.Stderr, redact.String(err.Error()))
		os.Exit(1)
	}
}
