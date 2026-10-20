// Command muscles explains container images: what each layer changed,
// where the wasted space is, and what changed between two versions.
package main

import (
	"os"

	"github.com/rossedman/muscles/internal/cli"
)

var execute = cli.Execute

func main() { os.Exit(run()) }

func run() int { return execute() }
