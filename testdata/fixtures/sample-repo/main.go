package main

import "fmt"

func main() {
	// TODO: Add proper CLI argument parsing
	fmt.Println("hello world")

	// FIXME: This will panic on nil input
	process(nil)
}

// process handles the input data.
func process(data []byte) {
	// HACK: Temporary workaround until upstream fixes the API
	if data == nil {
		return
	}
	fmt.Println(string(data))
}
