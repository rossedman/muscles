package cli

import (
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// TestDocsCoverEveryCommand keeps docs/cli.md in step with the command tree:
// every visible command, and every flag it defines, is documented there.
func TestDocsCoverEveryCommand(t *testing.T) {
	b, err := os.ReadFile("../../docs/cli.md")
	if err != nil {
		t.Fatal(err)
	}
	doc := string(b)
	root := newRootCmd(newApp(Options{}, nil))
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		for _, sub := range c.Commands() {
			if !sub.IsAvailableCommand() || sub.Name() == "completion" {
				continue
			}
			if !strings.Contains(doc, sub.CommandPath()) {
				t.Errorf("docs/cli.md doesn't mention %q", sub.CommandPath())
			}
			sub.LocalNonPersistentFlags().VisitAll(func(f *pflag.Flag) {
				if f.Name != "help" && !strings.Contains(doc, "--"+f.Name) {
					t.Errorf("docs/cli.md doesn't mention %s --%s", sub.CommandPath(), f.Name)
				}
			})
			walk(sub)
		}
	}
	walk(root)
	root.PersistentFlags().VisitAll(func(f *pflag.Flag) {
		if !strings.Contains(doc, "--"+f.Name) {
			t.Errorf("docs/cli.md doesn't mention global flag --%s", f.Name)
		}
	})
}
