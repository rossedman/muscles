// Command docker-muscles is muscles as a Docker CLI plugin, so that
// `docker muscles layers app:dev` runs `muscles layers app:dev` against the
// daemon of the active Docker context.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/rossedman/muscles/internal/cli"
	"github.com/rossedman/muscles/internal/version"
)

// pluginName is the name docker runs the plugin under, and passes back as
// its first argument.
const pluginName = "muscles"

// metadataCommand is how docker asks a plugin to describe itself.
const metadataCommand = "docker-cli-plugin-metadata"

// metadata is what docker expects from `docker-muscles docker-cli-plugin-metadata`.
type metadata struct {
	SchemaVersion    string
	Vendor           string
	Version          string
	ShortDescription string
	URL              string
}

// env is the process environment, abstracted so tests can fake it.
type env struct {
	getenv func(string) string
	setenv func(string, string) error
	home   func() (string, error)
}

// osEnv is the real environment.
var osEnv = env{getenv: os.Getenv, setenv: os.Setenv, home: os.UserHomeDir}

// runCLI runs muscles, a variable so tests can swap in a fake.
var runCLI = cli.Run

// main runs the plugin and exits with its code.
func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, osEnv)) }

// run answers docker's metadata request, or runs muscles with the plugin
// name stripped and the active Docker context applied.
func run(args []string, stdout, stderr io.Writer, e env) int {
	if len(args) > 0 && args[0] == metadataCommand {
		return writeMetadata(stdout)
	}
	if len(args) > 0 && args[0] == pluginName {
		args = args[1:]
	}
	if err := applyContext(e); err != nil {
		fmt.Fprintf(stderr, "docker muscles: %v\n", err)
		return cli.ExitFailure
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return runCLI(ctx, args, cli.Options{})
}

// writeMetadata prints the plugin's metadata as JSON.
func writeMetadata(w io.Writer) int {
	m := metadata{
		SchemaVersion:    "0.1.0",
		Vendor:           "rossedman",
		Version:          version.Get().Version,
		ShortDescription: "Explain container images, layer by layer",
		URL:              "https://rossedman.io/muscles/",
	}
	if err := json.NewEncoder(w).Encode(m); err != nil {
		return cli.ExitFailure
	}
	return cli.ExitOK
}
