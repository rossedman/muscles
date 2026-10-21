// Command muscles explains container images: what each layer changed,
// where the wasted space is, and what changed between two versions.
package main

import (
	"os"

	"github.com/rossedman/muscles/internal/cli"
)

var execute = cli.Execute

// main runs the CLI and exits with its code.
func main() { os.Exit(run()) }

// run calls execute, a variable so tests can swap in a fake.
func run() int { return execute() }
