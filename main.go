// main is the entry point that starts the Cobra-based CLI application.
package main

import "commitment-issues/cmd"

func main() {
	// Defers all execution routing to the Cobra framework
	cmd.Execute()
}