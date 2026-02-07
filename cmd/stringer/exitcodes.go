package main

// Exit codes for stringer CLI.
const (
	ExitOK             = 0 // All collectors succeeded.
	ExitInvalidArgs    = 1 // Invalid arguments or bad path.
	ExitPartialFailure = 2 // Some collectors failed, partial output written.
	ExitTotalFailure   = 3 // No output produced.
)
